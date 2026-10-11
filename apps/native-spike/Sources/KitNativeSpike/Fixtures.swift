import AppKit

enum Fixtures {
    /// Terminals start in this checkout.
    static let workingDirectory: String = {
        let url = URL(fileURLWithPath: #filePath)
        return url.deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().path
    }()

    static func sessions(_ model: AppModel) -> [Session] {
        // Tiling the native shell: agent | (diff / reviewer), as on the board,
        // plus a terminal and a browser to exercise native views.
        let agent = model.makePane(.agent, title: "Agent")
        let scratch = model.makePane(.scratchpad, title: "Scratchpad")
        let diff = model.makePane(.diff, title: "Diff")
        let file = model.makePane(.file("Models/WorkspaceSplitTree.swift"), title: "WorkspaceSplitTree.swift")
        let browser = model.makePane(.browser(URL(string: "https://developer.apple.com/documentation/swiftui")!),
                                     title: "SwiftUI docs")
        let reviewer = model.makePane(.reviewer, title: "reviewer")
        let terminal = model.makePane(.terminal, title: "Terminal")
        let left = Tile(id: UUID(), panes: [agent, scratch], selected: agent)
        let top = Tile(id: UUID(), panes: [diff, file, browser], selected: diff)
        let bottom = Tile(id: UUID(), panes: [reviewer, terminal], selected: reviewer)
        let tiling = Session(
            title: "Tiling the native shell", project: "KIT", directory: "~/Developer/agent/kit",
            branch: "spike/macos-terminal-shell*", pr: 74, status: .running,
            root: .split(.init(id: UUID(), axis: .horizontal, ratio: 0.53,
                               first: .leaf(left.id),
                               second: .split(.init(id: UUID(), axis: .vertical, ratio: 0.64,
                                                    first: .leaf(top.id), second: .leaf(bottom.id))))),
            tiles: [left, top, bottom], focused: left.id)
        tiling.activity = "Checking whether `equalized()` should walk nested splits or only reset the root ratio before the zoomed node is restored"
        tiling.commentChips = ["WorkspaceSplitTree.swift:38"]

        func single(_ title: String, _ project: String, _ dir: String, _ branch: String, _ status: SessionStatus) -> Session {
            let agent = model.makePane(.agent, title: "Agent")
            let tile = Tile(id: UUID(), panes: [agent], selected: agent)
            return Session(title: title, project: project, directory: dir, branch: branch, pr: nil, status: status,
                           root: .leaf(tile.id), tiles: [tile], focused: tile.id)
        }
        let release = single("Release 0.40 notes", "KIT", "~/Developer/agent/kit", "main", .waiting)
        release.question = Question(
            title: "Set up the 0.40 release", prompt: "Which channel?",
            options: [
                .init(title: "Stable", detail: "Tag, publish the binary and update Homebrew"),
                .init(title: "Beta", detail: "Tag and publish; Homebrew stays on 0.39"),
                .init(title: "Draft", detail: "Only create the release notes"),
            ], step: 0, steps: 4)
        let paper = single("Paper MCP setup", "KIT", "~/Developer/agent/kit", "main", .idle)
        let parser = single("Parser error recovery", "ARD", "~/Developer/ard", "parser-recovery", .running)
        parser.activity = "Working…"
        let codegen = single("Go target codegen", "ARD", "~/Developer/ard", "go-target", .finished)
        return [tiling, release, paper, parser, codegen]
    }

    struct ToolCall: Identifiable {
        let id = UUID()
        let icon: Ph
        let verb: String
        let chip: String
        var added: Int? = nil
        var removed: Int? = nil
        var meta: String? = nil
    }

    static let agentTools: [ToolCall] = [
        .init(icon: .file, verb: "Read file", chip: "Models/WorkspaceSplitTree.swift:30–60"),
        .init(icon: .pencilSimple, verb: "Edit 2 sections", chip: "Models/WorkspaceSplitTree.swift", added: 22, removed: 4),
        .init(icon: .pencilSimple, verb: "Edit 3 sections", chip: "Views/WorkspaceLayout.swift", added: 38, removed: 12),
        .init(icon: .pencilSimple, verb: "Edit 2 sections", chip: "Views/TerminalSurfaceView.swift", added: 26, removed: 15),
    ]

    static let reviewerTools: [ToolCall] = [
        .init(icon: .file, verb: "Read file", chip: "Models/WorkspaceSplitTree.swift"),
        .init(icon: .magnifyingGlass, verb: "Search", chip: "withRatio", meta: "4 matches"),
    ]

    enum LineKind { case context, added, removed }
    struct DiffLine: Identifiable {
        let id = UUID()
        let kind: LineKind
        let old: Int?
        let new: Int?
        let text: String
    }
    struct DiffFile: Identifiable {
        let id = UUID()
        let path: String
        let added: Int
        let removed: Int
        let lines: [DiffLine]
    }

    static let diff: [DiffFile] = [
        DiffFile(path: "Models/WorkspaceSplitTree.swift", added: 22, removed: 4, lines: [
            .init(kind: .context, old: 36, new: 36, text: "    func resizing(_ node: Node, to ratio: Double) -> Self {"),
            .init(kind: .removed, old: 37, new: nil, text: "        let ratio = ratio"),
            .init(kind: .added, old: nil, new: 37, text: "        // Keep both tiles usable while dragging the divider."),
            .init(kind: .added, old: nil, new: 38, text: "        let ratio = min(max(ratio, 0.15), 0.85)"),
            .init(kind: .context, old: 38, new: 39, text: "        return replacing(node, with: node.withRatio(ratio))"),
            .init(kind: .context, old: 39, new: 40, text: "    }"),
            .init(kind: .context, old: 40, new: 41, text: ""),
            .init(kind: .context, old: 41, new: 42, text: "    func equalized() -> Self {"),
            .init(kind: .removed, old: 42, new: nil, text: "        return self"),
            .init(kind: .added, old: nil, new: 43, text: "        guard case .split(let split) = root else { return self }"),
            .init(kind: .added, old: nil, new: 44, text: "        return Self(root: .split(split.withRatio(0.5)), zoomed: zoomed)"),
            .init(kind: .context, old: 43, new: 45, text: "    }"),
        ]),
        DiffFile(path: "Views/WorkspaceLayout.swift", added: 38, removed: 12, lines: [
            .init(kind: .context, old: 12, new: 12, text: "struct WorkspaceLayout: View {"),
            .init(kind: .removed, old: 13, new: nil, text: "    let groups: [PaneGroup]"),
            .init(kind: .added, old: nil, new: 13, text: "    let tree: WorkspaceSplitTree"),
            .init(kind: .context, old: 14, new: 14, text: "    var body: some View {"),
        ]),
        DiffFile(path: "Views/TerminalSurfaceView.swift", added: 26, removed: 15, lines: [
            .init(kind: .context, old: 88, new: 88, text: "    override func becomeFirstResponder() -> Bool {"),
            .init(kind: .added, old: nil, new: 89, text: "        onFocusChange?(true)"),
        ]),
    ]

    struct Command: Identifiable {
        var id: String { name }
        let name: String
        var args: String? = nil
        let detail: String
        var idleOnly = false
        var run: ((AppModel) -> Void)? = nil
    }

    static let commands: [Command] = baseCommands + themeCommands

    static let themeCommands: [Command] = Theme.all.map { (theme: Theme) -> Command in
        Command(name: "theme \(theme.id)", detail: "Switch to \(theme.name)", run: { (model: AppModel) in model.theme = theme })
    }

    static let baseCommands: [Command] = [
        .init(name: "browser", detail: "Open a browser tab in the focused tile",
              run: { $0.open(.browser(URL(string: "https://developer.apple.com/documentation/swiftui")!), title: "Browser") }),
        .init(name: "cd", args: "[path]", detail: "Change working directory", idleOnly: true),
        .init(name: "compact", detail: "Compact session context", idleOnly: true),
        .init(name: "debug", detail: "Show session diagnostics"),
        .init(name: "diff", detail: "Review working-tree changes", run: { $0.open(.diff, title: "Diff") }),
        .init(name: "equalize", detail: "Even out every split", run: { $0.equalize() }),
        .init(name: "files", detail: "Open a workspace file"),
        .init(name: "fork", args: "[message]", detail: "Fork the current session into a linked child session"),
        .init(name: "login", detail: "Connect another provider"),
        .init(name: "mcp", detail: "Show configured MCP servers"),
        .init(name: "model", detail: "Change session model"),
        .init(name: "name", args: "[name]", detail: "Rename session"),
        .init(name: "new", detail: "Start a new session", run: { $0.newSession() }),
        .init(name: "question", detail: "Ask a guided question (interaction dock demo)", run: { $0.askDemo() }),
        .init(name: "quit", detail: "Exit Kit", run: { _ in NSApp.terminate(nil) }),
        .init(name: "refresh-models", detail: "Update the provider catalog of models", idleOnly: true),
        .init(name: "reload", detail: "Reload session context"),
        .init(name: "split-down", detail: "Split the focused tile with a terminal below", run: { $0.split(.vertical) }),
        .init(name: "split-right", detail: "Split the focused tile with a terminal beside it", run: { $0.split(.horizontal) }),
        .init(name: "tabs", detail: "Toggle side and top session tabs", run: { $0.sideTabs.toggle() }),
        .init(name: "terminal", detail: "Open a terminal tab in the focused tile", run: { $0.open(.terminal, title: "Terminal") }),
        .init(name: "toast", detail: "Raise a sample toast", run: { $0.demoToasts() }),
        .init(name: "zoom", detail: "Zoom the focused tile", run: { $0.toggleZoom() }),
    ]
}
