import SwiftUI

struct RenameSessionView: View {
    @Bindable var state: SessionStore
    var initialName = ""
    /// Saves on appear, for a name typed after `name`.
    var submitImmediately = false
    var back: () -> Void
    @Environment(\.mica) private var theme
    @State private var name = ""
    @State private var saving = false
    @State private var dismissed = false
    @State private var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        PaletteSheet(title: "Rename session", detail: state.selected?.title) {
            VStack(alignment: .leading, spacing: 10) {
                PaletteTextInput(prompt: "Session name", text: $name)
                    .focused($focused).onSubmit { save() }
                    .disabled(saving)
                if let error {
                    Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger).fixedSize(horizontal: false, vertical: true)
                }
                if !name.isEmpty && SessionName.normalized(name) == nil {
                    Text("Use a non-empty name without control characters, up to 256 bytes.")
                        .font(.kit(size: 12)).foregroundStyle(theme.warning)
                }
            }.padding(16)
        } actions: {
            Button("Back", action: back).disabled(saving)
            Spacer()
            if saving { KitSpinner() }
            Button("Cancel", action: dismiss)
            Button("Rename", action: save)
                .buttonStyle(MicaButtonStyle(primary: true, compact: true))
                .disabled(saving || SessionName.normalized(name) == nil || state.renameUnavailableReason != nil)
        }
        .buttonStyle(MicaButtonStyle(compact: true))
        .task {
            name = initialName
            if submitImmediately { save() }
            // Wait for the palette search field to leave the responder chain.
            do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
            if !saving { focused = true }
        }
        .onExitCommand(perform: dismiss)
        .onDisappear { dismissed = true }
    }

    private func dismiss() {
        dismissed = true
        state.ui.palette = false
    }

    private func save() {
        guard !saving, let name = SessionName.normalized(name), state.renameUnavailableReason == nil else { return }
        saving = true; error = nil
        Task { @MainActor in
            do {
                try await state.renameSession(name)
                if !dismissed { state.ui.palette = false }
            } catch {
                self.error = error.localizedDescription
                saving = false
                if !dismissed { focused = true }
            }
            saving = false
        }
    }
}
