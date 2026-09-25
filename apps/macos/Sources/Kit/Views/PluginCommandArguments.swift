import SwiftUI

/// Arguments belong to the palette selection, never to the composer's draft.
struct PluginCommandArguments: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let command: PluginCommand
    let back: () -> Void
    @State private var arguments = ""
    @FocusState private var focused: Bool
    private var available: Bool {
        state.pluginCommands.contains { $0.selectionID == command.selectionID }
    }
    private var reason: String? {
        !available ? "This command changed. Go back and select it again." : state.pluginCommandUnavailableReason
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Button("Back", action: back).buttonStyle(.plain)
            Text("/" + command.name).font(.kit(size: 16, weight: .medium))
            Text(command.description).foregroundStyle(theme.muted)
            TextField(command.argName ?? "Arguments (optional)", text: $arguments)
                .textFieldStyle(.roundedBorder).focused($focused).onSubmit(execute)
            if let reason { Text(reason).foregroundStyle(theme.muted) }
            Spacer()
            HStack {
                Button("Cancel") { state.ui.palette = false }.keyboardShortcut(.cancelAction)
                Spacer()
                Button("Run", action: execute).disabled(reason != nil).keyboardShortcut(.defaultAction)
            }
        }
        .padding(20).frame(width: 520, height: 280)
        .foregroundStyle(theme.text).background(theme.surface)
        .task { focused = true }
    }
    private func execute() {
        guard reason == nil else { return }
        state.ui.palette = false
        Task { await state.runPluginCommand(command, args: arguments) }
    }
}
