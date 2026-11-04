import KitClient
import Testing

struct FilePathSearchTests {
    @Test func ranksExactThenPrefixThenSubstringThenLetters() {
        #expect(FilePathSearch.filtered(query: "src", paths: ["other/src.go", "src/main.go", "src/", "src", "scratch"]) ==
            ["src", "src/main.go", "src/", "other/src.go", "scratch"])
        #expect(FilePathSearch.filtered(query: "tsh", paths: ["internal/tui/app.go", "internal/tui/shell.go"]) == ["internal/tui/shell.go"])
        #expect(FilePathSearch.filtered(query: "msw", paths: ["README.md", "main.swift"]) == ["main.swift"])
    }

    @Test func ignoresCaseAndSurroundingSpaceAndKeepsEveryPathForNoQuery() {
        #expect(FilePathSearch.filtered(query: "  ReadMe ", paths: ["docs/readme.md", "README.md"]) == ["README.md", "docs/readme.md"])
        #expect(FilePathSearch.filtered(query: "", paths: ["b", "a"]) == ["b", "a"])
    }
}
