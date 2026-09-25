import SwiftUI

/// Shared catalog operations; the server decides whether a session can be deleted.
struct SessionCatalogActions: ViewModifier {
    let client: (any SessionClient)?
    let refresh: () async -> Void
    @Binding var target: SessionExcerpt?
    @Binding var deleting: Bool

    func body(content: Content) -> some View {
        content.sheet(item: $target) { session in
            SessionCatalogActionView(client: client, session: session, deleting: deleting) {
                await refresh()
            }
        }
    }
}

private struct SessionCatalogActionView: View {
    let client: (any SessionClient)?
    let session: SessionExcerpt
    let deleting: Bool
    let refresh: () async -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.mica) private var theme
    @State private var name = ""
    @State private var pending = false
    @State private var error: String?
    @FocusState private var focused: Bool

    private var disposing: Bool { deleting && session.isTemporary == true }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(disposing ? "Dispose temporary session?" : deleting ? "Delete session?" : "Rename session")
                .font(.kit(size: 16, weight: .medium))
            if deleting {
                Text(disposing ? "“\(session.title)” will be removed from daemon memory and any active work will be cancelled. This cannot be undone." : "“\(session.title)” and its saved history will be permanently deleted. This cannot be undone.")
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                TextField("Session name", text: $name).textFieldStyle(.roundedBorder)
                    .focused($focused).onSubmit { perform() }.disabled(pending)
            }
            if let error { Text(error).foregroundStyle(theme.danger).fixedSize(horizontal: false, vertical: true) }
            HStack {
                if pending { KitSpinner() }
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction).disabled(pending)
                Button(disposing ? "Dispose session" : deleting ? "Delete session" : "Rename", role: deleting ? .destructive : nil) { perform() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(pending || (!deleting && SessionName.normalized(name) == nil))
            }
        }
        .font(.kit(size: 13)).padding(24).frame(width: 440)
        .foregroundStyle(theme.text).background(theme.surface)
        .interactiveDismissDisabled(pending)
        .task {
            name = session.title
            do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
            focused = !deleting
        }
    }

    private func perform() {
        guard !pending else { return }
        pending = true; error = nil
        Task { @MainActor in
            do {
                if disposing, let client = client as? any SessionDisposalClient {
                    do { try await client.disposeSession(session.id) }
                    catch ClientError.http(404) {}
                    let identity = SessionIdentity(server: client.serverID, session: session.id)
                    TemporarySessions.shared.forget(identity)
                    NotificationCenter.default.post(name: .kitSessionDeleted, object: identity)
                } else if deleting, let client = client as? any SessionDeletionClient {
                    try await SessionDeletion.perform(session.id, client: client)
                    NotificationCenter.default.post(name: .kitSessionDeleted,
                        object: SessionIdentity(server: client.serverID, session: session.id))
                } else if !deleting, let client = client as? any SessionNamingClient,
                          let name = SessionName.normalized(name) {
                    _ = try await client.renameSession(session.id, name: name)
                } else { throw MutationNotSent(reason: "This server does not support this operation.") }
                await refresh()
                dismiss()
            } catch {
                if case ClientError.http(409) = error {
                    self.error = "This session is in use and cannot be deleted. Stop its active work before trying again."
                } else { self.error = error.localizedDescription }
                await refresh()
                pending = false
                focused = !deleting
            }
        }
    }
}
