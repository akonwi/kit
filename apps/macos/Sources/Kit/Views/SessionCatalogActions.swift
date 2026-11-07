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

/// What a rename or delete sheet says, by what it does.
struct SessionCatalogActionCopy: Equatable {
    let title: String
    /// The consequence a delete spells out: a saved session loses its
    /// history, and a temporary one, never saved, is disposed of.
    let message: String?
    let confirm: String
}

extension SessionCatalogActionCopy {
    init(session: SessionExcerpt, deleting: Bool) {
        let disposing = deleting && session.isTemporary == true
        self.init(title: deleting ? "Delete session?" : "Rename session",
                  message: disposing ? "“\(session.title)” will be removed from daemon memory and any active work will be cancelled. This cannot be undone."
                    : deleting ? "“\(session.title)” and its saved history will be permanently deleted. This cannot be undone." : nil,
                  confirm: deleting ? "Delete session" : "Rename")
    }
}

/// Renames, deletes, or disposes of a cataloged session through its
/// server, announcing a deletion so every window can let go of the session.
@MainActor enum SessionCatalogMutation {
    static func perform(session: SessionExcerpt, deleting: Bool, name: String, client: (any SessionClient)?) async throws {
        if deleting && session.isTemporary == true, let client = client as? any SessionDisposalClient {
            do { try await client.disposeSession(session.id) }
            catch ClientError.http(404) {}
            let identity = SessionIdentity(server: client.serverID, session: session.id)
            TemporarySessions.shared.forget(identity)
            NotificationCenter.default.post(name: .kitSessionDeleted, object: identity)
        } else if deleting && session.isTemporary != true, let client = client as? any SessionDeletionClient {
            try await SessionDeletion.perform(session.id, client: client)
            NotificationCenter.default.post(name: .kitSessionDeleted,
                object: SessionIdentity(server: client.serverID, session: session.id))
        } else if !deleting, let client = client as? any SessionNamingClient,
                  let name = SessionName.normalized(name) {
            _ = try await client.renameSession(session.id, name: name)
        } else { throw MutationNotSent(reason: "This server does not support this operation.") }
    }

    /// What the sheet says when the operation fails.
    static func failure(_ error: Error) -> String {
        if case ClientError.http(409) = error {
            "This session is in use and cannot be deleted. Stop its active work before trying again."
        } else { error.localizedDescription }
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

    var body: some View {
        let copy = SessionCatalogActionCopy(session: session, deleting: deleting)
        VStack(alignment: .leading, spacing: 16) {
            Text(copy.title)
                .font(.kit(size: 16, weight: .medium))
            if let message = copy.message {
                Text(message)
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
                Button(copy.confirm, role: deleting ? .destructive : nil) { perform() }
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
                try await SessionCatalogMutation.perform(session: session, deleting: deleting, name: name, client: client)
                await refresh()
                dismiss()
            } catch {
                self.error = SessionCatalogMutation.failure(error)
                await refresh()
                pending = false
                focused = !deleting
            }
        }
    }
}
