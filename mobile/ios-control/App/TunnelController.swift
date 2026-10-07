import Foundation
import NetworkExtension

/// Owns the app's VPN configuration (NETunnelProviderManager) and talks to
/// the running packet-tunnel extension.
@MainActor
final class TunnelController: ObservableObject {
    @Published private(set) var status = TunnelStatus()
    private var manager: NETunnelProviderManager?
    private var poll: Task<Void, Never>?
    private var observer: NSObjectProtocol?

    private var extensionBundleID: String { (Bundle.main.bundleIdentifier ?? "io.agentmesh.control") + ".tunnel" }

    init() {
        Task { await load() }
    }

    /// Loads (or creates on first connect) the AgentMesh VPN configuration.
    @discardableResult
    func load() async -> NETunnelProviderManager? {
        if let manager { return manager }
        let all = (try? await NETunnelProviderManager.loadAllFromPreferences()) ?? []
        let m = all.first ?? NETunnelProviderManager()
        manager = m
        observer = NotificationCenter.default.addObserver(forName: .NEVPNStatusDidChange, object: m.connection, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.vpnStatusChanged() }
        }
        vpnStatusChanged()
        return m
    }

    /// Starts the tunnel for a saved server. The first time, iOS asks the user
    /// to allow adding a VPN configuration.
    func connect(profileID: String) async throws {
        guard let m = await load() else { return }
        let proto = (m.protocolConfiguration as? NETunnelProviderProtocol) ?? NETunnelProviderProtocol()
        proto.providerBundleIdentifier = extensionBundleID
        proto.serverAddress = "AgentMesh"
        m.protocolConfiguration = proto
        m.localizedDescription = "AgentMesh"
        m.isEnabled = true
        try await m.saveToPreferences()
        try await m.loadFromPreferences() // required after saving, before starting
        ProfileStore().lastError = nil
        status = TunnelStatus(state: .connecting)
        try m.connection.startVPNTunnel(options: ["profileID": profileID as NSString])
    }

    func disconnect() {
        manager?.connection.stopVPNTunnel()
    }

    /// The address websites see, fetched through the tunnel by the extension.
    func publicIP() async throws -> String {
        let reply = try await send(.publicIP)
        if let ip = reply.ip { return ip }
        throw APIError(status: 0, code: "ip", message: reply.error ?? "Could not check the IP")
    }

    // MARK: - private

    private func vpnStatusChanged() {
        guard let m = manager else { return }
        switch m.connection.status {
        case .connected, .connecting, .reasserting:
            startPolling()
            if status.state == .idle || status.state == .failed { status = TunnelStatus(state: .connecting) }
        case .disconnecting:
            break
        default: // disconnected, invalid
            poll?.cancel()
            poll = nil
            if let err = ProfileStore().lastError, !err.isEmpty {
                status = TunnelStatus(state: .failed, reason: err)
            } else {
                status = TunnelStatus()
            }
        }
    }

    /// Asks the extension for its status once a second while it runs.
    private func startPolling() {
        guard poll == nil else { return }
        poll = Task { [weak self] in
            while !Task.isCancelled {
                if let reply = try? await self?.send(.status), let s = reply.status, s.state != .idle {
                    self?.status = s
                }
                try? await Task.sleep(nanoseconds: 1_000_000_000)
            }
        }
    }

    private func send(_ cmd: TunnelCommand) async throws -> TunnelReply {
        guard let session = manager?.connection as? NETunnelProviderSession, session.status != .disconnected else {
            throw APIError(status: 0, code: "not_connected", message: "Not connected")
        }
        let data = try JSONEncoder().encode(cmd)
        return try await withCheckedThrowingContinuation { cont in
            do {
                try session.sendProviderMessage(data) { response in
                    if let response, let r = try? JSONDecoder().decode(TunnelReply.self, from: response) {
                        cont.resume(returning: r)
                    } else {
                        cont.resume(throwing: APIError(status: 0, code: "no_reply", message: "The tunnel did not answer"))
                    }
                }
            } catch {
                cont.resume(throwing: error)
            }
        }
    }
}
