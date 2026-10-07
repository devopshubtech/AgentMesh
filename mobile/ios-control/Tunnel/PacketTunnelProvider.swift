import Network
import NetworkExtension
import Tunnel // gomobile framework built from mobile/tunnel (see build.sh)

/// Routes this iPhone's traffic through an AgentMesh exit-node device. The
/// iOS counterpart of the Android ExitVpnService:
///
/// - all IPv4 goes into the tunnel, private LAN ranges stay direct; IPv6 is
///   captured too (so it cannot leak) and dropped by the engine, so apps fall
///   back to IPv4 at once;
/// - the Go engine terminates TCP/UDP and carries every flow over the relay to
///   the agent; this extension's own connections (API, relay) bypass the tunnel;
/// - if the relay drops (network change, server restart) a fresh session is
///   swapped in without taking the VPN down; it gives up after 10 minutes or
///   when the server says no (link revoked).
final class PacketTunnelProvider: NEPacketTunnelProvider {
    private let store = ProfileStore()
    private lazy var api = ConnectAPI(store: store)
    private let queue = DispatchQueue(label: "agentmesh.tunnel")
    private var engine: TunnelEngine?
    private var profileID = ""
    private var deviceName = "server"
    private var status = TunnelStatus()
    private var monitor: Task<Void, Never>?
    private var stopping = false
    private var networkChanged = false
    private var pathMonitor: NWPathMonitor?
    private var lastPathKey: String?

    private static let retryDelays: [UInt64] = [1, 3, 5, 10]
    private static let relayRetry: [UInt64] = [0, 1, 2, 3, 5, 10, 15, 30]
    private static let giveUpAfter: TimeInterval = 10 * 60
    private static let localRanges: [(String, String)] = [
        ("10.0.0.0", "255.0.0.0"), ("172.16.0.0", "255.240.0.0"), ("192.168.0.0", "255.255.0.0"),
        ("169.254.0.0", "255.255.0.0"), ("100.64.0.0", "255.192.0.0"),
    ]

    // MARK: - lifecycle

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        // iOS may restart the tunnel by itself (no options): use the last server.
        let id = (options?["profileID"] as? String) ?? store.lastProfileID
        guard let id, let profile = store.get(id) else {
            completionHandler(fail("The saved server was not found. Open AgentMesh and connect again."))
            return
        }
        profileID = id
        store.lastProfileID = id
        store.lastError = nil
        deviceName = profile.deviceName.isEmpty ? profile.name : profile.deviceName
        setStatus { $0 = TunnelStatus(state: .connecting, deviceName: self.deviceName) }

