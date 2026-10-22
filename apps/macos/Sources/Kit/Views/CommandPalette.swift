import AppKit
import SwiftUI

/// The command palette: the TUI's catalog plus tile commands, run from one
/// search field where arguments are typed after the command name.
struct CommandPalette: View {
    @Environment(\.mica) private var theme
    @Environment(\.sessionNavigation) private var navigation
    @Environment(\.openSettings) private var openSettings
    @Environment(\.installedThemes) private var installedThemes
    @Environment(\.reloadThemes) private var reloadThemes
    @Environment(\.serverDirectory) private var servers
    @Bindable var state: SessionStore
    let openFork: (String) -> Void
    @State private var screen = Screen.commands
    @State private var disposalTarget: SessionExcerpt?
    @State private var deleting = true
    @State private var reloadSessionID: String?
    @State private var query = ""
    @State private var refusal = PaletteRefusal.none
    @State private var argumentProblem: String?

    /// A nested screen the palette shows in place of the command list.
    enum Screen {
        case commands, details, mcp, sessions, model, thinking, tabs, theme
        case directory(path: String, submit: Bool)
        case fork(message: String, submit: Bool)
        case rename(name: String, submit: Bool)
        case reload(SessionReloadOperation)
    }

    private var catalog: [PaletteCommand] {
        PaletteCommand.catalog(
            builtIns: PaletteCommand.builtIns(scratchpad: state.canOpenScratchpad,
                                              temporary: state.isTemporary,
                                              terminal: state.offersTerminal),
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
                    SessionPickerView(servers: servers?.connections ?? [],
                                      current: SessionIdentity(server: state.serverID, session: state.selectedID),
                                      close: close) { identity in
                        close()
                        navigation.openSession(identity)
                    }
                case .model: PaletteChoices.model(state: state, close: close)
                case .thinking: PaletteChoices.thinking(state: state, close: close)
                case .tabs: PaletteChoices.tabs(state: state, close: close)
                case .theme:
                    PaletteChoices.theme(dark: theme.dark, installed: installedThemes, close: close)
                        .onAppear(perform: reloadThemes)
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
            problem: argumentProblem, refusal: refusal,
            activate: run, dismiss: close)
            .onChange(of: query) { argumentProblem = nil }
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
        case "new": state.newSessionUnavailableReason
        case "name": state.renameUnavailableReason
        case "refresh-models": modelRefreshUnavailableReason
        case "reload": state.reloadOperation.pending ? state.reloadOperation.progressLabel : state.reloadUnavailableReason
        case "model", "thinking": configurationUnavailableReason
        case "diff": state.catalogClient is any DiffClient ? nil : "Unavailable for this connection"
        case "files": state.unavailable ? "Session unavailable" : nil
        case "terminal": state.terminalUnavailableReason
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
        // The row already shows why; flash it rather than raise a toast.
        if unavailableReason(row) != nil {
            let next = refusal.next(row.id)
            refusal = next
            // Settle the refusal so a row scrolled back into view doesn't flash again.
            Task { @MainActor in
                try? await Task.sleep(for: PaletteRefusal.duration + .milliseconds(300))
                if refusal == next { refusal = PaletteRefusal(rowID: "", generation: next.generation) }
            }
            return
        }
        let arguments = PaletteCommand.commandArguments(query)
        if let problem = row.argumentProblem(arguments) {
            argumentProblem = problem
            return
        }
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
        case "theme": screen = .theme
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
            case "terminal":
                if let directory = state.terminalDirectory { workspace.openTerminal(directory: directory) }
            case "browser":
                if let url = BrowserAddress.url(from: trimmed) {
                    workspace.openBrowser(url)
                } else {
                    // Text that isn't an address shows why in the new tab.
                    workspace.openBrowser().navigate(trimmed)
                }
            case "files": state.showFiles(intent: "open")
            case "new": newSession()
            case "quit": NSApp.terminate(nil)
            case "login": close(); SettingsSection.providers.open(with: openSettings)
            case "split-right": workspace.split(workspace.selected, .right)
            case "split-down": workspace.split(workspace.selected, .bottom)
            case "zoom": workspace.toggleZoom()
            case "equalize": workspace.equalize()
            case "join-tabs": workspace.joinTiles(selecting: workspace.selected)
            default: break
            }
        }
    }

    private func newSession() {
        guard state.canCreateSibling else { navigation.openLauncher(); return }
        let open = navigation.openSession
        let server = state.serverID
        Task {
            _ = await state.createSibling { open(SessionIdentity(server: server, session: $0.id)) }
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
    var preview: ((PaletteCommand?) -> Void)? = nil
    let choose: (PaletteCommand) -> Void
    let close: () -> Void

    var body: some View {
        PaletteListScreen(query: $query, placeholder: placeholder, catalog: rows, wholeQuery: true,
                          emptyMessage: { $0.isEmpty ? empty : "Nothing matches “\($0)”" },
                          initialSelection: current, selectionChanged: preview,
                          activate: { close(); choose($0) }, dismiss: close)
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

    /// The theme for the current mode, from every theme. Moving through the list previews
    /// each theme; choosing one saves it for this mode, and closing the list
    /// any other way restores the saved theme.
    static func theme(dark: Bool, installed: [InstalledTheme], close: @escaping () -> Void) -> some View {
        let configuration = ThemeConfiguration.decode(UserDefaults.standard.string(forKey: "themeConfiguration") ?? "")
        let current = configuration.choice(dark: dark)
        let rows = ThemeConfiguration.choices(installed: installed).map { choice in
            PaletteCommand(id: choice.id, name: choice.name, description: "", meta: choice.id == current ? "current" : "")
        }
        let preview = ThemePreview.shared
        return Self(placeholder: "Search themes…", rows: rows,
                    empty: "No themes", current: current,
                    preview: { row in preview.choice = row.map { .init(id: $0.id, dark: dark) } },
                    choose: { row in
                        var updated = ThemeConfiguration.decode(UserDefaults.standard.string(forKey: "themeConfiguration") ?? "")
                        if dark { updated.dark = row.id } else { updated.light = row.id }
                        UserDefaults.standard.set(updated.json, forKey: "themeConfiguration")
                        preview.choice = nil
                    }, close: close)
            .onDisappear { preview.choice = nil }
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
            PaletteCommand(id: pane.id, name: workspace.title(of: pane), description: description(pane, in: workspace),
                           meta: workspace.panes.contains(pane) ? "open" : "")
        }
        return .init(placeholder: "Search tabs…", rows: rows, empty: "No tabs", current: workspace.selected.id,
                     choose: { row in if let pane = WorkspacePane(id: row.id) { workspace.open(pane) } }, close: close)
    }

    private static func description(_ pane: WorkspacePane, in workspace: WorkspaceState) -> String {
        switch pane {
        case .conversation: "Conversation with the agent"
        case .review: "Working-tree changes"
        case .scratchpad: "Shared working notes"
        case .agent: "Subagent conversation"
        case .file(let path): path
        case .image: "Transcript image"
        case .browser: workspace.browser(pane)?.url.map { BrowserAddress.withoutCredentials($0).absoluteString } ?? "New browser tab"
        case .terminal: workspace.terminal(pane).map { ($0.directory as NSString).abbreviatingWithTildeInPath } ?? "Terminal"
        }
    }
}
