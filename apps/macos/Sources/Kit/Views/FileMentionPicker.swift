import SwiftUI

struct FileMentionPicker: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    @State private var query = ""
    @State private var selection = 0
    @FocusState private var focused: Bool
    @State private var remoteFiles: [String] = []
    @State private var indexNotice: String?
    @State private var loading = false
    @State private var revision = 0
    @State private var loadID = UUID()
    @State private var error: String?
    private var files: [String] {
        ComposerMentionState.filtered(query: query,
            paths: (state.isDemo ? state.ui.workspace.fixture.files.map(\.path) : remoteFiles)
                .filter { !$0.hasSuffix("/") })
    }

    private func load() async {
        guard let client = state.composerClient else { return }
        let token = UUID(); loadID = token
        loading = true; error = nil; remoteFiles = []; indexNotice = nil
        defer { if loadID == token { loading = false } }
        let id = state.selectedID
        let cwd = state.selected?.cwd
        do {
            let key = FileIndexCache.Key(server: state.serverID, session: id, cwd: cwd ?? "")
            let index = try await state.ui.workspace.fileIndex.load(key: key, refresh: revision > 0) {
                try await client.fileIndex(id, refresh: $0)
            }
            try Task.checkCancellation()
            guard state.selectedID == id, state.selected?.cwd == cwd else { return }
            indexNotice = index.notice
            remoteFiles = index.paths.filter { !$0.hasSuffix("/") }
            selection = 0
        }
        catch is CancellationError {} catch { if !Task.isCancelled { self.error = error.localizedDescription } }
    }

    var body: some View {
        let matches = files
        return VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "doc.text.magnifyingglass").foregroundStyle(theme.muted)
                TextField(state.ui.filePickerIntent == "open" ? "Find a workspace file…" : "Mention a file…", text: $query)
                    .textFieldStyle(.plain).font(.kit(size: 15)).focused($focused).onSubmit { choose() }
                Button { dismiss() } label: { Image(systemName: "xmark") }.buttonStyle(.plain).accessibilityLabel("Close file picker")
            }.padding(20)
            Rule()
            HStack {
                Text("Workspace files").foregroundStyle(theme.muted)
                Spacer()
                Button { revision += 1 } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(.plain).help("Refresh files (⌘R)").accessibilityLabel("Refresh files")
                    .keyboardShortcut("r", modifiers: .command).disabled(loading)
            }.padding(16)
            if let indexNotice { Text(indexNotice).font(.kit(size: 11)).foregroundStyle(theme.muted).padding(.horizontal, 16).padding(.bottom, 8) }
            ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(Array(matches.enumerated()), id: \.element) { index, file in
                        Button { selection = index; choose() } label: {
                            HStack(spacing: 12) {
                                Image(systemName: "doc")
                                Text(file).font(.kit(size: 12, design: .monospaced)).lineLimit(2).multilineTextAlignment(.leading)
                                Spacer()
                                if selection == index { Image(systemName: "return") }
                            }.padding(12).background(selection == index ? theme.hover : Color.clear).contentShape(Rectangle())
                        }.buttonStyle(.plain).padding(.horizontal, 8).id(file)
                    }
                    if loading { KitSpinner().padding(20) }
                    if let error { Text(error).foregroundStyle(theme.muted); Button("Retry") { revision += 1 } }
                    if matches.isEmpty && !loading && error == nil { Text("No matching files").foregroundStyle(theme.muted).padding(20) }
                }
            }
            .onChange(of: selection) {
                if matches.indices.contains(selection) { proxy.scrollTo(matches[selection]) }
            }
            .onChange(of: query) {
                if let first = matches.first { proxy.scrollTo(first, anchor: .top) }
            }
            }
            Rule()
            HStack { Text("↑ ↓ navigate   ↵ \(state.ui.filePickerIntent == "open" ? "open" : "insert mention")"); Spacer(); Text("Esc cancel") }
                .font(.kit(size: 10)).foregroundStyle(theme.muted).padding(14)
        }.foregroundStyle(theme.text).background(theme.surface).frame(width: 600, height: 360)
            .task(id: state.serverID + "|" + state.selectedID + "|" + (state.selected?.cwd ?? "") + "|" + String(revision)) {
                try? await Task.sleep(for: .milliseconds(100))
                guard !Task.isCancelled else { return }
                focused = true
                await load()
            }
            .onChange(of: query) { selection = 0 }
            .onKeyPress(.downArrow) { selection = min(selection + 1, max(0, files.count - 1)); return .handled }
            .onKeyPress(.upArrow) { selection = max(0, selection - 1); return .handled }
            .onExitCommand { dismiss() }
    }

    private func dismiss() { state.ui.filePicker = false; state.ui.composerFocus += 1 }
    private func choose() {
        guard files.indices.contains(selection) else { return }
        state.chooseFile(files[selection])
    }
}
