import Foundation
import KitClient
import KitTranscript
import Testing
@testable import Kit

/// A file link in the transcript or a Markdown preview opens its file at
/// the lines it names: in the session's folder, or else from its server.
struct FileLinkTests {
    static func lines(_ start: Int, _ end: Int? = nil) -> FileLineRange { FileLineRange(start: start, end: end)! }

    @Test func aTranscriptsFileLinkOpensItsFileAtItsLines() throws {
        let relative = try #require(FilePaths.link("src/a.go", lines: Self.lines(42, 50)))
        #expect(OpenedFile.linked(relative, cwd: "/repo") == OpenedFile(path: "src/a.go", lines: Self.lines(42, 50)))
        let absolute = try #require(FilePaths.link("/repo/docs/b.md", lines: Self.lines(7)))
        #expect(OpenedFile.linked(absolute, cwd: "/repo") == OpenedFile(path: "docs/b.md", lines: Self.lines(7)))
        let whole = try #require(FilePaths.link("README.md"))
        #expect(OpenedFile.linked(whole, cwd: "/repo") == OpenedFile(path: "README.md", lines: nil))
    }

    /// A file outside the session's folder opens from its server, view-only.
    @Test func aFileElsewhereOpensFromTheServer() throws {
        #expect(OpenedFile.linked(try #require(FilePaths.link("/etc/hosts", lines: Self.lines(2))), cwd: "/repo")
            == OpenedFile(path: "/etc/hosts", lines: Self.lines(2), onServer: true))
        #expect(OpenedFile.linked(try #require(FilePaths.link("~/notes.md")), cwd: "/repo")
            == OpenedFile(path: "~/notes.md", onServer: true))
        #expect(OpenedFile.linked(try #require(FilePaths.link("../other/a.go")), cwd: "/repo")
            == OpenedFile(path: "/other/a.go", onServer: true))
        #expect(OpenedFile.at("/repo/src/../README.md", cwd: "/repo") == OpenedFile(path: "README.md"))
    }

    @Test func anotherLinkOrAPathWithNoStartOpensNothing() throws {
        #expect(OpenedFile.linked(try #require(FilePaths.link("src/a.go")), cwd: nil) == nil)
        #expect(OpenedFile.linked(try #require(URL(string: "https://example.com/a.go#L3")), cwd: "/repo") == nil)
        #expect(OpenedFile.at("~", cwd: "/repo") == nil)
        #expect(OpenedFile.at("/", cwd: "/repo") == nil)
    }

    /// A relative link in a Markdown file elsewhere starts from that file's folder.
    @Test func aFileElsewhereLinksFromItsOwnFolder() throws {
        let link = try #require(FilePaths.link("../b.md"))
        #expect(OpenedFile.linked(link, cwd: "/repo", folder: "/srv/docs/guide") == OpenedFile(path: "/srv/docs/b.md", onServer: true))
        #expect(OpenedFile.linked(link, cwd: "/repo", folder: "~/notes/day") == OpenedFile(path: "~/notes/b.md", onServer: true))
        #expect(OpenedFile.linked(try #require(FilePaths.link("README.md")), cwd: "/repo", folder: "/repo")
            == OpenedFile(path: "README.md"))
    }

    @Test func aMarkdownFilesRelativeLinkStartsFromItsFolder() throws {
        let link = try #require(FilePaths.link("../README.md", lines: Self.lines(3, 4)))
        #expect(OpenedFile.linked(link, cwd: "/repo", folder: "docs/guide") == OpenedFile(path: "docs/README.md", lines: Self.lines(3, 4)))
        let absolute = try #require(FilePaths.link("/repo/a.md"))
        #expect(OpenedFile.linked(absolute, cwd: "/repo", folder: "docs") == OpenedFile(path: "a.md"))
    }

    @Test func transcriptMarkdownLinksCarryTheirLines() throws {
        let content = MarkdownContent(source: "See [a](src/a.go#L3-L5) and [b](src/b.go#L7).")
        guard case .paragraph(let text)? = content.blocks.first else { Issue.record("no paragraph"); return }
        let opened = text.runs.compactMap(\.link).map { OpenedFile.linked($0, cwd: "/repo") }
        #expect(opened == [OpenedFile(path: "src/a.go", lines: Self.lines(3, 5)), OpenedFile(path: "src/b.go", lines: Self.lines(7))])
    }

    @Test func openedFilesAreDistinctByTheirLines() {
        #expect(OpenedFile(path: "a.go", lines: Self.lines(3)).id != OpenedFile(path: "a.go", lines: Self.lines(4)).id)
        #expect(OpenedFile(path: "a.go").id == "a.go:")
    }
}
