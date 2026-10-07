import SwiftUI

private let muted = Color.secondary
private let good = Color(red: 0.086, green: 0.639, blue: 0.290)

struct ContentView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var tunnel: TunnelController

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    StatusCard(status: tunnel.status, exitIP: model.exitIP, checking: model.checkingIP,
                               onCheckIP: model.checkExitIP, onStop: model.disconnect)
                    if model.profiles.isEmpty {
                        WelcomeCard { model.showAdd = true }
                    } else {
                        Text("Saved servers").font(.title3.weight(.semibold))
                        ForEach(model.profiles) { p in
                            ProfileRow(profile: p, active: isActive(p),
                                       onConnect: { model.connect(p) }, onEdit: { model.editing = p })
                        }
                        Button { model.showAdd = true } label: {
                            Text("+ Connect to remote server").frame(maxWidth: .infinity, minHeight: 44)
                        }
                        .buttonStyle(.bordered)
                    }
                    Text("AgentMesh v\(AppConfig.version)").font(.caption).foregroundStyle(muted).padding(.top, 8)
                }
                .padding(16)
            }
            .navigationTitle("AgentMesh")
            .refreshable { model.reload() }
        }
        .sheet(isPresented: $model.showAdd) {
            AddServerView(busy: model.pairing, onSubmit: model.submitCodeOrLink, onLink: { model.importLink($0) },
                          onError: { model.message = $0 })
        }
        .sheet(item: $model.editing) { p in
            EditServerView(profile: p, onSave: { model.saveEdit(p, name: $0, server: $1, newLink: $2) },
                           onDelete: { model.delete(p) })
        }
        .alert(model.message ?? "", isPresented: Binding(get: { model.message != nil }, set: { if !$0 { model.message = nil } })) {
            Button("OK", role: .cancel) {}
        }
        .onChange(of: tunnel.status.state) { _ in model.reload() }
    }

    private func isActive(_ p: ConnectProfile) -> Bool {
        tunnel.status.state == .connected && (tunnel.status.deviceName == p.deviceName || tunnel.status.deviceName == p.name)
    }
}

private struct WelcomeCard: View {
    var onAdd: () -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Use another device's internet").font(.title2.weight(.semibold))
            Text("Connect this iPhone to a computer running AgentMesh. Your internet will go out through that computer and websites will see its IP address — on mobile data or any Wi-Fi.")
                .font(.subheadline)
            Button(action: onAdd) {
                Text("Connect to remote server").font(.headline).frame(maxWidth: .infinity, minHeight: 50)
            }
            .buttonStyle(.borderedProminent)
            Text("You need the QR code or 6-digit pairing code from the AgentMesh dashboard → Connect a phone.")
                .font(.caption).foregroundStyle(muted)
        }
        .padding(20)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16))
    }
}

private struct ProfileRow: View {
    let profile: ConnectProfile
    let active: Bool
    var onConnect: () -> Void
    var onEdit: () -> Void

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(profile.name).font(.headline)
                let via = (!profile.deviceName.isEmpty && profile.deviceName != profile.name) ? "via \(profile.deviceName) · " : ""
                Text(via + profile.serverUrl.replacingOccurrences(of: "https://", with: ""))
                    .font(.caption).foregroundStyle(muted).lineLimit(1)
                if let last = profile.lastConnectedAt {
                    Text("Last connected \(last.formatted(date: .abbreviated, time: .shortened))")
                        .font(.caption2).foregroundStyle(muted)
                }
            }
            Spacer()
            Button("Edit", action: onEdit).buttonStyle(.borderless)
            if active {
                Text("Connected").font(.subheadline.weight(.semibold)).foregroundStyle(good)
            } else {
                Button("Connect", action: onConnect).buttonStyle(.borderedProminent)
            }
        }
        .padding(16)
        .background(active ? AnyShapeStyle(Color.accentColor.opacity(0.15)) : AnyShapeStyle(.thinMaterial),
                    in: RoundedRectangle(cornerRadius: 14))
        .contentShape(Rectangle())
        .onTapGesture { if !active { onConnect() } }
    }
}

private struct StatusCard: View {
    let status: TunnelStatus
    let exitIP: String?
    let checking: Bool
    var onCheckIP: () -> Void
    var onStop: () -> Void

    var body: some View {
        if case let (title, detail)? = texts {
            VStack(alignment: .leading, spacing: 8) {
                Text(title).font(.headline)
                if let detail { Text(detail).font(.footnote) }
                if status.state == .connecting || status.state == .reconnecting { ProgressView().frame(maxWidth: .infinity) }
                if status.state == .connected {
                    Text(exitIP.map { "Your IP now: \($0)" } ?? "Tap 'Check my IP' to see the IP websites see.")
                        .font(.subheadline.weight(exitIP == nil ? .regular : .semibold))
                }
                if status.state != .failed {
                    HStack {
                        if status.state == .connected {
                            Button(checking ? "Checking…" : "Check my IP", action: onCheckIP)
                                .buttonStyle(.bordered).disabled(checking)
                        }
                        Button("Disconnect", action: onStop).buttonStyle(.borderedProminent)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(16)
            .background(Color.accentColor.opacity(0.12), in: RoundedRectangle(cornerRadius: 16))
        }
    }

    private var texts: (String, String?)? {
        switch status.state {
        case .idle: return nil
        case .connecting: return ("Connecting to \(status.deviceName.isEmpty ? "server" : status.deviceName)…", nil)
        case .reconnecting: return ("Reconnecting to \(status.deviceName) (attempt \(status.attempt))…", status.reason)
        case .connected: return ("Connected — internet via \(status.deviceName)",
                                 "↑ \(humanBytes(status.bytesUp))   ↓ \(humanBytes(status.bytesDown))")
        case .failed: return ("Disconnected", status.reason)
        }
    }
}
