import Foundation

/// Settings shared by the app and the packet-tunnel extension.
enum AppConfig {
    /// App Group both targets belong to (set per build in Info.plist, see project.yml).
    static let appGroup: String =
        Bundle.main.object(forInfoDictionaryKey: "AMAppGroup") as? String ?? "group.io.agentmesh.control"
    /// Built-in rendezvous for pairing codes (same gist as the Android app,
    /// mobile/android-control/gradle.properties), kept current by the server.
    static let defaultRendezvous = "https://api.github.com/gists/9b876c950f541c735c8a817c96362ca9"
    static let mtu = 1500
    static var version: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev"
    }
    /// Shown in the dashboard's "Phones connected now" list.
    static var clientLabel: String {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        return "iPhone (iOS \(v.majorVersion).\(v.minorVersion))"
    }
}

/// A saved remote server: the AgentMesh address plus the connect key from a
/// QR code / link. The key itself is kept in the Keychain (see ProfileStore).
struct ConnectProfile: Codable, Identifiable, Equatable {
    var id: String
    var name: String
    var serverUrl: String
    var keyHash: String
    var rendezvousUrl: String = ""
    var deviceName: String = ""
    var lastConnectedAt: Date?
}

/// What a connect link carries.
struct ConnectLink: Equatable {
    var serverUrl: String
    var key: String
    var rendezvousUrl: String
}

struct APIError: LocalizedError {
    let status: Int
    let code: String
    let message: String
    var errorDescription: String? { message }
}

enum Links {
    /// Accepts every form a connect link can arrive in:
    ///   https://<server>/join.html#k=<key>&r=<rendezvous>   (QR code / shared link)
    ///   agentmesh://join?u=<server>&k=<key>&r=<rendezvous>   (deep link from the join page)
    static func parse(_ raw: String) -> ConnectLink? {
        let s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !s.isEmpty, let c = URLComponents(string: s) else { return nil }
        switch c.scheme?.lowercased() {
        case "agentmesh":
            guard var u = c.queryItems?.first(where: { $0.name == "u" })?.value,
                  let k = c.queryItems?.first(where: { $0.name == "k" })?.value else { return nil }
            while u.hasSuffix("/") { u.removeLast() }
            guard u.hasPrefix("https://") else { return nil }
            return ConnectLink(serverUrl: u, key: k, rendezvousUrl: c.queryItems?.first(where: { $0.name == "r" })?.value ?? "")
        case "https":
            guard let host = c.host, !host.isEmpty,
                  let params = URLComponents(string: "x://x?" + (c.percentEncodedFragment ?? ""))?.queryItems,
                  let k = params.first(where: { $0.name == "k" })?.value else { return nil }
            let authority = c.port.map { "\(host):\($0)" } ?? host
            return ConnectLink(serverUrl: "https://" + authority, key: k,
                               rendezvousUrl: params.first(where: { $0.name == "r" })?.value ?? "")
        default:
            return nil
        }
    }

    /// Normalizes a server address typed by hand (Edit screen):
    ///   203.0.113.7 -> https://203.0.113.7:13443, host:port -> https://host:port, https://x/y -> https://x
    static func normalizeServer(_ input: String) -> String? {
        var s = input.trimmingCharacters(in: .whitespacesAndNewlines)
        while s.hasSuffix("/") { s.removeLast() }
        if s.lowercased().hasPrefix("http://") { return nil }
        if s.lowercased().hasPrefix("https://") { s = String(s.dropFirst(8)) }
        let hostPort = String(s.split(separator: "/", maxSplits: 1, omittingEmptySubsequences: false).first ?? "")
        guard !hostPort.isEmpty, !hostPort.contains(" ") else { return nil }
        let host = String(hostPort.split(separator: ":", maxSplits: 1).first ?? "")
        let parts = host.split(separator: ".", omittingEmptySubsequences: false)
        let isIP = parts.count == 4 && parts.allSatisfy { Int($0).map { (0...255).contains($0) } ?? false }
        if hostPort.contains(":") { return "https://\(hostPort)" }
        if isIP { return "https://\(host):13443" }
        if host.contains(".") || host == "localhost" { return "https://\(host)" }
        return nil
    }

    /// True for a typed 6-digit pairing code ("123 456" is fine; letters are not).
    static func pairingCode(_ raw: String) -> String? {
        let digits = raw.filter(\.isNumber)
        return digits.count == 6 && !raw.contains(where: \.isLetter) ? digits : nil
    }
}

/// Live tunnel state, reported by the extension to the app (handleAppMessage).
struct TunnelStatus: Codable, Equatable {
    enum State: String, Codable { case idle, connecting, reconnecting, connected, failed }
    var state: State = .idle
    var deviceName = ""
    var attempt = 0
    var reason = ""
    var since: Date?
    var bytesUp: Int64 = 0
    var bytesDown: Int64 = 0
    var flows: Int64 = 0
    var failedFlows: Int64 = 0
}

/// Messages the app sends to the extension.
enum TunnelCommand: String, Codable { case status, publicIP }

struct TunnelReply: Codable {
    var status: TunnelStatus?
    var ip: String?
    var error: String?
}

func humanBytes(_ b: Int64) -> String {
    let d = Double(b)
    switch b {
    case (1 << 30)...: return String(format: "%.1f GB", d / Double(1 << 30))
    case (1 << 20)...: return String(format: "%.1f MB", d / Double(1 << 20))
    case (1 << 10)...: return String(format: "%.1f KB", d / 1024)
    default: return "\(b) B"
    }
}
