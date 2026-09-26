import SwiftUI

struct CommandPalette: View {
    @Environment(\.mica) private var theme
    @AppStorage("appearance") private var appearance = "system"
    @Bindable var state: SessionStore
    let openFork: (String) -> Void
    @State private var disposalTarget: SessionExcerpt?
    @State private var deleting = true
    @State private var showingDetails = false
    @State private var showingMCP = false
    @State private var changingDirectory = false
    @State private var browsing = false
    @State private var forking = false
    @State private var renaming = false
    @State private var reloadSessionID: String?
    @State private var reloadProgressOperation: SessionReloadOperation?
    @State private var query = ""
    @State private var selection = 0
    @State private var selectedIdentity: String?
    @State private var pluginSelection: PluginCommand?
    @FocusState private var focused: Bool

    private var actions: [PaletteCommand] {
        PaletteCommand.sortedByName(PaletteCommand.catalog(dark: theme.dark)
                                    + PaletteCommand.promptCatalog(state.selected?.promptCommands ?? [])
                                    + PaletteCommand.pluginCatalog(state.pluginCommands))
            .filter { state.isDemo || !$0.demoOnly }
            .filter { $0.id != "Open Scratchpad" || state.canOpenScratchpad }
            .filter { $0.id != "Dispose temporary session" || state.isTemporary }
            .filter { $0.matches($0.prompt == nil ? query : promptQuery) }
    }

    private var promptQuery: String { PaletteCommand.splitQuery(query).command }
    private var promptArguments: String { PaletteCommand.splitQuery(query).args }

    var body: some View {
        Group {
            if let reloadProgressOperation {
                reloadProgress(operation: reloadProgressOperation)
            } else if let command = pluginSelection {
                PluginCommandArguments(state: state, command: command) { pluginSelection = nil }
            } else if showingDetails {
                SessionDetailsView(state: state) { showingDetails = false }
            } else if showingMCP {
                MCPStatusView(state: state) { showingMCP = false }
            } else if changingDirectory {
                ChangeDirectoryView(state: state) { changingDirectory = false }
            } else if browsing {
                SessionPickerView(client: state.catalogClient, currentID: state.selectedID, close: { state.ui.palette = false }) { session in
                    state.ui.palette = false
                    openFork(session.id)
                }
            } else if forking {
                ForkSessionView(state: state, back: { forking = false }, open: openFork)
            } else if renaming {
                RenameSessionView(state: state) { renaming = false }
            } else {
                commandList
            }
        }
        .modifier(SessionCatalogActions(client: state.catalogClient, refresh: {
            await state.refreshSessions()
            if state.unavailable { state.ui.palette = false }
        }, target: $disposalTarget, deleting: $deleting))
        .onChange(of: state.ui.palette) { _, presented in
            if !presented {
                reloadSessionID = nil
                reloadProgressOperation = nil
            }
        }
    }

