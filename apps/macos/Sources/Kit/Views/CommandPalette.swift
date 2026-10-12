import AppKit
import SwiftUI

/// The command palette: the TUI's catalog plus tile commands, run from one
/// search field where arguments are typed after the command name.
struct CommandPalette: View {
    @Environment(\.mica) private var theme
    @Environment(\.sessionNavigation) private var navigation
    @Environment(\.openSettings) private var openSettings
    @AppStorage("appearance") private var appearance = "system"
    @Bindable var state: SessionStore
    let openFork: (String) -> Void
    @State private var screen = Screen.commands
    @State private var disposalTarget: SessionExcerpt?
    @State private var deleting = true
    @State private var reloadSessionID: String?
    @State private var query = ""

    /// A nested screen the palette shows in place of the command list.
    enum Screen {
        case commands, details, mcp, sessions, model, thinking, tabs
        case directory(path: String, submit: Bool)
        case fork(message: String, submit: Bool)
        case rename(name: String, submit: Bool)
        case reload(SessionReloadOperation)
    }

    private var catalog: [PaletteCommand] {
        PaletteCommand.catalog(
            builtIns: PaletteCommand.builtIns(dark: theme.dark, scratchpad: state.canOpenScratchpad,
                                              temporary: state.isTemporary),
            prompts: state.selected?.promptCommands ?? [],
            plugins: state.pluginCommands
        ).map { row in
            var row = row
            row.unavailableReason = unavailableReason(row)
            return row
        }
    }

    var body: some View {
        Group {
            if state.ui.subagentsPresented {
                SubagentsPanel(state: state)
            } else {
                switch screen {
                case .commands: commandList
                case .details: SessionDetailsView(state: state, back: back)
                case .mcp: MCPStatusView(state: state, back: back)
                case .sessions:
                    SessionPickerView(client: state.catalogClient, currentID: state.selectedID, close: close) { session in
                        close()
                        openFork(session.id)
                    }
                case .model: PaletteChoices.model(state: state, close: close)
                case .thinking: PaletteChoices.thinking(state: state, close: close)
                case .tabs: PaletteChoices.tabs(state: state, close: close)
                case let .directory(path, submit):
                    ChangeDirectoryView(state: state, initialPath: path, submitImmediately: submit, back: back)
                case let .fork(message, submit):
                    ForkSessionView(state: state, initialMessage: message, submitImmediately: submit,
                                    back: back, open: openFork)
                case let .rename(name, submit):
                    RenameSessionView(state: state, initialName: name, submitImmediately: submit, back: back)
                case let .reload(operation): reloadProgress(operation: operation)
                }
            }
        }
        .environment(\.paletteDismiss, PaletteDismiss(action: close))
        .modifier(SessionCatalogActions(client: state.catalogClient, refresh: {
            await state.refreshSessions()
            if state.unavailable { close() }
        }, target: $disposalTarget, deleting: $deleting))
    }

    private var commandList: some View {
        PaletteListScreen(
            query: $query, placeholder: "Search commands…", catalog: catalog,
            emptyMessage: { "No commands match “\($0)”" },
            footer: "tab complete",
            complete: { $0.name + " " },
            activate: run, dismiss: close)
    }

    private func reloadProgress(operation: SessionReloadOperation) -> some View {
        PaletteSheet(title: "Reloading session context") {
            HStack(spacing: 10) {
                KitSpinner()
                Text(operation.progressLabel)
                Spacer()
            }.padding(16)
            Text("You can close the palette; the reload continues.")
                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                .padding(.horizontal, 16).padding(.bottom, 16)
        }
        .onExitCommand(perform: close)
    }

    private func close() { state.ui.palette = false }
    private func back() { screen = .commands }

    // MARK: Availability

    private func unavailableReason(_ row: PaletteCommand) -> String? {
        if row.prompt != nil { return state.palettePromptUnavailableReason }
        if row.plugin != nil { return state.pluginCommandUnavailableReason }
        let workspace = state.ui.workspace
        return switch row.id {
        case "cd": state.directoryUnavailableReason
        case "compact": state.compactionUnavailableReason
        case "fork": state.forkUnavailableReason
        case "name": state.renameUnavailableReason
        case "refresh-models": modelRefreshUnavailableReason
        case "reload": state.reloadOperation.pending ? state.reloadOperation.progressLabel : state.reloadUnavailableReason
        case "model", "thinking": configurationUnavailableReason
        case "diff": state.catalogClient is any DiffClient ? nil : "Unavailable for this connection"
        case "files": state.unavailable ? "Session unavailable" : nil
        // Provider sign-in has no session API yet (#153).
        case "login": "not available yet"
        case "split-right", "split-down": workspace.canSplit(workspace.selected) ? nil : "needs another tab"
        case "zoom", "equalize", "join-tabs": workspace.root.tiles.count < 2 ? "needs two tiles" : nil
        default: nil
        }
    }

    private var modelRefreshUnavailableReason: String? {
        if !(state.catalogClient is any ModelCatalogRefreshClient) { return "Unavailable for this connection" }
        if state.unavailable { return "Session unavailable" }
        if state.connectionState != .connected { return "Connect to refresh" }
        if state.running { return "idle only" }
        if state.modelCatalogRefreshPending { return "Refreshing model catalog" }
        return nil
    }

    private var configurationUnavailableReason: String? {
        if state.composerClient == nil { return "Unavailable for this connection" }
        if state.unavailable { return "Session unavailable" }
        if state.connectionState != .connected { return "Connect to change" }
        if state.configuration.changing { return "Wait for the current change" }
        return nil
    }

