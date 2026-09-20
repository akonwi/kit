import SwiftUI

struct CommandPalette: View {
    @Environment(\.mica) private var theme
    @AppStorage("appearance") private var appearance = "system"
    @Bindable var state: SessionStore
    let openFork: (String) -> Void
    @State private var disposalTarget: SessionExcerpt?
    @State private var deleting = true
    @State private var reloading = false
    @State private var showingDetails = false
    @State private var changingDirectory = false
    @State private var browsing = false
    @State private var forking = false
    @State private var renaming = false
    @State private var query = ""
    @State private var selection = 0
    @FocusState private var focused: Bool

    private var actions: [PaletteCommand] {
        PaletteCommand.catalog(dark: theme.dark)
            .filter { state.isDemo || !$0.demoOnly }
            .filter { $0.id != "Open Scratchpad" || state.canOpenScratchpad }
            .filter { $0.id != "Dispose temporary session" || state.isTemporary }
            .filter { $0.matches(query) }
    }

    var body: some View {
        Group {
            if reloading {
                SessionReloadView(state: state) { reloading = false }
            } else if showingDetails {
                SessionDetailsView(state: state) { showingDetails = false }
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
                Button { selection = index; performSelected() } label: {
                    HStack(spacing: 12) {
                        Image(systemName: action.icon).frame(width: 18)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(action.name).font(.kit(size: 13, weight: .medium))
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
        .onChange(of: query) { selection = 0 }
        .onKeyPress(.downArrow) { selection = min(selection + 1, max(0, actions.count - 1)); return .handled }
        .onKeyPress(.upArrow) { selection = max(0, selection - 1); return .handled }
        .onExitCommand { state.ui.palette = false }
    }

    private func unavailableReason(_ name: String) -> String? {
        switch name {
        case "compact": state.compactionUnavailableReason
        case "Reload session context": state.reloadUnavailableReason
        case "Change working directory": state.directoryUnavailableReason
        case "Rename session": state.renameUnavailableReason
        case "Fork session": state.forkUnavailableReason
        default: nil
        }
    }

    private func performSelected() {
        guard actions.indices.contains(selection) else { return }
        let name = actions[selection].id
        if name == "compact" {
            guard state.compactionUnavailableReason == nil else { return }
            state.compactionOperation.beginNewIfResolved()
            focused = false
            state.ui.palette = false
            Task { await state.compactSession() }
            return
        }
        if name == "Reload session context" {
            guard state.reloadUnavailableReason == nil else { return }
            focused = false
            reloading = true
            return
        }
        if name == "Session details" {
            focused = false
            showingDetails = true
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
