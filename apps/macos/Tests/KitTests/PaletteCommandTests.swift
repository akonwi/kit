import Testing
@testable import Kit

struct PaletteCommandTests {
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
