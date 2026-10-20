import SwiftUI

/// Discover sessions to open in the current window group.
struct SessionPickerView: View {
    @Environment(\.mica) private var theme
    let client: any SessionClient
    let currentID: String
    let close: () -> Void
    let open: (SessionExcerpt) -> Void
    @State private var catalog: SessionCatalog
    @State private var target: SessionExcerpt?
    @State private var deleting = false

    init(client: any SessionClient, currentID: String, close: @escaping () -> Void, open: @escaping (SessionExcerpt) -> Void) {
        self.client = client
        self.currentID = currentID
        self.close = close
        self.open = open
        _catalog = State(initialValue: SessionCatalog(client: client))
    }

    var body: some View {
        PaletteSheet(title: "Open session", height: PaletteMetrics.height) {
            SessionLauncherContent(sessions: catalog.sessions, connecting: catalog.loading,
                connected: catalog.loaded, error: catalog.error, open: open,
                refresh: { Task { await catalog.refresh() } },
                focusRequest: target == nil, compact: true, currentID: currentID,
                rename: { deleting = false; target = $0 },
                delete: { deleting = true; target = $0 })
        }
        .onExitCommand(perform: close)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .modifier(SessionCatalogActions(client: client, refresh: { await catalog.refresh() },
                                        target: $target, deleting: $deleting))
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            if let identity = notification.object as? SessionIdentity { catalog.sessionDeleted(identity) }
        }
        .task { await catalog.load() }
    }
}
