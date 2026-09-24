import Foundation

/// Command identity is independent of its visible label and searchable aliases.
struct PaletteCommand: Identifiable {
    let id: String
    let name: String
    let description: String
    let icon: String
    var aliases: [String] = []
    var demoOnly = false
    var plugin: PluginCommand? = nil

    func matches(_ query: String) -> Bool {
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        let text = ([name, description] + aliases).joined(separator: " ")
        return query.split(whereSeparator: { $0.isWhitespace }).allSatisfy {
            text.localizedCaseInsensitiveContains(String($0))
        }
    }

    static func pluginCatalog(_ commands: [PluginCommand]) -> [Self] {
        commands.map { .init(id: $0.selectionID, name: $0.name, description: $0.description,
                             icon: "puzzlepiece.extension", plugin: $0) }
    }

    static func catalog(dark: Bool) -> [Self] {
        [
            .init(id: "Change working directory", name: "cd", description: "Change working directory", icon: "folder", aliases: ["cwd", "directory", "folder"]),
            .init(id: "compact", name: "compact", description: "Compact session context", icon: "arrow.down.right.and.arrow.up.left", aliases: ["summarize", "shrink"]),
            .init(id: "Rename session", name: "name", description: "Rename session", icon: "pencil", aliases: ["rename", "title"]),
            .init(id: "Reload session context", name: "reload", description: "Reload session context", icon: "arrow.clockwise", aliases: ["agents", "context", "refresh"]),
            .init(id: "Session details", name: "debug", description: "Show session diagnostics", icon: "chart.bar", aliases: ["details", "usage", "tokens", "cost"]),
            .init(id: "Fork session", name: "fork", description: "Fork the current session into a linked child session", icon: "arrow.triangle.branch", aliases: ["branch"]),
            .init(id: "Switch session", name: "sessions", description: "Browse sessions", icon: "rectangle.stack", aliases: ["list", "resume", "switch", "threads"]),
            .init(id: "Dispose temporary session", name: "dispose", description: "Dispose temporary session", icon: "trash", aliases: ["delete", "remove"]),
            .init(id: "Open Code Review", name: "diffs", description: "Browse diffs", icon: "chevron.left.forwardslash.chevron.right", demoOnly: false),
            .init(id: "Open Scratchpad", name: "scratchpad", description: "Open scratchpad", icon: "note.text"),
            .init(id: "Find workspace file", name: "files", description: "Find workspace file", icon: "doc.text.magnifyingglass"),
            .init(id: "Switch appearance", name: "appearance", description: "Switch to \(dark ? "light" : "dark") appearance", icon: "circle.lefthalf.filled", aliases: ["theme", "colors"])
        ]
    }
}
