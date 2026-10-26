import AppKit
import SwiftUI
import Testing
@testable import Kit

struct PaletteCommandTests {
    @Test func catalogListsTheTUICommandsThenTileCommands() {
        let names = PaletteCommand.builtIns().map(\.name)
        #expect(names == [
            "cd", "compact", "debug", "diff", "files", "fork", "login", "mcp", "model", "name", "new", "quit",
            "refresh-models", "reload", "sessions", "subagents", "tabs", "theme", "thinking",
            "browser", "split-right", "split-down", "zoom", "equalize", "join-tabs", "local-file", "terminal", "scratchpad"
        ])
        #expect(PaletteCommand.builtIns(terminal: false).contains { $0.name == "terminal" } == false)
        #expect(PaletteCommand.builtIns(scratchpad: false).contains { $0.name == "scratchpad" } == false)
        #expect(PaletteCommand.builtIns(temporary: true).last?.name == "dispose")
    }

    @Test func builtInsCarryTheTUIArgumentHintsAndDescriptions() {
        let rows = Dictionary(uniqueKeysWithValues: PaletteCommand.builtIns().map { ($0.name, $0) })
        #expect(rows["cd"]?.hint == "[path]")
        #expect(rows["fork"]?.hint == "[message]")
        #expect(rows["name"]?.hint == "[name]")
        #expect(rows["browser"]?.hint == "[url]")
        #expect(rows["diff"]?.description == "Review working-tree changes")
        #expect(rows["scratchpad"]?.description == "Open shared working notes")
        #expect(rows["subagents"]?.description == "Inspect delegated work")
        #expect(rows["local-file"]?.description == "View a file on this Mac")
    }

    @Test func builtInsPromptsAndPluginsSortTogetherIgnoringCase() {
        let prompts = [
            PromptCommand(name: "Review", description: "Review", source: "project", location: "/repo/review.md"),
            PromptCommand(name: "alpha", description: "First", source: "user", location: "/home/alpha.md"),
            // A prompt named like a built-in is hidden, as in the TUI.
            PromptCommand(name: "diff", description: "Shadow", source: "project", location: "/repo/diff.md")
        ]
        let plugin = PluginCommand(id: "demo.echo", instance: "host:1", description: "Echo", argName: nil)
        let rows = PaletteCommand.catalog(builtIns: PaletteCommand.builtIns(), prompts: prompts, plugins: [plugin])
        #expect(rows.map { $0.name.lowercased() } == rows.map { $0.name.lowercased() }.sorted())
        #expect(rows.filter { ["alpha", "cd", "echo", "reload", "Review"].contains($0.name) }.map(\.name)
                == ["alpha", "cd", "echo", "reload", "Review"])
        #expect(rows.filter { $0.name == "diff" }.map(\.id) == ["diff"])
    }

    @Test func promptRowsShowTheirHintAndSourceAndMatchTheirLocation() throws {
        let prompt = PromptCommand(name: "review", description: "Review recent changes", source: "claude_project",
                                   location: "/repo/.claude/commands/review.md", argumentHint: "<scope>")
        let row = try #require(PaletteCommand.promptCatalog([prompt]).first)
        #expect(row.id == "prompt:review")
        #expect(row.hint == "<scope>")
        #expect(row.meta == "claude")
        for query in ["review", "claude", "/repo/.claude/commands/review.md"] {
            #expect(PaletteCommand.filter([row], query: query).map(\.id) == ["prompt:review"])
        }
    }

    @Test func pluginRowsShowTheirLocalNameArgumentAndPlugin() throws {
        let plugin = PluginCommand(id: "demo.say-hello", instance: "host:1", description: "Greet", argName: "name")
        let row = try #require(PaletteCommand.pluginCatalog([plugin]).first)
        #expect(row.id == "plugin:demo.say-hello:host:1")
        #expect(row.name == "say-hello")
        #expect(row.hint == "<name>")
        #expect(row.meta == "demo")
        #expect(PaletteCommand.filter([row], query: "demo.say").map(\.name) == ["say-hello"])
    }

    @Test func argumentsFollowTheCommandWordLiterally() {
        #expect(PaletteCommand.commandWord("  review  auth module ") == "review")
        #expect(PaletteCommand.commandWord("/review auth") == "review")
        // Like the TUI, only a space separates the command from its input.
        #expect(PaletteCommand.commandWord("cd\tfoo bar") == "cd\tfoo")
        #expect(PaletteCommand.commandArguments("  review  auth module ") == " auth module ")
        #expect(PaletteCommand.commandArguments("review") == "")
        #expect(PaletteCommand.commandArguments("review ") == "")
    }

    @Test func filteringRanksNamesFirstAndIgnoresArguments() {
        let review = PromptCommand(name: "review", description: "Review recent changes", source: "project",
                                   location: "/repo/review.md", argumentHint: "<scope>")
        let rows = PaletteCommand.catalog(builtIns: PaletteCommand.builtIns(), prompts: [review])
        // Board 8: the exact name, then the built-in whose description starts with it.
        #expect(PaletteCommand.filter(rows, query: "review auth module").map(\.name) == ["review", "diff"])
        #expect(PaletteCommand.filter(rows, query: "rel").first?.name == "reload")
        #expect(PaletteCommand.filter(rows, query: "CWD").first?.name == "cd")
        #expect(PaletteCommand.filter(rows, query: "deploy").isEmpty)
        #expect(PaletteCommand.filter(rows, query: "  ").map(\.id) == rows.map(\.id))
    }

    @Test func stringScoresOrderExactPrefixBoundarySubstringAndSubsequence() {
        let exact = PaletteCommand.stringScore("reload", "reload")
        let prefix = PaletteCommand.stringScore("reload context", "reload")
        let boundary = PaletteCommand.stringScore("session reload", "reload")
        let inner = PaletteCommand.stringScore("autoreload", "reload")
        let gaps = PaletteCommand.stringScore("refresh-models", "rfm")
        #expect(exact > prefix)
        #expect(prefix > boundary)
        #expect(boundary > inner)
        #expect(inner > gaps)
        #expect(gaps > 0)
        #expect(PaletteCommand.stringScore("compact", "xyz") == 0)
    }

    @Test func newQueriesSelectTheFirstCommandThatCanRun() {
        var compact = PaletteCommand(id: "compact", name: "compact", description: "")
        compact.unavailableReason = "idle only"
        let debug = PaletteCommand(id: "debug", name: "debug", description: "")
        #expect(PaletteCommand.selection(in: [compact, debug], current: nil, key: "") == "debug")
        #expect(PaletteCommand.selection(in: [compact, debug], current: "compact", key: "d") == "debug")
        #expect(PaletteCommand.selection(in: [compact], current: nil, key: "") == "compact")
        #expect(PaletteCommand.selection(in: [], current: nil, key: "x") == nil)
    }

    @Test func completingACommandKeepsItOverOthersWithTheSameName() {
        // Plugin rows show their local name, so names repeat across plugins
        // and can match a built-in.
        let plugins = [
            PluginCommand(id: "git.status", instance: "host:1", description: "Git status", argName: nil),
            PluginCommand(id: "jj.status", instance: "host:1", description: "Jujutsu status", argName: nil),
            PluginCommand(id: "git.diff", instance: "host:1", description: "Git diff", argName: nil)
        ]
        let catalog = PaletteCommand.catalog(builtIns: PaletteCommand.builtIns(), plugins: plugins)
        for id in ["plugin:jj.status:host:1", "plugin:git.diff:host:1", "diff"] {
            let row = catalog.first { $0.id == id }!
            let query = row.name + " "
            let key = PaletteCommand.commandWord(query)
            let rows = PaletteCommand.filter(catalog, query: query)
            #expect(PaletteCommand.selection(in: rows, current: id, key: key) == id)
        }
    }

    @Test func valueListsMatchTheWholeQuery() {
        let models = [
            PaletteCommand(id: "claude-sonnet", name: "Claude Sonnet 4.6", description: ""),
            PaletteCommand(id: "claude-opus", name: "Claude Opus 4.6", description: "")
        ]
        #expect(PaletteCommand.filter(models, query: "claude opus", wholeQuery: true).map(\.id) == ["claude-opus"])
        #expect(PaletteCommand.filter(models, query: "claude opus").map(\.id) == ["claude-sonnet", "claude-opus"])
        #expect(PaletteCommand.matchKey(" /claude opus ", wholeQuery: true) == "/claude opus")
    }

    @MainActor @Test func closingThePaletteReturnsTheNextOpeningToTheCommandList() {
        let ui = SessionUIState(demo: true)
        ui.palette = true
        ui.subagentsPresented = true
        ui.palette = false
        #expect(ui.subagentsPresented == false)
        // Session > Subagents opens the palette straight to the list.
        ui.subagentsPresented = true
        ui.palette = true
        #expect(ui.subagentsPresented)
    }

    @MainActor @Test func searchFieldTurnsEditingCommandsIntoPaletteActions() {
        var text = "rev"
        var moves: [Int] = []
        var submits = 0, cancels = 0
        let field = PaletteSearchField(
            text: Binding(get: { text }, set: { text = $0 }), placeholder: "Search commands…",
            move: { moves.append($0) }, complete: { "review " },
            submit: { submits += 1 }, cancel: { cancels += 1 })
        let coordinator = PaletteSearchField.Coordinator(field)
        let control = NSTextField(string: "rev")
        let editor = NSTextView()
        for selector in [#selector(NSResponder.moveDown(_:)), #selector(NSResponder.moveUp(_:)),
                         #selector(NSResponder.insertTab(_:)), #selector(NSResponder.insertNewline(_:)),
                         #selector(NSResponder.cancelOperation(_:))] {
            #expect(coordinator.control(control, textView: editor, doCommandBy: selector))
        }
        #expect(moves == [1, -1])
        #expect(text == "review ")
        #expect(control.stringValue == "review ")
        #expect(submits == 1)
        #expect(cancels == 1)
        #expect(!coordinator.control(control, textView: editor, doCommandBy: #selector(NSResponder.moveLeft(_:))))
    }
}
