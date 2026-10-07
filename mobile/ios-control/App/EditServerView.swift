import SwiftUI

struct EditServerView: View {
    let profile: ConnectProfile
    var onSave: (_ name: String, _ server: String, _ newLink: String) -> Void
    var onDelete: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var server = ""
    @State private var link = ""
    @State private var confirmDelete = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Name") { TextField("Name", text: $name) }
                Section {
                    TextField("Server address", text: $server)
                        .keyboardType(.URL).textInputAutocapitalization(.never).autocorrectionDisabled()
                } header: { Text("Server address") } footer: {
                    Text("IP, domain or https URL. Updated automatically when it changes.")
                }
                Section {
                    TextField("New connect link (optional)", text: $link, axis: .vertical)
                        .lineLimit(2...4).textInputAutocapitalization(.never).autocorrectionDisabled()
                } footer: { Text("Paste a new link if the old one was revoked.") }
                Section {
                    Button("Delete \(profile.name)", role: .destructive) { confirmDelete = true }
                }
            }
            .navigationTitle("Edit saved server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) { Button("Save") { onSave(name, server, link) } }
            }
            .confirmationDialog("Delete '\(profile.name)' from this iPhone?", isPresented: $confirmDelete, titleVisibility: .visible) {
                Button("Yes, delete", role: .destructive, action: onDelete)
            }
            .onAppear {
                name = profile.name
                server = profile.serverUrl
            }
        }
    }
}
