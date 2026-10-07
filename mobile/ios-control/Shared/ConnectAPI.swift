import Foundation

/// A granted exit-node session, ready for the tunnel engine.
struct SessionGrant {
    var relayUrl: String
    var ticket: String
    var sessionId: String
    var deviceName: String
    var serverUrl: String
}

/// Opens sessions with a connect key (no login) and exchanges pairing codes.
/// Same protocol as the Android app (ConnectApi.kt). If the saved address stops
/// answering (the free tunnel restarted with a new address), the current
/// address is looked up via the rendezvous URL and saved.
final class ConnectAPI {
    private let store: ProfileStore
    private let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.timeoutIntervalForRequest = 30
        return URLSession(configuration: c)
    }()

    init(store: ProfileStore) { self.store = store }

    func open(_ p: ConnectProfile) async throws -> SessionGrant {
        guard let key = store.key(p) else {
            throw APIError(status: 0, code: "no_key", message: "The connect key for \(p.name) is missing. Scan the QR code again.")
        }
        do {
            return try await openAt(p.serverUrl, key: key)
        } catch {
            // A clear answer from the server (e.g. link revoked) is final.
            if let e = error as? APIError, (400..<500).contains(e.status) { throw e }
            guard !p.rendezvousUrl.isEmpty, let fresh = try? await resolve(p.rendezvousUrl), fresh != p.serverUrl else { throw error }
            let g = try await openAt(fresh, key: key)
            var updated = store.get(p.id) ?? p
            updated.serverUrl = fresh
            store.upsert(updated)
            return g
        }
    }

    /// Exchanges a 6-digit pairing code for a connect link. The phone does not
    /// know the server's address, so it tries the address published at the
    /// built-in rendezvous URL, then the servers it has saved.
    func pair(code: String) async throws -> ConnectLink {
        var servers: [String] = []
        func add(_ s: String?) { if let s, !servers.contains(s) { servers.append(s) } }
        add(try? await resolve(AppConfig.defaultRendezvous))
        for p in store.all() {
            if !p.rendezvousUrl.isEmpty { add(try? await resolve(p.rendezvousUrl)) }
            add(p.serverUrl)
        }
        if servers.isEmpty {
            throw APIError(status: 0, code: "no_server",
                           message: "Could not find the AgentMesh server. Check your internet connection, or scan the QR code instead.")
        }
        var last: Error?
        for server in servers {
            do {
                return try await pairAt(server, code: code)
            } catch {
                // A wrong code on one server may still be right on another; prefer reporting the server's answer.
                if !(last is APIError) { last = error }
            }
        }
        throw last ?? APIError(status: 0, code: "unreachable", message: "Could not reach the AgentMesh server")
    }

    // MARK: - private

    private func post(_ url: String, body: [String: String], headers: [String: String] = [:]) async throws -> (Int, [String: Any]) {
        guard let u = URL(string: url) else { throw APIError(status: 0, code: "bad_url", message: "Invalid server address") }
        var req = URLRequest(url: u)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("AgentMesh-iOS", forHTTPHeaderField: "User-Agent")
        for (k, v) in headers { req.setValue(v, forHTTPHeaderField: k) }
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (data, resp) = try await session.data(for: req)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
        if !(200..<300).contains(status) {
            let err = json["error"] as? [String: Any]
            guard let msg = err?["message"] as? String else {
                throw URLError(.badServerResponse, userInfo: [NSLocalizedDescriptionKey: "server unreachable (HTTP \(status))"])
            }
            throw APIError(status: status, code: err?["code"] as? String ?? "http_\(status)", message: msg)
        }
        return (status, json)
    }

    private func openAt(_ server: String, key: String) async throws -> SessionGrant {
        let (_, o) = try await post("\(server)/v1/connect/session", body: ["client_label": AppConfig.clientLabel],
                                    headers: ["X-AgentMesh-Connect-Key": key])
        guard let ticket = o["ticket"] as? String, let sid = o["session_id"] as? String else {
            throw URLError(.cannotParseResponse)
        }
        // Same address that just answered: works from any network.
        return SessionGrant(relayUrl: "\(server)/v1/relay", ticket: ticket, sessionId: sid,
                            deviceName: o["device_name"] as? String ?? "", serverUrl: server)
    }

    private func pairAt(_ server: String, code: String) async throws -> ConnectLink {
        let (_, o) = try await post("\(server)/v1/connect/pair", body: ["code": code, "client_label": AppConfig.clientLabel])
        guard let raw = o["link"] as? String, var link = Links.parse(raw) else {
            throw APIError(status: 0, code: "bad_link", message: "The server sent an invalid link")
        }
        link.serverUrl = server // the address that actually answered works from this network
        return link
    }

    /// Reads {"url": ...} directly or from a GitHub gist API response.
    func resolve(_ rendezvous: String) async throws -> String? {
        guard let u = URL(string: rendezvous) else { return nil }
        var req = URLRequest(url: u)
        req.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        req.cachePolicy = .reloadIgnoringLocalCacheData
        let (data, resp) = try await session.data(for: req)
        guard (resp as? HTTPURLResponse)?.statusCode == 200,
              let root = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        var doc = root
        if let files = root["files"] as? [String: Any],
           let f = files["agentmesh-endpoint.json"] as? [String: Any],
           let content = (f["content"] as? String)?.data(using: .utf8),
           let inner = try? JSONSerialization.jsonObject(with: content) as? [String: Any] {
            doc = inner
        }
        guard var url = doc["url"] as? String, url.hasPrefix("https://") else { return nil }
        while url.hasSuffix("/") { url.removeLast() }
        return url
    }
}
