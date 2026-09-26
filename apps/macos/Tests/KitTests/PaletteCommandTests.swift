import Testing
@testable import Kit

struct PaletteCommandTests {
    @Test func builtInsPromptsAndPluginsSortTogetherByDisplayName() {
        let prompts = [
            PromptCommand(name: "review", description: "Review", source: "project", location: "/repo/review.md"),
            PromptCommand(name: "alpha", description: "First", source: "project", location: "/repo/alpha.md")
        ]
        let plugin = PluginCommand(id: "demo.echo", instance: "host:1", description: "Echo", argName: nil)
        let actions = PaletteCommand.sortedByName(PaletteCommand.catalog(dark: true)
            + PaletteCommand.promptCatalog(prompts) + PaletteCommand.pluginCatalog([plugin]))
        #expect(actions.map(\.name) == actions.map(\.name).sorted())
        #expect(actions.filter { ["alpha", "cd", "demo.echo", "reload", "review"].contains($0.name) }.map(\.name)
                == ["alpha", "cd", "demo.echo", "reload", "review"])
        #expect(actions.first?.name == "alpha")
    }

    @Test func customPromptsHaveDistinctSearchableRowsAndArgumentHints() throws {
        let prompt = PromptCommand(name: "review", description: "Review recent changes", source: "project",
                                   location: "/repo/.agents/prompts/review.md", argumentHint: "<scope>")
        let row = try #require(PaletteCommand.promptCatalog([prompt]).first)
        #expect(row.id == "prompt:review")
        #expect(row.name == "review")
        #expect(row.description == "Review recent changes")
        #expect(row.prompt?.argumentHint == "<scope>")
        for query in ["review", "/review", "Review recent", "project", "/repo/.agents/prompts/review.md"] {
            #expect(row.matches(query))
        }
        let invocation = PaletteCommand.splitQuery(" review \"auth module\" carefully ")
        #expect(row.matches(invocation.command))
        #expect(invocation.args == "\"auth module\" carefully")
    }

    @Test func directoryCommandMatchesItsNameDescriptionAndAliases() throws {
        let commands = PaletteCommand.catalog(dark: true)
        for query in ["cd", " CWD ", "folder", "working directory"] {
            #expect(commands.filter { $0.matches(query) }.map(\.name) == ["cd"])
        }
        let directory = try #require(commands.first { $0.name == "cd" })
        #expect(directory.description == "Change working directory")
        #expect(directory.id == "Change working directory")
    }

    @Test func sharedCommandsUseTUILabelsAndSearchTerms() {
        let commands = PaletteCommand.catalog(dark: true)
        #expect(commands.filter { $0.matches("refresh") }.map(\.name) == ["refresh-models", "reload"])
        for (query, name, description) in [
            ("rename", "name", "Rename session"),
            ("provider catalog", "refresh-models", "Update the provider catalog of models"),
            ("usage", "debug", "Show session diagnostics"),
            ("branch", "fork", "Fork the current session into a linked child session"),
            ("threads", "sessions", "Browse sessions")
        ] {
            #expect(commands.filter { $0.matches(query) }.map(\.name) == [name])
            #expect(commands.first { $0.name == name }?.description == description)
        }
    }
}
