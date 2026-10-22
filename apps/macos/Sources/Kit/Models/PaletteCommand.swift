import Foundation

/// One palette row. Built-ins use their name as identity; prompt and plugin
/// rows carry their definitions. Rows in nested choice screens (models,
/// thinking levels, tabs) use the same shape so every list reads alike.
struct PaletteCommand: Identifiable {
    let id: String
    let name: String
    var hint = ""
    let description: String
    /// Right-aligned source or state, such as "project" or "current".
    var meta = ""
    /// Aliases that match without being shown.
    var searchTerms: [String] = []
    /// Why the row can't run now. The row stays listed, dimmed.
    var unavailableReason: String? = nil
    var plugin: PluginCommand? = nil
    var prompt: PromptCommand? = nil

    // MARK: Invocation

    /// The command token. The rest of the query is the command's input, not
    /// filter text, so typing arguments keeps the selected command. Like the
    /// TUI, it ends at the first space; a leading slash is ignored.
    static func commandWord(_ query: String) -> String {
        var trimmed = Substring(query.trimmingCharacters(in: .whitespacesAndNewlines))
        if trimmed.hasPrefix("/") { trimmed = trimmed.dropFirst() }
        guard let separator = trimmed.firstIndex(of: " ") else { return String(trimmed) }
        return String(trimmed[..<separator])
    }

    /// The text after the command token and one separator, kept literal for
    /// prompt and plugin commands.
    static func commandArguments(_ query: String) -> String {
        let invocation = query.drop(while: { $0 == " " || $0 == "\t" })
        guard let separator = invocation.firstIndex(of: " ") else { return "" }
        return String(invocation[invocation.index(after: separator)...])
    }

    /// Why `arguments` can't be sent to this prompt or plugin command, or
    /// nil when they can. The palette stays open to show it.
    func argumentProblem(_ arguments: String) -> String? {
        let limit = prompt != nil ? Self.promptArgumentLimit : plugin != nil ? Self.pluginArgumentLimit : nil
        guard let limit else { return nil }
        if arguments.contains("\0") { return "Arguments can’t contain a NUL character." }
        if arguments.utf8.count > limit { return "Arguments must fit within \(limit / 1024) KiB." }
        return nil
    }
    static let promptArgumentLimit = 128 * 1024
    static let pluginArgumentLimit = 64 * 1024

    // MARK: Matching

    /// The text a list filters by: the command word, or for lists that pick
    /// a value (models, tabs), the whole trimmed query.
    static func matchKey(_ query: String, wholeQuery: Bool = false) -> String {
        wholeQuery ? query.trimmingCharacters(in: .whitespacesAndNewlines) : commandWord(query)
    }

    /// Rows matching the query, best first, ties in catalog order.
    static func filter(_ rows: [Self], query: String, wholeQuery: Bool = false) -> [Self] {
        let query = matchKey(query, wholeQuery: wholeQuery).lowercased()
        guard !query.isEmpty else { return rows }
        var ranked: [(row: Self, score: Int, position: Int)] = []
        for (position, row) in rows.enumerated() {
            let score = score(row, query: query)
            if score > 0 { ranked.append((row, score, position)) }
        }
        ranked.sort { $0.score == $1.score ? $0.position < $1.position : $0.score > $1.score }
        return ranked.map(\.row)
    }

    /// The row to select after the match key changes. A selected row whose
    /// name is now the key stays selected, so completing it with Tab never
    /// moves to another command of the same name. Otherwise the first row
    /// that can run is selected.
    static func selection(in rows: [Self], current: String?, key: String) -> String? {
        if let current, let row = rows.first(where: { $0.id == current }),
           row.name.lowercased() == key.lowercased() {
            return current
        }
        return (rows.first { $0.unavailableReason == nil } ?? rows.first)?.id
    }

    /// The best match among the name (weighted highest), the description, and
    /// the search terms. Zero means no match. Mirrors the TUI's palette.
    static func score(_ row: Self, query: String) -> Int {
        var best = 0
        let name = stringScore(row.name.lowercased(), query)
        if name > 0 { best = name + 40 }
        best = max(best, stringScore(row.description.lowercased(), query))
        for term in row.searchTerms where !term.isEmpty {
            let alias = stringScore(term.lowercased(), query)
            if alias > 0 { best = max(best, alias + 15) }
        }
        return best
    }

    /// Ranks an exact match, then a prefix, then a substring (higher at a word
    /// boundary), then the query's characters in order with gaps.
    static func stringScore(_ candidate: String, _ query: String) -> Int {
        let chars = Array(candidate), needle = Array(query)
        guard !needle.isEmpty else { return 0 }
        if candidate == query { return 1000 }
        if candidate.hasPrefix(query) { return 900 + needle.count * 4 }
        if let index = find(chars, needle) {
            return 800 + needle.count * 4 - index + (wordBoundary(chars, index) ? 50 : 0)
        }
        return subsequenceScore(chars, needle)
    }

    private static func find(_ chars: [Character], _ needle: [Character]) -> Int? {
        guard needle.count <= chars.count else { return nil }
        for start in 0...(chars.count - needle.count) where Array(chars[start..<start + needle.count]) == needle {
            return start
        }
        return nil
    }

    /// Rewards consecutive characters and word starts, and penalizes a late
    /// start and a wide span.
    private static func subsequenceScore(_ chars: [Character], _ needle: [Character]) -> Int {
        var matched = 0, start = -1, last = -1, consecutive = 0, boundaries = 0
        for (index, char) in chars.enumerated() where char == needle[matched] {
            if start == -1 { start = index }
            if last == index - 1 { consecutive += 1 }
            if wordBoundary(chars, index) { boundaries += 1 }
            last = index
            matched += 1
            if matched == needle.count {
                let span = last - start + 1
                let score = 500 + needle.count * 8 + consecutive * 12 + boundaries * 10 - start * 2 - span
                return max(0, score)
            }
        }
        return 0
    }

