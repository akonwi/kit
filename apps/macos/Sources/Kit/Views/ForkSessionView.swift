import SwiftUI

/// Forks the session. With a message typed after `fork`, it submits as soon
/// as it opens; the fields stay editable so a failure can be retried.
struct ForkSessionView: View {
    @Bindable var state: SessionStore
    var initialMessage = ""
    var submitImmediately = false
    let back: () -> Void
    let open: (String) -> Void
    @Environment(\.mica) private var theme
    @State private var name = ""
    @State private var message = ""
    @State private var operation = SessionForkOperation()
    @State private var dismissed = false
    @FocusState private var focused: Bool

    var body: some View {
        PaletteSheet(title: "Fork session", detail: state.selected?.title) {
            VStack(alignment: .leading, spacing: 10) {
                Text("Continue from this session’s current history in a separate session. A first message starts the fork’s first turn.")
                    .font(.kit(size: 12)).foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                PaletteTextInput(prompt: "Name (optional)", text: $name)
                    .focused($focused).onSubmit(submit)
                    .disabled(operation.pending)
                PaletteTextInput(prompt: "First message (optional)", text: $message, axis: .vertical)
                    .lineLimit(3...8).onSubmit(submit)
                    .disabled(operation.pending)
                if let error = operation.error {
                    Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger).fixedSize(horizontal: false, vertical: true)
                }
            }.padding(16)
        } actions: {
            Button("Back", action: back).disabled(operation.pending)
            Spacer()
            if operation.pending { KitSpinner() }
            Button("Cancel", action: dismiss)
            Button(operation.error == nil ? "Fork" : "Retry", action: submit)
                .buttonStyle(MicaButtonStyle(primary: true, compact: true))
                .disabled(operation.pending || state.forkUnavailableReason != nil)
        }
        .buttonStyle(MicaButtonStyle(compact: true))
        .task {
            message = initialMessage
            if submitImmediately { submit() }
            do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
            focused = true
        }
        .onExitCommand(perform: dismiss)
        .onDisappear { dismissed = true }
    }

    private func dismiss() { dismissed = true; state.ui.palette = false }
    private func submit() {
        guard let client = state.forkClient, !operation.pending, state.forkUnavailableReason == nil else { return }
        let source = state.selectedID
        let message = message
        Task { @MainActor in
            guard let forked = await operation.submit(client: client, source: source, name: name, message: message) else { return }
            if let error = forked.firstTurnError {
                ForkFirstTurnFailures.shared.record(.init(message: message.trimmingCharacters(in: .whitespacesAndNewlines), error: error),
                                                    for: SessionIdentity(server: state.serverID, session: forked.session.id))
            }
            if dismissed {
                // The palette closed while the fork ran; the fork still exists.
                state.feedback.show(title: "Session forked", detail: forked.session.title,
                                    actionTitle: "Open", action: { open(forked.session.id) })
            } else {
                state.ui.palette = false
                open(forked.session.id)
            }
        }
    }
}
