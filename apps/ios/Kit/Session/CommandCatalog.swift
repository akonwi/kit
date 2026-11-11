import Foundation
import KitClient

/// One command the composer's command search can run: one of the session
/// menu's actions, a prompt command, or a plugin command.
struct SessionCommand: Identifiable, Equatable {
    enum Kind: Equatable {
        case builtIn(BuiltIn)
        case prompt(PromptCommand)
        case plugin(PluginCommand)
    }

    /// The session menu's actions, and reloading the session's context, in
    /// the order the menu lists them.
    enum BuiltIn: String, CaseIterable {
        case changes, openFile, scratchpad, model, thinking, compact, rename, fork, reload, details

        var title: String {
            switch self {
            case .changes: "Changes"
            case .openFile: "Open File…"
            case .scratchpad: "Scratchpad"
            case .model: "Model"
            case .thinking: "Thinking"
            case .compact: "Compact context"
            case .rename: "Rename…"
            case .fork: "Fork…"
            case .reload: "Reload context"
            case .details: "Details"
            }
        }

        /// The menu item's symbol.
        var symbol: String {
            switch self {
            case .changes: "plus.forwardslash.minus"
            case .openFile: "doc"
            case .scratchpad: "note.text"
            case .model: "sparkles"
            case .thinking: "brain"
            case .compact: "arrow.down.right.and.arrow.up.left"
            case .rename: "pencil"
            case .fork: "arrow.triangle.branch"
            case .reload: "arrow.clockwise"
            case .details: "info.circle"
            }
        }

        /// What the TUI calls it, and words people search for.
        var searchTerms: [String] {
            switch self {
            case .changes: ["diff", "working tree"]
            case .openFile: ["files", "browse", "path"]
            case .scratchpad: ["notes", "memory"]
            case .model: ["engine"]
            case .thinking: ["reasoning", "effort"]
            case .compact: ["summarize", "shrink"]
            case .rename: ["name", "title"]
            case .fork: ["branch"]
            case .reload: ["agents", "refresh", "instructions"]
            case .details: ["debug", "usage", "tokens", "cost"]
            }
        }
    }

    let kind: Kind
    /// The name shown, plain: `Model`, `review`.
    let title: String
    /// What sits at the row's end: a value such as the model, or what the
    /// command does.
    var detail = ""
    /// Why it can't run now. The row stays, dimmed, with the reason as its detail.
    var unavailableReason: String?

    var id: String {
        switch kind {
        case .builtIn(let command): "builtin:" + command.rawValue
        case .prompt(let command): "prompt:" + command.name
        // Recents outlive a plugin instance, so its ID leaves the instance out.
        case .plugin(let command): "plugin:" + command.id
        }
    }

    var searchTerms: [String] {
        switch kind {
        case .builtIn(let command): command.searchTerms
        case .prompt(let command): [command.sourceLabel]
        case .plugin(let command): [command.id, command.pluginID]
        }
    }

    /// The text matched besides the title and terms.
    var description: String {
        switch kind {
        case .builtIn: ""
        case .prompt(let command): command.description
        case .plugin(let command): command.description
        }
    }

    /// Whether picking it puts a chip in the composer for what to run it
    /// with. Prompt commands always take text; plugin commands when they name an argument.
    var takesArgument: Bool {
        switch kind {
        case .builtIn: false
        case .prompt: true
        case .plugin(let command): command.argName != nil
        }
    }
}

/// What the built-in commands show and whether they can run, read from the
/// session when the search opens.
struct CommandState: Equatable {
    var changes = ""
    var model = ""
    var modelLocked: String?
    var thinking = ""
    /// Whether the model offers thinking levels.
    var hasThinking = false
    var thinkingLocked: String?
    var contextPercent: Int?
    var compactUnavailable: String?
    var live = true
    var forkUnavailable: String?
    var reloadUnavailable: String?
    var pluginUnavailable: String?
    /// Temporary sessions have no scratchpad.
    var scratchpad = true
}

/// The commands a session offers, and how the command search lists them.
enum CommandCatalog {
    /// A titled group of rows. Search results have no title.
    struct Section: Equatable {
        let title: String?
        let rows: [SessionCommand]
    }

