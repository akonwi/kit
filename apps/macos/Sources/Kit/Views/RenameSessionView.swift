import SwiftUI

struct RenameSessionView: View {
    @Bindable var state: SessionStore
    var back: () -> Void
    @Environment(\.mica) private var theme
    @State private var name = ""
    @State private var saving = false
    @State private var dismissed = false
    @State private var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Rename session").font(.kit(size: 16, weight: .medium))
            TextField("Session name", text: $name)
                .textFieldStyle(.roundedBorder).focused($focused)
                .accessibilityLabel("Session name").onSubmit { save() }
                .onExitCommand(perform: dismiss)
                .disabled(saving)
            if let error { Text(error).foregroundStyle(theme.danger).fixedSize(horizontal: false, vertical: true) }
            if !name.isEmpty && SessionName.normalized(name) == nil {
                Text("Use a non-empty name without control characters, up to 256 bytes.").foregroundStyle(theme.warning)
            }
            HStack {
                Button("Back", action: back).disabled(saving)
                Spacer()
                if saving { KitSpinner() }
                Button("Cancel", action: dismiss).keyboardShortcut(.cancelAction)
                Button("Rename", action: save)
                    .disabled(saving || SessionName.normalized(name) == nil || state.renameUnavailableReason != nil)
            }
        }
        .font(.kit(size: 13)).padding(20).frame(width: 520)
        .foregroundStyle(theme.text).background(theme.surface)
        .task {
            name = state.selected?.title ?? ""
            // Wait for the palette search field to leave the responder chain.
            do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
            focused = true
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
