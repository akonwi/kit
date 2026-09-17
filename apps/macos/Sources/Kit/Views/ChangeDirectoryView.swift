import SwiftUI

struct ChangeDirectoryView: View {
    @Bindable var state: SessionStore
    let back: () -> Void
    @Environment(\.mica) private var theme
    @State private var path = ""
    @State private var dismissed = false
    @FocusState private var focused: Bool

    var body: some View {
        let operation = state.directoryChange
        VStack(alignment: .leading, spacing: 16) {
            Text("Change working directory").font(.kit(size: 16, weight: .medium))
            Text("Current: " + (state.selected?.cwd ?? state.selected?.workspace ?? ""))
                .font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.muted)
                .textSelection(.enabled)
            TextField("Directory on the session’s server", text: $path)
                .textFieldStyle(.roundedBorder).focused($focused)
                .disabled(operation.pending || operation.request != nil)
                .onSubmit { submit() }
            if let error = operation.error {
                Text(error).foregroundStyle(theme.danger).fixedSize(horizontal: false, vertical: true)
            }
            HStack {
                Button("Back", action: back).disabled(operation.pending)
                Spacer()
                if operation.pending { KitSpinner() }
                Button("Cancel") { state.ui.palette = false }.keyboardShortcut(.cancelAction)
                Button(operation.request == nil ? "Change directory" : "Retry") { submit() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(operation.pending || DirectoryChangeOperation.path(path) == nil || state.directoryUnavailableReason != nil)
            }
        }
        .font(.kit(size: 13)).padding(20).frame(width: 520)
        .foregroundStyle(theme.text).background(theme.surface)
        .task {
            path = state.directoryChange.request?.path ?? ""
            do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
            focused = true
        }
        .onDisappear { dismissed = true }
    }

    private func submit() {
        Task { @MainActor in
            if await state.changeDirectory(path), !dismissed { state.ui.palette = false }
        }
    }
}