        Task {
            var lastError: Error = APIError(status: 0, code: "unreachable", message: "Server unreachable")
            for (i, wait) in ([0] + Self.retryDelays).enumerated() {
                if i > 0 {
                    setStatus { $0.state = .reconnecting; $0.attempt = i; $0.reason = lastError.localizedDescription }
                    try? await Task.sleep(nanoseconds: wait * 1_000_000_000)
                }
                do {
                    guard let grant = try await openSession() else { return completionHandler(fail(store.lastError ?? "Stopped")) }
                    try await applyNetworkSettings(server: grant.serverUrl)
                    try startEngine(grant)
                    watchNetwork()
                    completionHandler(nil)
                    return
                } catch let e as APIError where (400..<500).contains(e.status) {
                    return completionHandler(fail(e.message)) // link revoked/expired: retrying will not help
                } catch {
                    lastError = error
                }
            }
            completionHandler(fail("Could not connect to \(deviceName): \(lastError.localizedDescription)"))
        }
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        queue.sync { stopping = true }
        monitor?.cancel()
        pathMonitor?.cancel()
        engine?.stop()
        engine = nil
        setStatus { $0 = TunnelStatus() }
        completionHandler()
    }

    /// The app asks for the live status (once a second while visible) and for
    /// the public IP check.
    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        let cmd = (try? JSONDecoder().decode(TunnelCommand.self, from: messageData)) ?? .status
        switch cmd {
        case .status:
            completionHandler?(try? JSONEncoder().encode(TunnelReply(status: queue.sync { status })))
        case .publicIP:
            guard let eng = engine else {
                completionHandler?(try? JSONEncoder().encode(TunnelReply(error: "Not connected")))
                return
            }
            DispatchQueue.global().async {
                var reply = TunnelReply()
                var err: NSError?
                let ip = eng.publicIP(&err)
                if let err { reply.error = err.localizedDescription } else { reply.ip = ip }
                completionHandler?(try? JSONEncoder().encode(reply))
            }
        }
    }

    // MARK: - session and engine

    /// Asks the server for a session with the saved connect key. Returns nil
    /// (and records why) when the saved server is gone; throws otherwise.
    private func openSession() async throws -> SessionGrant? {
        guard let profile = store.get(profileID) else {
            store.lastError = "The saved server was deleted"
            return nil
        }
        let grant = try await api.open(profile)
        if !grant.deviceName.isEmpty { deviceName = grant.deviceName }
        if var p = store.get(profileID) {
            p.deviceName = deviceName
            p.lastConnectedAt = Date()
            store.upsert(p)
        }
        return grant
    }

    private func applyNetworkSettings(server: String) async throws {
        let host = URL(string: server)?.host ?? "127.0.0.1"
        let s = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: host)
        let v4 = NEIPv4Settings(addresses: ["10.111.0.2"], subnetMasks: ["255.255.255.0"])
        v4.includedRoutes = [NEIPv4Route.default()]
        // Keep the local network (printers, a LAN AgentMesh server, ...) direct.
        v4.excludedRoutes = Self.localRanges.map { NEIPv4Route(destinationAddress: $0.0, subnetMask: $0.1) }
        s.ipv4Settings = v4
        // Captured so IPv6 cannot leak around the VPN; the engine drops it.
        let v6 = NEIPv6Settings(addresses: ["fd00:a6e5:6d65::2"], networkPrefixLengths: [64])
        v6.includedRoutes = [NEIPv6Route.default()]
        s.ipv6Settings = v6
        let dns = NEDNSSettings(servers: ["1.1.1.1", "1.0.0.1"])
        dns.matchDomains = [""] // all DNS goes through the tunnel
        s.dnsSettings = dns
        s.mtu = NSNumber(value: AppConfig.mtu)
        try await setTunnelNetworkSettings(s)
    }

    private func startEngine(_ grant: SessionGrant) throws {
        guard let fd = UtunFD.find() else {
            throw APIError(status: 0, code: "no_utun", message: "Could not find the VPN interface")
        }
        guard let eng = TunnelNewEngine() else {
            throw APIError(status: 0, code: "no_engine", message: "Could not start the tunnel engine")
        }
        try eng.start(Int(fd), relayURL: grant.relayUrl, ticket: grant.ticket, caPEM: "", mtu: AppConfig.mtu)
        engine = eng
        startMonitor(eng)
    }

    /// Watches the engine once a second. When only the relay link drops, a new
    /// one is swapped in and the VPN stays up, so apps barely notice.
    private func startMonitor(_ eng: TunnelEngine) {
        let since = Date()
        monitor?.cancel()
        monitor = Task { [weak self] in
            while let self, !Task.isCancelled, self.engine === eng, !self.isStopping {
                if !eng.isRunning() {
                    self.give(up: eng.lastError().isEmpty ? "Tunnel closed" : eng.lastError())
                    return
                }
                let changed = self.queue.sync { () -> Bool in defer { self.networkChanged = false }; return self.networkChanged }
                if !eng.relayUp() || changed {
                    let why = eng.relayUp() ? "Network changed" : (eng.lastError().isEmpty ? "relay connection closed" : eng.lastError())
                    if await !self.recoverRelay(eng, reason: why) { return }
                    continue
                }
                self.setStatus {
                    $0 = TunnelStatus(state: .connected, deviceName: self.deviceName, since: since,
                                      bytesUp: eng.bytesUp(), bytesDown: eng.bytesDown(),
                                      flows: eng.flows(), failedFlows: eng.failedFlows())
                }
                try? await Task.sleep(nanoseconds: 1_000_000_000)
            }
        }
    }

    /// Swaps a fresh relay link into the running engine. False if we gave up.
    private func recoverRelay(_ eng: TunnelEngine, reason: String) async -> Bool {
        let deadline = Date().addingTimeInterval(Self.giveUpAfter)
        var why = reason
        var attempt = 0
        while !isStopping, engine === eng, eng.isRunning() {
            attempt += 1
            setStatus { $0.state = .reconnecting; $0.attempt = attempt; $0.reason = why }
            do {
                guard let grant = try await openSession() else {
                    give(up: store.lastError ?? "Stopped")
                    return false
                }
                try eng.reconnect(grant.relayUrl, ticket: grant.ticket, caPEM: "")
                return true
            } catch let e as APIError where (400..<500).contains(e.status) {
                give(up: e.message) // the server said no for good (link revoked/expired)
                return false
            } catch {
                why = error.localizedDescription
            }
            if Date() > deadline {
                give(up: "Lost connection to \(deviceName): \(why)")
                return false
            }
            let wait = Self.relayRetry[min(attempt - 1, Self.relayRetry.count - 1)]
            try? await Task.sleep(nanoseconds: wait * 1_000_000_000)
        }
        return false
    }

    /// Reconnects at once when the iPhone switches between Wi-Fi and mobile
    /// data: the old relay socket is dead but would take the keepalive timeout
    /// to notice.
    private func watchNetwork() {
        guard pathMonitor == nil else { return }
        let m = NWPathMonitor()
        m.pathUpdateHandler = { [weak self] path in
            guard let self else { return }
            let key = path.availableInterfaces.filter { $0.type != .other }.map(\.name).joined(separator: ",")
            self.queue.sync {
                if let prev = self.lastPathKey, prev != key, path.status == .satisfied { self.networkChanged = true }
                self.lastPathKey = key
            }
        }
        m.start(queue: queue)
        pathMonitor = m
    }

    // MARK: - helpers

    private var isStopping: Bool { queue.sync { stopping } }

    private func setStatus(_ change: @escaping (inout TunnelStatus) -> Void) {
        queue.sync { change(&status) }
    }

    /// Stops the VPN with a reason the app shows.
    private func give(up message: String) {
        store.lastError = message
        setStatus { $0 = TunnelStatus(state: .failed, deviceName: self.deviceName, reason: message) }
        engine?.stop()
        cancelTunnelWithError(fail(message))
    }

    private func fail(_ message: String) -> NSError {
        store.lastError = message
        return NSError(domain: "io.agentmesh.tunnel", code: 1, userInfo: [NSLocalizedDescriptionKey: message])
    }
}