    // MARK: Running

    private func run(_ row: PaletteCommand) {
        // Availability is read live: the row's reason is from the last render.
        if let reason = unavailableReason(row) {
            state.feedback.show(title: "Command unavailable", detail: reason, tone: .warning)
            return
        }
        let arguments = PaletteCommand.commandArguments(query)
        let trimmed = arguments.trimmingCharacters(in: .whitespacesAndNewlines)
        if let command = row.prompt {
            close()
            state.runPalettePrompt(command, args: arguments)
            return
        }
        if let command = row.plugin {
            close()
            Task { await state.runPluginCommand(command, args: arguments) }
            return
        }
        let workspace = state.ui.workspace
        switch row.id {
        case "cd": screen = .directory(path: trimmed, submit: !trimmed.isEmpty)
        case "debug": screen = .details
        case "mcp": screen = .mcp
        case "sessions": screen = .sessions
        case "model": screen = .model
        case "thinking": screen = .thinking
        case "tabs": screen = .tabs
        case "fork": screen = .fork(message: trimmed, submit: !trimmed.isEmpty)
        case "name":
            screen = .rename(name: trimmed.isEmpty ? state.selected?.title ?? "" : trimmed, submit: !trimmed.isEmpty)
        case "subagents": state.ui.subagentsPresented = true
        case "reload": reload()
        case "dispose":
            guard var session = state.selected else { return }
            session.isTemporary = true
            disposalTarget = session
        default:
            close()
            switch row.id {
            case "compact":
                state.compactionOperation.beginNewIfResolved()
                Task { await state.compactSession() }
            case "refresh-models": Task { await state.refreshModelCatalog() }
            case "diff": workspace.open(.review)
            case "scratchpad": workspace.open(.scratchpad)
            case "files": state.showFiles(intent: "open")
            case "new": navigation.openLauncher()
            case "quit": NSApp.terminate(nil)
            case "theme": openSettings()
            case "appearance": appearance = theme.dark ? "light" : "dark"
            case "split-right": workspace.split(workspace.selected, .right)
            case "split-down": workspace.split(workspace.selected, .bottom)
            case "zoom": workspace.toggleZoom()
            case "equalize": workspace.equalize()
            case "join-tabs": workspace.joinTiles(selecting: workspace.selected)
            default: break
            }
        }
    }

    private func reload() {
        let session = state.selectedID
        let operation = state.reloadOperation
        let presentation = state.ui.paletteGeneration
        reloadSessionID = session
        screen = .reload(operation)
        Task {
            await state.reloadSession(for: session)
            guard state.ui.palette, state.ui.paletteGeneration == presentation,
                  state.selectedID == session, reloadSessionID == session,
                  case let .reload(current) = screen, current === operation else { return }
            close()
        }
    }
}

/// Lists the palette opens for commands that pick a value: the session's
/// model, its thinking level, or a workspace tab.
struct PaletteChoices: View {
    @State private var query = ""
    let placeholder: String
    let rows: [PaletteCommand]
    let empty: String
    var current: String? = nil
    let choose: (PaletteCommand) -> Void
    let close: () -> Void

    var body: some View {
        PaletteListScreen(query: $query, placeholder: placeholder, catalog: rows, wholeQuery: true,
                          emptyMessage: { $0.isEmpty ? empty : "Nothing matches “\($0)”" },
                          initialSelection: current, activate: { close(); choose($0) }, dismiss: close)
    }

    static func model(state: SessionStore, close: @escaping () -> Void) -> Self {
        let rows = state.configuration.models.map { model in
            PaletteCommand(id: model.id, name: model.name,
                           description: ComposerModelPicker.rowDescription(model),
                           meta: model.id == state.model ? "current" : model.provider,
                           searchTerms: [model.id, model.provider])
        }
        return .init(placeholder: "Search models…", rows: rows, empty: "No models available",
                     current: state.model, choose: { row in Task { await state.configure(model: row.id) } }, close: close)
    }

    static func thinking(state: SessionStore, close: @escaping () -> Void) -> Self {
        let rows = state.thinkingLevels.map {
            PaletteCommand(id: $0, name: $0, description: "", meta: $0 == state.thinking ? "current" : "")
        }
        return .init(placeholder: "Search thinking levels…", rows: rows, empty: "This model has no thinking levels",
                     current: state.thinking, choose: { row in Task { await state.configure(thinking: row.id) } }, close: close)
    }

    static func tabs(state: SessionStore, close: @escaping () -> Void) -> Self {
        let workspace = state.ui.workspace
        var panes = workspace.panes
        if state.catalogClient is any DiffClient, !panes.contains(.review) { panes.append(.review) }
        if state.canOpenScratchpad, !panes.contains(.scratchpad) { panes.append(.scratchpad) }
        let rows = panes.map { pane in
            PaletteCommand(id: pane.id, name: pane.title, description: description(pane),
                           meta: workspace.panes.contains(pane) ? "open" : "")
        }
        return .init(placeholder: "Search tabs…", rows: rows, empty: "No tabs", current: workspace.selected.id,
                     choose: { row in if let pane = WorkspacePane(id: row.id) { workspace.open(pane) } }, close: close)
    }

    private static func description(_ pane: WorkspacePane) -> String {
        switch pane {
        case .conversation: "Conversation with the agent"
        case .review: "Working-tree changes"
        case .scratchpad: "Shared working notes"
        case .agent: "Subagent conversation"
        case .file(let path): path
        }
    }
}
