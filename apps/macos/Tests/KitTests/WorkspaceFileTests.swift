import Foundation
import Testing
@testable import Kit

struct WorkspaceFileTests {
    @MainActor @Test func filesCommandOpensAndReusesRetainedTabs() throws {
        let command = try #require(PaletteCommand.builtIns().first { $0.name == "files" })
        #expect(command.description == "Open a workspace file")
        let workspace = WorkspaceState(demo: false)
        workspace.open(.file("main.swift"))
        workspace.open(.file("other.swift"))
        workspace.open(.file("main.swift"))
        #expect(workspace.selected == .file("main.swift"))
        #expect(workspace.panes == [.conversation, .file("main.swift"), .file("other.swift")])
        #expect(ComposerMentionState.filtered(query: "msw", paths: ["README.md", "main.swift"]) == ["main.swift"])
    }
}
