import CryptoKit
import Foundation
import Security

/// Saved remote servers, shared by the app and the tunnel extension through
/// the App Group: the list in the group's UserDefaults, each connect key in
/// the Keychain (access group = the App Group, this device only).
final class ProfileStore {
    private let defaults = UserDefaults(suiteName: AppConfig.appGroup) ?? .standard
    private static let keychainService = "io.agentmesh.connect-keys"

    func all() -> [ConnectProfile] {
        guard let data = defaults.data(forKey: "profiles"),
              let list = try? JSONDecoder().decode([ConnectProfile].self, from: data) else { return [] }
        return list.sorted { ($0.lastConnectedAt ?? .distantPast) > ($1.lastConnectedAt ?? .distantPast) }
    }

    func get(_ id: String) -> ConnectProfile? { all().first { $0.id == id } }

    func upsert(_ p: ConnectProfile) {
        save(all().filter { $0.id != p.id } + [p])
    }

    func delete(_ id: String) {
        save(all().filter { $0.id != id })
        SecItemDelete(keyQuery(id) as CFDictionary)
    }

    func key(_ p: ConnectProfile) -> String? {
        var q = keyQuery(p.id)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let d = out as? Data else { return nil }
        return String(data: d, encoding: .utf8)
    }

    /// Adds a link, or refreshes the saved server that already uses the same key.
    @discardableResult
    func importLink(_ link: ConnectLink, name: String? = nil) -> ConnectProfile {
        let hash = Self.sha256(link.key)
        var p = all().first { $0.keyHash == hash }
            ?? ConnectProfile(id: UUID().uuidString, name: name ?? Self.hostName(link.serverUrl), serverUrl: link.serverUrl, keyHash: hash)
        p.serverUrl = link.serverUrl
        if !link.rendezvousUrl.isEmpty { p.rendezvousUrl = link.rendezvousUrl }
        setKey(link.key, for: p.id)
        upsert(p)
        return p
    }

    /// Replaces the key of an existing server with the one from a new link.
    func replaceLink(_ p: ConnectProfile, with link: ConnectLink) -> ConnectProfile {
        var p = p
        p.serverUrl = link.serverUrl
        p.keyHash = Self.sha256(link.key)
        if !link.rendezvousUrl.isEmpty { p.rendezvousUrl = link.rendezvousUrl }
        setKey(link.key, for: p.id)
        upsert(p)
        return p
    }

    /// The server the tunnel should (re)connect to, e.g. after iOS restarts it.
    var lastProfileID: String? {
        get { defaults.string(forKey: "lastProfileID") }
        set { defaults.set(newValue, forKey: "lastProfileID") }
    }

    /// Why the tunnel last stopped on its own (shown by the app).
    var lastError: String? {
        get { defaults.string(forKey: "lastError") }
        set { defaults.set(newValue, forKey: "lastError") }
    }

    // MARK: - private

    private func save(_ list: [ConnectProfile]) {
        if let data = try? JSONEncoder().encode(list) { defaults.set(data, forKey: "profiles") }
    }

    private func keyQuery(_ id: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: Self.keychainService,
         kSecAttrAccount as String: id,
         kSecAttrAccessGroup as String: AppConfig.appGroup]
    }

    private func setKey(_ key: String, for id: String) {
        SecItemDelete(keyQuery(id) as CFDictionary)
        var q = keyQuery(id)
        q[kSecValueData as String] = Data(key.utf8)
        // After first unlock: iOS may restart the tunnel while the phone is locked.
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        SecItemAdd(q as CFDictionary, nil)
    }

    private static func sha256(_ s: String) -> String {
        SHA256.hash(data: Data(s.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private static func hostName(_ url: String) -> String {
        let host = url.replacingOccurrences(of: "https://", with: "").split(separator: "/").first.map(String.init) ?? url
        return host.split(separator: ".").first.map(String.init) ?? host
    }
}
