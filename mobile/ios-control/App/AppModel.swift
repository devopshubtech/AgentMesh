import Foundation

/// App state and actions; the iOS counterpart of the Android AppViewModel.
@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var profiles: [ConnectProfile] = []
    @Published var message: String?
    @Published var showAdd = false
    @Published var editing: ConnectProfile?
    @Published private(set) var pairing = false
    @Published private(set) var exitIP: String?
    @Published private(set) var checkingIP = false

    let tunnel = TunnelController()
    private let store = ProfileStore()
    private lazy var api = ConnectAPI(store: store)

    init() { reload() }

    func reload() { profiles = store.all() }

    /// Saves a scanned / picked / pasted / deep-linked connect link and connects.
    func importLink(_ raw: String, connectAfter: Bool = true) {
        guard let link = Links.parse(raw) else {
            message = "That is not an AgentMesh connect link. Use the QR code from 'Connect a phone' in the dashboard."
            return
        }
        let p = store.importLink(link)
        showAdd = false
        reload()
        if connectAfter { connect(p) }
    }

    /// What was typed in the Add screen: a 6-digit pairing code is exchanged
    /// with the server for a connect link; anything else is treated as a link.
    func submitCodeOrLink(_ raw: String) {
        guard let code = Links.pairingCode(raw) else {
            importLink(raw)
            return
        }
        pairing = true
        Task {
            defer { pairing = false }
            do {
                let link = try await api.pair(code: code)
                let p = store.importLink(link)
                showAdd = false
                reload()
                connect(p)
            } catch {
                message = error.localizedDescription
            }
        }
    }

    func saveEdit(_ p: ConnectProfile, name: String, server: String, newLink: String) {
        var updated = p
        let trimmedName = name.trimmingCharacters(in: .whitespaces)
        if !trimmedName.isEmpty { updated.name = trimmedName }
        if !newLink.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            guard let link = Links.parse(newLink) else {
                message = "The new link is not a valid AgentMesh connect link"
                return
            }
            updated = store.replaceLink(updated, with: link)
        } else if !server.trimmingCharacters(in: .whitespaces).isEmpty, server.trimmingCharacters(in: .whitespaces) != p.serverUrl {
            guard let normalized = Links.normalizeServer(server) else {
                message = "Enter a server address like 203.0.113.7 or https://name.example.com"
                return
            }
            updated.serverUrl = normalized
        }
        store.upsert(updated)
        editing = nil
        reload()
        message = "Saved"
    }

    func delete(_ p: ConnectProfile) {
        store.delete(p.id)
        editing = nil
        reload()
    }

    func connect(_ p: ConnectProfile) {
        exitIP = nil
        Task {
            do {
                try await tunnel.connect(profileID: p.id)
            } catch {
                message = "Could not start the VPN: \(error.localizedDescription)"
            }
        }
    }

    func disconnect() {
        tunnel.disconnect()
        exitIP = nil
        Task {
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            reload()
        }
    }

    func checkExitIP() {
        checkingIP = true
        Task {
            defer { checkingIP = false }
            do {
                exitIP = try await tunnel.publicIP()
            } catch {
                message = "Could not check IP: \(error.localizedDescription)"
            }
        }
    }
}
