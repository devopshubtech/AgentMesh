import SwiftUI

@main
struct AgentMeshApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            ContentView(model: model, tunnel: model.tunnel)
                // agentmesh://join?u=...&k=...&r=... from the connect web page (join.html).
                .onOpenURL { url in model.importLink(url.absoluteString) }
        }
    }
}