    private func reloadProgress(operation: SessionReloadOperation) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                KitSpinner()
                Text(operation.progressLabel).font(.kit(size: 16, weight: .medium))
            }.padding(20)
            Rule()
            Text("You can hide this panel; the reload will continue.")
                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                .padding(20)
            Spacer(minLength: 16)
            Rule()
            Button("Esc hide") { state.ui.palette = false }
                .keyboardShortcut(.cancelAction).buttonStyle(.plain)
                .font(.kit(size: 11)).foregroundStyle(theme.muted).padding(14)
        }
        .foregroundStyle(theme.text).background(theme.surface)
        .frame(width: 520, height: 180)
        .onExitCommand { state.ui.palette = false }
    }

    private var commandList: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "magnifyingglass").foregroundStyle(theme.muted)
                TextField("Search commands…", text: $query).textFieldStyle(.plain)
                    .font(.kit(size: 16)).focused($focused)
                    .onSubmit { performSelected() }
                    .onExitCommand { state.ui.palette = false }
                Button("Esc") { state.ui.palette = false }.keyboardShortcut(.cancelAction).buttonStyle(.plain).font(.kit(size: 11, design: .monospaced))
            }.padding(20)
            Rule()
            MetaLabel(text: "COMMANDS").padding(.horizontal, 20).padding(.top, 18).padding(.bottom, 10)
            if actions.isEmpty {
                Text("No matching commands").foregroundStyle(theme.muted).padding(20)
            }
            ScrollViewReader { proxy in
            ScrollView { VStack(spacing: 0) { ForEach(Array(actions.enumerated()), id: \.element.id) { index, action in
                Button { selection = index; selectedIdentity = action.id; performSelected() } label: {
                    HStack(spacing: 12) {
                        if action.id == "Reload session context" && state.reloadOperation.pending {
                            KitSpinner().frame(width: 18)
                        } else {
                            Image(systemName: action.icon).frame(width: 18)
                        }
                        VStack(alignment: .leading, spacing: 3) {
                            HStack {
                                Text(action.name).font(.kit(size: 13, weight: .medium))
                                if let hint = action.plugin?.argName { Text("<" + hint + ">").font(.kit(size: 12)).foregroundStyle(theme.muted) }
                                if let hint = action.prompt?.argumentHint, !hint.isEmpty {
                                    Text(hint).font(.kit(size: 12)).foregroundStyle(theme.muted)
                                }
                            }
                            Text(action.description).font(.kit(size: 12)).foregroundStyle(theme.muted)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        Spacer()
                        if let reason = unavailableReason(action.id) {
                            Text(reason).font(.kit(size: 11)).foregroundStyle(theme.muted)
                        } else if selection == index { Text("↵").foregroundStyle(theme.muted) }
                    }.padding(.horizontal, 12).padding(.vertical, 9).frame(minHeight: 52)
                        .background(selection == index ? theme.hover : Color.clear)
                        .contentShape(Rectangle())
                }.buttonStyle(.plain).disabled(unavailableReason(action.id) != nil).padding(.horizontal, 8).id(action.id)
                    .help(action.prompt.map { $0.source + " · " + $0.location } ?? action.description)
            }
            } }
            .onChange(of: selection) {
                if actions.indices.contains(selection) { proxy.scrollTo(actions[selection].id) }
            }
            .onChange(of: query) {
                if let first = actions.first { proxy.scrollTo(first.id, anchor: .top) }
            }
            }
            Spacer(minLength: 16)
            Rule()
            HStack {
                Text("↑ ↓ navigate"); Text("↵ select")
                Spacer()
            }.font(.kit(size: 10)).foregroundStyle(theme.muted).padding(14)
        }
        .foregroundStyle(theme.text).background(theme.surface)
        .frame(width: 520, height: 460)
        .task {
                do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
                focused = true
            }
        .onChange(of: query) { selection = 0; selectedIdentity = actions.first?.id }
        .onAppear {
            if let selectedIdentity { selection = actions.firstIndex { $0.id == selectedIdentity } ?? -1 }
            else if actions.indices.contains(selection) { selectedIdentity = actions[selection].id }
        }
        .onChange(of: actions.map(\.id)) {
            // Keep the selected generation, not its previous numeric row. A
            // removed generation requires a fresh pointer or arrow selection.
            if let selectedIdentity, let index = actions.firstIndex(where: { $0.id == selectedIdentity }) { selection = index }
            else { selection = -1; selectedIdentity = nil }
        }
        .onKeyPress(.downArrow) { selection = min(selection + 1, max(0, actions.count - 1)); selectedIdentity = actions.indices.contains(selection) ? actions[selection].id : nil; return .handled }
        .onKeyPress(.upArrow) { selection = max(0, selection - 1); selectedIdentity = actions.indices.contains(selection) ? actions[selection].id : nil; return .handled }
        .onExitCommand { state.ui.palette = false }
    }

    private func unavailableReason(_ name: String) -> String? {
        if name.hasPrefix("prompt:") { return state.palettePromptUnavailableReason }
        if name.hasPrefix("plugin:") { return state.pluginCommandUnavailableReason }
        return switch name {
        case "compact": state.compactionUnavailableReason
        case "Refresh model catalog": modelRefreshUnavailableReason
        case "Reload session context": state.reloadOperation.pending ? state.reloadOperation.progressLabel : state.reloadUnavailableReason
        case "Change working directory": state.directoryUnavailableReason
        case "Rename session": state.renameUnavailableReason
        case "Fork session": state.forkUnavailableReason
        default: nil
        }
    }

    private var modelRefreshUnavailableReason: String? {
        if !(state.catalogClient is any ModelCatalogRefreshClient) { return "Unavailable for this connection" }
        if state.unavailable { return "Session unavailable" }
        if state.connectionState != .connected { return "Connect to refresh" }
        if state.modelCatalogRefreshPending { return "Refreshing model catalog" }
        return nil
    }

    private func performSelected() {
        guard actions.indices.contains(selection) else { return }
        let action = actions[selection]
        guard selectedIdentity == action.id else { return }
        if let command = action.prompt {
            guard state.palettePromptUnavailableReason == nil else { return }
            focused = false
            state.runPalettePrompt(command, args: promptArguments)
            state.ui.palette = false
            return
        }
        if let command = action.plugin {
            guard state.pluginCommandUnavailableReason == nil else { return }
            focused = false
            pluginSelection = command
            return
        }
        let name = action.id
        if name == "compact" {
            guard state.compactionUnavailableReason == nil else { return }
            state.compactionOperation.beginNewIfResolved()
            focused = false
            state.ui.palette = false
            Task { await state.compactSession() }
            return
        }
        if name == "Refresh model catalog" {
            guard modelRefreshUnavailableReason == nil else { return }
            focused = false
            state.ui.palette = false
            Task { await state.refreshModelCatalog() }
            return
        }
        if name == "Reload session context" {
            guard state.reloadUnavailableReason == nil else { return }
            focused = false
            let session = state.selectedID
            let operation = state.reloadOperation
            let presentation = state.ui.paletteGeneration
            reloadSessionID = session
            reloadProgressOperation = operation
            Task {
                await state.reloadSession(for: session)
                guard state.ui.palette, state.ui.paletteGeneration == presentation,
                      state.selectedID == session, reloadSessionID == session,
                      reloadProgressOperation === operation else { return }
                state.ui.palette = false
            }
            return
        }
        if name == "Session details" {
            focused = false
            showingDetails = true
            return
        }
        if name == "MCP servers" {
            focused = false
            showingMCP = true
            return
        }
        if name == "Change working directory" {
            guard state.directoryUnavailableReason == nil else { return }
            focused = false
            changingDirectory = true
            return
        }
        if name == "Dispose temporary session" {
            guard var session = state.selected else { return }
            session.isTemporary = true
            disposalTarget = session
            return
        }
        if name == "Switch session" {
            focused = false
            browsing = true
            return
        }
        if name == "Fork session" {
            guard state.forkUnavailableReason == nil else { return }
            focused = false
            forking = true
            return
        }
        if name == "Rename session" {
            guard state.renameUnavailableReason == nil else { return }
            focused = false
            renaming = true
            return
        }
        state.ui.palette = false
        if name == "Open Code Review" { state.ui.workspace.open(.review) }
        else if name == "Open Scratchpad" { state.ui.workspace.open(.scratchpad) }
        else if name == "Find workspace file" { state.showFiles(intent: "open") }
        else if name.hasPrefix("Switch") { appearance = theme.dark ? "light" : "dark" }
        else { state.select(state.selectedID) }
    }
}
