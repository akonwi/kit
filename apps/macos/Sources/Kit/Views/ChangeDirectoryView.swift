import SwiftUI

struct ChangeDirectoryView: View {
    @Bindable var state: SessionStore
    var initialPath = ""
    /// Runs the change on appear, for a path typed after `cd`.
    var submitImmediately = false
    let back: () -> Void
    @Environment(\.mica) private var theme
    @State private var path = ""
    @State private var dismissed = false
    /// Set when a typed path waits behind an unresolved earlier change.
    @State private var notice: String?
    @FocusState private var focused: Bool

    var body: some View {
        let operation = state.directoryChange
        PaletteSheet(title: "Change working directory", detail: state.selected?.cwd ?? state.selected?.workspace) {
            VStack(alignment: .leading, spacing: 10) {
                PaletteTextInput(prompt: "Directory on the session’s server", text: $path)
                    .focused($focused)
                    .disabled(operation.pending || operation.request != nil)
                    .onSubmit { submit() }
                if let notice {
                    Text(notice).font(.kit(size: 12)).foregroundStyle(theme.warning).fixedSize(horizontal: false, vertical: true)
                }
                if let error = operation.error {
                    Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger).fixedSize(horizontal: false, vertical: true)
                }
            }.padding(16)
        } actions: {
            Button("Back", action: back).disabled(operation.pending)
            Spacer()
            if operation.pending { KitSpinner() }
            Button("Cancel") { state.ui.palette = false }
            Button(operation.request == nil ? "Change directory" : "Retry") { submit() }
                .buttonStyle(MicaButtonStyle(primary: true, compact: true))
                .disabled(operation.pending || DirectoryChangeOperation.path(path) == nil || state.directoryUnavailableReason != nil)
        }
        .buttonStyle(MicaButtonStyle(compact: true))
        .onExitCommand { state.ui.palette = false }
        .task {
            if let retained = state.directoryChange.request?.path {
                // An earlier change may have reached the server; it must be
                // resolved before another path is sent.
                path = retained
                if submitImmediately {
                    if DirectoryChangeOperation.path(initialPath) == retained { submit() }
                    else { notice = "Resolve the earlier change to \(retained) before changing to \(initialPath)." }
                }
            } else {
                path = initialPath
                if submitImmediately { submit() }
            }
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