    private static func wordBoundary(_ chars: [Character], _ index: Int) -> Bool {
        index <= 0 || " -_:/.+#@\t".contains(chars[index - 1])
    }

    // MARK: Catalog

    /// Built-ins, prompts, and plugins in one case-insensitive alphabetical
    /// list. A prompt named like a built-in is hidden, as in the TUI.
    static func catalog(builtIns: [Self], prompts: [PromptCommand] = [], plugins: [PluginCommand] = []) -> [Self] {
        let names = Set(builtIns.map(\.name))
        let rows = builtIns + promptCatalog(prompts.filter { !names.contains($0.name) }) + pluginCatalog(plugins)
        return rows.sorted {
            let left = $0.name.lowercased(), right = $1.name.lowercased()
            return left == right ? $0.id < $1.id : left < right
        }
    }

    static func promptCatalog(_ commands: [PromptCommand]) -> [Self] {
        commands.map {
            .init(id: "prompt:" + $0.name, name: $0.name, hint: $0.argumentHint ?? "", description: $0.description,
                  meta: $0.sourceLabel, searchTerms: [$0.sourceLabel, $0.location], prompt: $0)
        }
    }

    static func pluginCatalog(_ commands: [PluginCommand]) -> [Self] {
        commands.map {
            .init(id: $0.selectionID, name: $0.localID, hint: $0.argName.map { "<\($0)>" } ?? "",
                  description: $0.description, meta: $0.pluginID, searchTerms: [$0.id, $0.pluginID], plugin: $0)
        }
    }

    /// The TUI's built-in commands, then the Mac's tile commands. Optional
    /// rows appear only where they apply.
    static func builtIns(scratchpad: Bool = true, temporary: Bool = false, terminal: Bool = true) -> [Self] {
        var rows: [Self] = [
            .init(id: "cd", name: "cd", hint: "[path]", description: "Change working directory", searchTerms: ["cwd", "directory", "folder"]),
            .init(id: "compact", name: "compact", description: "Compact session context", searchTerms: ["summarize", "shrink"]),
            .init(id: "debug", name: "debug", description: "Show session diagnostics", searchTerms: ["details", "usage", "tokens", "cost"]),
            .init(id: "diff", name: "diff", description: "Review working-tree changes", searchTerms: ["changes", "working", "tree"]),
            .init(id: "files", name: "files", description: "Open a workspace file", searchTerms: ["file", "open", "browse"]),
            .init(id: "fork", name: "fork", hint: "[message]", description: "Fork the current session into a linked child session", searchTerms: ["branch"]),
            .init(id: "login", name: "login", description: "Connect another provider", searchTerms: ["auth", "connect", "provider"]),
            .init(id: "mcp", name: "mcp", description: "Show configured MCP servers", searchTerms: ["tools", "connections", "status"]),
            .init(id: "model", name: "model", description: "Change session model", searchTerms: ["engine"]),
            .init(id: "name", name: "name", hint: "[name]", description: "Rename session", searchTerms: ["rename", "title"]),
            .init(id: "new", name: "new", description: "Start a new session"),
            .init(id: "quit", name: "quit", description: "Exit Kit", searchTerms: ["close", "exit"]),
            .init(id: "refresh-models", name: "refresh-models", description: "Update the provider catalog of models", searchTerms: ["catalog", "models"]),
            .init(id: "reload", name: "reload", description: "Reload session context", searchTerms: ["agents", "context", "refresh"]),
            .init(id: "sessions", name: "sessions", description: "Browse sessions", searchTerms: ["list", "resume", "switch", "threads"]),
            .init(id: "subagents", name: "subagents", description: "Inspect delegated work", searchTerms: ["agents", "delegates", "children"]),
            .init(id: "tabs", name: "tabs", description: "Open a workspace tab", searchTerms: ["panes", "workspace"]),
            .init(id: "theme", name: "theme", description: "Choose UI colors", searchTerms: ["appearance", "colors"]),
            .init(id: "thinking", name: "thinking", description: "Change reasoning effort", searchTerms: ["reasoning", "effort"]),
            .init(id: "browser", name: "browser", hint: "[url]", description: "Open a web page in a browser tab", searchTerms: ["web", "url", "page", "localhost"]),
            .init(id: "split-right", name: "split-right", description: "Move the selected tab into a new tile on the right", searchTerms: ["tile"]),
            .init(id: "split-down", name: "split-down", description: "Move the selected tab into a new tile below", searchTerms: ["tile"]),
            .init(id: "zoom", name: "zoom", description: "Zoom or unzoom the focused tile", searchTerms: ["tile", "maximize"]),
            .init(id: "equalize", name: "equalize", description: "Size every tile evenly", searchTerms: ["tile", "balance"]),
            .init(id: "join-tabs", name: "join-tabs", description: "Gather every tab into one tile", searchTerms: ["tile", "merge"])
        ]
        if terminal {
            rows.append(.init(id: "terminal", name: "terminal", description: "Open a shell in the session's directory",
                              searchTerms: ["shell", "console", "command line"]))
        }
        if scratchpad {
            rows.append(.init(id: "scratchpad", name: "scratchpad", description: "Open shared working notes", searchTerms: ["notes", "memory"]))
        }
        if temporary {
            rows.append(.init(id: "dispose", name: "dispose", description: "Dispose temporary session", searchTerms: ["delete", "remove"]))
        }
        return rows
    }
}