    /// How many recent commands lead the list.
    static let recentLimit = 3

    /// The built-ins in menu order, then prompt commands, then plugin commands.
    static func commands(_ state: CommandState, prompts: [PromptCommand], plugins: [PluginCommand]) -> [SessionCommand] {
        builtIns(state) + prompts.map {
            SessionCommand(kind: .prompt($0), title: $0.name, detail: $0.description)
        } + plugins.map {
            SessionCommand(kind: .plugin($0), title: $0.localID, detail: $0.description,
                           unavailableReason: state.pluginUnavailable)
        }
    }

    static func builtIns(_ state: CommandState) -> [SessionCommand] {
        SessionCommand.BuiltIn.allCases.compactMap { command in
            var row = SessionCommand(kind: .builtIn(command), title: command.title)
            switch command {
            case .changes: row.detail = state.changes
            case .openFile: break
            case .scratchpad: if !state.scratchpad { return nil }
            case .model: row.detail = state.model; row.unavailableReason = state.modelLocked
            case .thinking:
                guard state.hasThinking else { return nil }
                row.detail = state.thinking
                row.unavailableReason = state.thinkingLocked
            case .compact:
                row.detail = state.contextPercent.map { "\($0)%" } ?? ""
                row.unavailableReason = state.compactUnavailable
            case .rename: row.unavailableReason = state.live ? nil : "Reconnect to rename"
            case .fork: row.unavailableReason = state.forkUnavailable
            case .reload: row.unavailableReason = state.reloadUnavailable
            case .details: break
            }
            return row
        }
    }

    /// With nothing typed: the recent commands, then the rest grouped as the
    /// menu groups them. With a query: every match, best first.
    static func sections(_ commands: [SessionCommand], query: String, recents: [String]) -> [Section] {
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard query.isEmpty else {
            let rows = rank(commands, query: query, name: \.title, description: \.description, terms: \.searchTerms)
            return rows.isEmpty ? [] : [Section(title: nil, rows: rows)]
        }
        let recent = recents.compactMap { id in commands.first { $0.id == id } }.prefix(recentLimit)
        let shown = Set(recent.map(\.id))
        let rest = commands.filter { !shown.contains($0.id) }
        func group(_ title: String, _ include: (SessionCommand.Kind) -> Bool) -> Section? {
            let rows = rest.filter { include($0.kind) }
            return rows.isEmpty ? nil : Section(title: title, rows: rows)
        }
        return [
            recent.isEmpty ? nil : Section(title: "Recent", rows: Array(recent)),
            group("Session") { if case .builtIn = $0 { true } else { false } },
            group("Prompts") { if case .prompt = $0 { true } else { false } },
            group("Plugins") { if case .plugin = $0 { true } else { false } }
        ].compactMap { $0 }
    }

    /// Rows matching the query, best first, ties in list order. `query` is lowercased.
    static func rank<Row>(_ rows: [Row], query: String, name: (Row) -> String,
                          description: (Row) -> String = { _ in "" },
                          terms: (Row) -> [String] = { _ in [] }) -> [Row] {
        guard !query.isEmpty else { return rows }
        var ranked: [(row: Row, score: Int, position: Int)] = []
        for (position, row) in rows.enumerated() {
            let score = CommandSearch.score(name: name(row), description: description(row), terms: terms(row), query: query)
            if score > 0 { ranked.append((row, score, position)) }
        }
        ranked.sort { $0.score == $1.score ? $0.position < $1.position : $0.score > $1.score }
        return ranked.map(\.row)
    }
}

/// The commands run most recently on this phone, newest first, kept across
/// sessions and launches.
struct CommandRecents {
    static let key = "commandSearch.recents"
    /// More than the list shows, so commands missing from one session don't
    /// push out the ones it has.
    static let limit = 12
    let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) { self.defaults = defaults }

    var ids: [String] { defaults.stringArray(forKey: Self.key) ?? [] }

    func record(_ id: String) {
        let ids = [id] + ids.filter { $0 != id }
        defaults.set(Array(ids.prefix(Self.limit)), forKey: Self.key)
    }
}
