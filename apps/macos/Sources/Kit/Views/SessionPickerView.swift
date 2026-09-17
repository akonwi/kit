import SwiftUI

/// Discover sessions to open in the current window group.
struct SessionPickerView: View {
    @Environment(\.mica) private var theme
    let client: any SessionClient
    let currentID: String
    let close: () -> Void
    let open: (SessionExcerpt) -> Void
    @State private var catalog = AppModel()
    @State private var target: SessionExcerpt?
    @State private var deleting = false

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Open session").font(.kit(size: 16, weight: .medium))
                Spacer()
                Button { close() } label: { Image(systemName: "xmark") }
                    .buttonStyle(.plain).keyboardShortcut(.cancelAction).accessibilityLabel("Close session picker")
            }.padding(20)
            SessionLauncherContent(sessions: catalog.sessions, connecting: catalog.connecting,
                connected: catalog.client != nil, error: catalog.error, open: open,
                refresh: { Task { await catalog.refreshSessions() } },
                focusRequest: target == nil, compact: true, currentID: currentID,
                rename: { deleting = false; target = $0 },
                delete: { deleting = true; target = $0 })
        }
        .frame(width: 620, height: 490)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .modifier(SessionCatalogActions(client: client, refresh: { await catalog.refreshSessions() },
                                        target: $target, deleting: $deleting))
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            if let identity = notification.object as? SessionIdentity { catalog.sessionDeleted(identity) }
        }
        .task { await catalog.connectLocal(using: client) }
    }
}
