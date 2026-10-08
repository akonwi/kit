import SwiftUI

struct ForkSessionView: View {
    @Bindable var state: SessionStore
    let back: () -> Void
    let open: (String) -> Void
    @Environment(\.mica) private var theme
    @State private var name = ""
    @State private var message = ""
    @State private var operation = SessionForkOperation()
    @State private var dismissed = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Fork session").font(.kit(size: 16, weight: .medium))
            Text("Continue from this session’s current history in a separate session. A first message starts the fork’s first turn.").foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            TextField("Name (optional)", text: $name)
                .textFieldStyle(.roundedBorder).focused($focused).accessibilityLabel("Fork name")
                .onSubmit(submit).onExitCommand(perform: dismiss)
                .disabled(operation.pending)
            TextField("First message (optional)", text: $message, axis: .vertical)
                .lineLimit(3...8)
                .textFieldStyle(.roundedBorder).accessibilityLabel("First message")
                .onSubmit(submit).onExitCommand(perform: dismiss)
                .disabled(operation.pending)
            if let error = operation.error { Text(error).foregroundStyle(theme.danger).fixedSize(horizontal: false, vertical: true) }
            HStack {
                Button("Back", action: back).disabled(operation.pending)
                Spacer()
                if operation.pending { KitSpinner() }
                Button("Cancel", action: dismiss).keyboardShortcut(.cancelAction)
                Button("Fork", action: submit)
                    .disabled(operation.pending || state.forkUnavailableReason != nil)
            }
        }.font(.kit(size: 13)).padding(20).frame(width: 520)
            .foregroundStyle(theme.text).background(theme.surface)
            .task {
                do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
                focused = true
            }
            .onExitCommand(perform: dismiss)
            .onDisappear { dismissed = true }
    }

    private func dismiss() { dismissed = true; state.ui.palette = false }
    private func submit() {
        guard let client = state.forkClient, !operation.pending else { return }
        let source = state.selectedID
        Task { @MainActor in
            if let forked = await operation.submit(client: client, source: source, name: name, message: message), !dismissed {
                if let error = forked.firstTurnError {
                    ForkFirstTurnFailures.shared.record(.init(message: message.trimmingCharacters(in: .whitespacesAndNewlines), error: error),
                                                        for: SessionIdentity(server: state.serverID, session: forked.session.id))
                }
                state.ui.palette = false
                open(forked.session.id)
            }
        }
    }
}
