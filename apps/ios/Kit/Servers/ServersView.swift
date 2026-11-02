import KitClient
import SwiftUI

/// The paired servers with their state, and the ways to pair another.
struct ServersView: View {
    @Environment(ServerStore.self) private var store
    @Environment(PairingCoordinator.self) private var pairing
    @State private var removing: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(store.entries) { entry in
                        NavigationLink(value: entry.id) {
                            ServerRow(entry: entry)
                        }
                        .swipeActions {
                            Button("Remove", role: .destructive) { removing = entry.id }
                        }
                    }
                }
                .listRowBackground(Theme.raised)
                Section {
                    Button { pairing.scan() } label: {
                        Label("Scan pairing code", systemImage: "qrcode.viewfinder")
                    }
                    Button { pairing.paste() } label: {
                        Label("Paste pairing link", systemImage: "link")
                    }
                }
                .listRowBackground(Theme.raised)
                .foregroundStyle(Theme.accent)
            }
            .scrollContentBackground(.hidden)
            .background(Theme.surface)
            .navigationTitle("Servers")
            .navigationDestination(for: String.self) { name in
                ServerDetailView(name: name)
            }
            .refreshable { await store.checkAll() }
            .alert(removing.map { "Remove \($0)?" } ?? "", isPresented: removingBinding) {
                Button("Cancel", role: .cancel) {}
                Button("Remove", role: .destructive) {
                    if let removing { try? store.remove(removing) }
                }
            } message: {
                Text("This iPhone forgets the server and its token. The server still lists this phone until you revoke it with kit server devices.")
            }
        }
    }

    private var removingBinding: Binding<Bool> {
        Binding(get: { removing != nil }, set: { if !$0 { removing = nil } })
    }
}

/// One server: its state, how it's reached, and how many sessions it has.
struct ServerRow: View {
    let entry: ServerStore.Entry

    var body: some View {
        HStack(spacing: 12) {
            ServerStateIndicator(state: entry.state)
            VStack(alignment: .leading, spacing: 2) {
                Text(entry.id)
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(Theme.text)
                detail
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
            if let sessions = entry.sessions, showsSessions {
                Text("\(sessions)")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
                    .accessibilityLabel("\(sessions) sessions")
            }
        }
        .padding(.vertical, 2)
    }

    private var showsSessions: Bool {
        switch entry.state {
        case .online, .unreachable: true
        default: false
        }
    }

    @ViewBuilder
    private var detail: some View {
        switch entry.state {
        case .online(let version):
            Text("\(ServerAddress.display(entry.url)) · Kit \(version)")
                .font(.system(size: 12, design: .monospaced))
                .foregroundStyle(Theme.muted)
        case .unreachable(let since):
            TimelineView(.periodic(from: .now, by: 30)) { context in
                Text("Can’t reach \(ServerAddress.display(entry.url)) · tried \(Self.elapsed(since, now: context.date))")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
            }
        default:
            Text(ServerStatePresentation.summary(entry.state))
                .font(.system(size: 13))
                .foregroundStyle(ServerStatePresentation.isProblem(entry.state) ? Theme.warning : Theme.muted)
        }
    }

    static func elapsed(_ since: Date, now: Date) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(since)))
        if seconds < 60 { return "just now" }
        if seconds < 3600 { return "\(seconds / 60)m ago" }
        if seconds < 86400 { return "\(seconds / 3600)h ago" }
        return "\(seconds / 86400)d ago"
    }
}

/// The dot before a server's name.
struct ServerStateIndicator: View {
    let state: ServerState

    var body: some View {
        Group {
            switch state {
            case .connecting:
                ProgressView()
                    .controlSize(.mini)
            case .online:
                Circle().fill(Theme.success)
            case .unreachable:
                Circle().strokeBorder(Theme.strongBorder, lineWidth: 1.5)
            case .needsPairing, .serverNeedsUpdate, .appNeedsUpdate, .failed:
                Circle().fill(Theme.warning)
            }
        }
        .frame(width: 10, height: 10)
        .accessibilityHidden(true)
    }
}

/// Words for a server's state.
enum ServerStatePresentation {
    static func summary(_ state: ServerState) -> String {
        switch state {
        case .connecting: "Connecting…"
        case .online(let version): "Kit \(version)"
        case .unreachable: "Can’t reach the server"
        // The server answers alike for an expired pairing and a revoked one.
        case .needsPairing(.expired), .needsPairing(.refused): "No longer paired. Pair again."
        case .needsPairing(.missing): "Not paired. Pair again."
        case .serverNeedsUpdate(let version, _): "Kit \(version) needs an update"
        case .appNeedsUpdate(let version, _): "Kit \(version) needs a newer app"
        case .failed(let message): message
        }
    }

    static func isProblem(_ state: ServerState) -> Bool {
        switch state {
        case .needsPairing, .serverNeedsUpdate, .appNeedsUpdate, .failed: true
        default: false
        }
    }
}
