import Foundation
import KitTranscript
import Testing
@testable import Kit

/// File links name lines as GitHub does in a link's target (`a.go#L42`,
/// `a.go#L42-L50`, `a.go#L42-50`); a transcript's file link carries them as
/// a `#L42-L50` fragment apart from its path.
struct FileLineParsingTests {
    static func lines(_ start: Int, _ end: Int? = nil) -> FileLineRange { FileLineRange(start: start, end: end)! }

    /// `testdata/file-links/destinations.tsv` at the repository root, five
    /// levels above this file, which every client's parser reads.
    static let fixture = (0..<5).reduce(URL(fileURLWithPath: #filePath)) { url, _ in
        url.deletingLastPathComponent()
    }.appendingPathComponent("testdata/file-links/destinations.tsv")

    struct Case: CustomTestStringConvertible, Sendable {
        let destination: String
        let path: String?
        let lines: FileLineRange?
        var testDescription: String { destination }
    }

    static var cases: [Case] {
        let text = (try? String(contentsOf: fixture, encoding: .utf8)) ?? ""
        return text.split(separator: "\n").filter { !$0.hasPrefix("# ") }.map { line in
            let fields = line.split(separator: "\t", omittingEmptySubsequences: false).map(String.init)
            let bounds = fields[2].split(separator: "-").compactMap { Int($0) }
            return Case(destination: fields[0], path: fields[1] == "-" ? nil : fields[1],
                        lines: bounds.first.flatMap { FileLineRange(start: $0, end: bounds.last) })
        }
    }

    @Test func theFixtureIsPresent() {
        #expect(Self.cases.count >= 25)
    }

    @Test(arguments: cases) func destinationsNameFilesAndLines(_ test: Case) {
        let location = FilePaths.linkedLocation(destination: test.destination)
        #expect(location == test.path.map { FileLocation(path: $0, lines: test.lines) })
        #expect(FilePaths.linkedPath(destination: test.destination) == test.path)
    }

    @Test func linesParseAndSpellAsFragments() {
        #expect(FileLineRange(fragment: "L7") == Self.lines(7))
        #expect(FileLineRange(fragment: "L7-L9") == Self.lines(7, 9))
        #expect(FileLineRange(fragment: "L7-9") == Self.lines(7, 9))
        #expect(FileLineRange(fragment: "L7-L7") == Self.lines(7))
        for fragment in ["L0", "L9-L7", "7", "L", "L7-", "L-7", "usage", "L7 "] { #expect(FileLineRange(fragment: fragment) == nil, "\(fragment)") }
        #expect(Self.lines(42).fragment == "L42")
        #expect(Self.lines(42, 50).fragment == "L42-L50")
        #expect(FileLocation(path: "src/a.go", lines: Self.lines(42, 50)).withLines == "src/a.go#L42-L50")
        #expect(FileLocation(path: "src/a.go", lines: Self.lines(42)).withLines == "src/a.go#L42")
        #expect(FileLocation(path: "src/a.go").withLines == "src/a.go")
    }

    @Test func fileLinksCarryTheirLinesApartFromThePath() throws {
        let url = try #require(FilePaths.link("/Users/me/My File #2.txt", lines: Self.lines(3, 5)))
        #expect(url.absoluteString == "kit-file:/Users/me/My%20File%20%232.txt#L3-L5")
        #expect(FilePaths.path(fromLink: url) == "/Users/me/My File #2.txt")
        #expect(FilePaths.lines(fromLink: url) == Self.lines(3, 5))
        #expect(FilePaths.lines(fromLink: try #require(FilePaths.link("/a.go"))) == nil)
        #expect(FilePaths.lines(fromLink: try #require(URL(string: "kit-file:/a.go#usage"))) == nil)
        #expect(FilePaths.lines(fromLink: try #require(URL(string: "https://example.com/a.go#L3"))) == nil)
    }

    @Test func markdownLinksKeepTheirLines() throws {
        let content = MarkdownContent(source: "See [a](src/a.go#L3-L5), [b](src/b.go#L7), [c](src/c.go), and [d](src/d.go:7).")
        guard case .paragraph(let text)? = content.blocks.first else { Issue.record("no paragraph"); return }
        let links = text.runs.compactMap(\.link)
        #expect(links == [FilePaths.link("src/a.go", lines: Self.lines(3, 5))!, FilePaths.link("src/b.go", lines: Self.lines(7))!,
                          FilePaths.link("src/c.go")!, FilePaths.link("src/d.go:7")!])
    }
}
