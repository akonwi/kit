import SwiftUI

/// One connection owner per restored scene; a server switch in another window
/// must never redirect this window's session requests.
struct SessionWindowRoot: View {
    @Binding var request: SessionWindowRequest
    let windows: SessionWindowRegistry
    @State private var app = AppModel()
    @State private var localConnectionRequest = 0
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(spacing: 0) {
            if let error = windows.restoration.error {
                HStack {
                    Text(error).textSelection(.enabled)
                    Button("Retry saving") { windows.restoration.save() }
                }.font(.caption).padding(12)
            }
            if request.sessionID.isEmpty && ["", "local-v2"].contains(request.serverID) {
                SessionLauncherView(app: app) { session in
                    request = SessionWindowRequest(serverID: app.client?.serverID ?? "local-v2", sessionID: session.id)
                }
            } else if let client = app.client,
               request.serverID.isEmpty || request.serverID == client.serverID {
                SessionScreen(fixture: Fixture(sessions: app.sessions), sessionID: request.sessionID,
                              windows: windows, client: client) { id in
                    request = SessionWindowRequest(serverID: client.serverID, sessionID: id)
                }.id(client.serverID)
            } else {
                VStack(spacing: 0) {
                    if !request.sessionID.isEmpty {
                        Text("Reopen session").font(.title2).padding(.top, 24)
                        Text("\(request.serverID) · \(request.sessionID)")
                            .font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                    }
                    if !["", "local-v2"].contains(request.serverID) {
                        Text("This server is not configured.")
                            .foregroundStyle(.secondary).padding()
                    }
                    if ["", "local-v2"].contains(request.serverID) {
                        ConnectionView(app: app)
                    } else {
                        Button("Connect to local server") { localConnectionRequest += 1 }
                            .disabled(app.connecting)
                        if let error = app.error { Text(error).foregroundStyle(.secondary) }
                    }
                }
            }
        }
        .background {
            RestorationWindowProbe(registry: windows, request: request).frame(width: 0, height: 0)
        }
        .task(id: localConnectionRequest) {
            if localConnectionRequest > 0 { await app.connectLocal() }
        }
        .task(id: request.serverID) {
            await app.connectIfNeeded(to: request.serverID)
        }
        .onChange(of: app.client?.serverID) {
            if let server = app.client?.serverID, server != request.serverID {
                request = SessionWindowRequest(serverID: server, sessionID: "")
            }
        }
        .onAppear {
            windows.restoreRemaining { openWindow(id: "session", value: $0) }
        }
    }
}

private struct RestorationWindowProbe: NSViewRepresentable {
    let registry: SessionWindowRegistry
    let request: SessionWindowRequest
    func makeNSView(context: Context) -> SessionWindowBridge.WindowProbe { .init() }
    func updateNSView(_ view: SessionWindowBridge.WindowProbe, context: Context) {
        view.prepare = { window in
            registry.prepareTabbing(window, request: request)
        }
        view.configure = { window in
            registry.register(window, request: request)
        }
        view.scheduleConfiguration()
    }
}
