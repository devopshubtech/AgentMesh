import PhotosUI
import SwiftUI

/// "Connect to remote server": scan the QR code, pick a picture of it from
/// Photos, or type the 6-digit pairing code (or paste the link).
struct AddServerView: View {
    let busy: Bool
    var onSubmit: (String) -> Void
    var onLink: (String) -> Void
    var onError: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    @State private var scanning = false
    @State private var photo: PhotosPickerItem?
    @State private var reading = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Button { scanning = true } label: {
                        Label("Scan QR code", systemImage: "qrcode.viewfinder").frame(maxWidth: .infinity, minHeight: 44)
                    }
                    .disabled(busy)
                    PhotosPicker(selection: $photo, matching: .images) {
                        Label(reading ? "Reading picture…" : "Choose QR code from Photos", systemImage: "photo.on.rectangle")
                            .frame(maxWidth: .infinity, minHeight: 44)
                    }
                    .disabled(busy || reading)
                } footer: {
                    Text("Tip: take a screenshot of the QR code from AgentMesh → Connect a phone, then choose it here.")
                }
                Section {
                    TextField("123 456", text: $text)
                        .font(.system(size: 24, weight: .medium, design: .monospaced))
                        .keyboardType(.numbersAndPunctuation)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .disabled(busy)
                    Button(busy ? "Pairing…" : "Connect") { onSubmit(text.trimmingCharacters(in: .whitespacesAndNewlines)) }
                        .disabled(text.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                    if busy { ProgressView() }
                } header: {
                    Text("Or enter the 6-digit pairing code (or paste the link)")
                } footer: {
                    Text("Get the code from the dashboard → Connect a phone → Pairing code.")
                }
            }
            .navigationTitle("Connect to remote server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
            .fullScreenCover(isPresented: $scanning) {
                ZStack(alignment: .topTrailing) {
                    QRScannerView(onCode: { code in
                        scanning = false
                        onLink(code)
                    }, onError: { msg in
                        scanning = false
                        onError(msg)
                    })
                    .ignoresSafeArea()
                    Button("Close") { scanning = false }
                        .buttonStyle(.borderedProminent).padding()
                }
            }
            .onChange(of: photo) { item in
                guard let item else { return }
                reading = true
                Task {
                    let data = try? await item.loadTransferable(type: Data.self)
                    reading = false
                    photo = nil
                    if let data, let code = QRImageReader.read(data) {
                        onLink(code)
                    } else {
                        onError("No QR code found in that picture. Choose a clear screenshot of the QR code from Connect a phone.")
                    }
                }
            }
        }
    }
}
