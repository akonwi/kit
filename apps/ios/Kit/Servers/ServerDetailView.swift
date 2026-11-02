import KitClient
import SwiftUI

/// One paired server: its state, how to pair again, and removal.
struct ServerDetailView: View {
    @Environment(ServerStore.self) private var store
    @Environment(PairingCoordinator.self) private var pairing
    @Environment(\.dismiss) private var dismiss
    @State private var confirmingRemoval = false

    let name: String

    var body: some View {
        Form {
            if let entry = store.entry(name) {
                Section {
                    LabeledContent("Status") {
                        HStack(spacing: 8) {
                            ServerStateIndicator(state: entry.state)
                            Text(status(entry))
                                .foregroundStyle(ServerStatePresentation.isProblem(entry.state) ? Theme.warning : Theme.muted)
                        }
                    }
                    LabeledContent("Address") {
                        Text(ServerAddress.display(entry.url))
                            .font(.system(size: 15, design: .monospaced))
                    }
                    if let sessions = entry.sessions {
                        LabeledContent("Sessions", value: "\(sessions)")
                    }
                } footer: {
                    if let explanation = explanation(entry) { Text(explanation) }
                }
                .listRowBackground(Theme.raised)

                Section {
                    Button("Check now") { Task { await store.check(name) } }
                    Button("Pair again with a code") { pairing.scan(target: name) }
                    Button("Pair again with a link") { pairing.paste(target: name) }
                }
                .listRowBackground(Theme.raised)

                Section {
                    Button("Remove server", role: .destructive) { confirmingRemoval = true }
                }
                .listRowBackground(Theme.raised)
            }
        }
        .scrollContentBackground(.hidden)
        .background(Theme.surface)
        .navigationTitle(name)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await store.check(name) }
        .alert("Remove \(name)?", isPresented: $confirmingRemoval) {
            Button("Cancel", role: .cancel) {}
            Button("Remove", role: .destructive) {
                try? store.remove(name)
                dismiss()
            }
        } message: {
            Text("This iPhone forgets the server and its token. The server still lists this phone until you revoke it with kit server devices.")
        }
    }

    private func status(_ entry: ServerStore.Entry) -> String {
        switch entry.state {
        case .online: "Online"
        case .unreachable(let since): "Can’t reach · tried \(ServerRow.elapsed(since, now: .now))"
        default: ServerStatePresentation.summary(entry.state)
        }
    }

    private func explanation(_ entry: ServerStore.Entry) -> String? {
        switch entry.state {
        case .online(let version): "Kit \(version)"
        case .unreachable: ServerAddress.reachabilityHint(entry.url)
        case .needsPairing: "Run kit server pair on the server, then pair again."
        case .serverNeedsUpdate(let version, let serverProtocol):
            "\(name) runs Kit \(version) (protocol \(serverProtocol)). This app speaks protocol \(kitWireVersion). Upgrade Kit on the server."
        case .appNeedsUpdate(let version, let serverProtocol):
            "\(name) runs Kit \(version) (protocol \(serverProtocol)). This app speaks protocol \(kitWireVersion). Update Kit on this iPhone."
        case .connecting, .failed: nil
        }
    }
}
