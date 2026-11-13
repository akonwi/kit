import Foundation
import KitClient
import Testing

/// The find bar's matcher reads the cases in `testdata/find`, which the Go
/// and web matchers read too.
struct TextFindTests {
    /// `testdata/find/cases.json` at the repository root, five levels above
    /// this file.
    static let fixture = (0..<5).reduce(URL(fileURLWithPath: #filePath)) { url, _ in
        url.deletingLastPathComponent()
    }.appendingPathComponent("testdata/find/cases.json")

    struct Fixture: Decodable {
        let cases: [Case]
        let labels: [Label]
    }

    struct Case: Decodable, CustomTestStringConvertible, Sendable {
        let name: String
        let query: String
        let matchCase: Bool?
        let wholeWord: Bool?
        let limit: Int?
        let capped: Bool?
        let current: Int?
        let marked: String
        var testDescription: String { name }
    }

    struct Label: Decodable, CustomTestStringConvertible, Sendable {
        let current: Int
        let total: Int
        let capped: Bool?
        let searchedLines: Int?
        let label: String
        var testDescription: String { label }
    }

    static let loaded: Fixture? = (try? Data(contentsOf: fixture)).flatMap { try? JSONDecoder().decode(Fixture.self, from: $0) }
    static var cases: [Case] { loaded?.cases ?? [] }
    static var labels: [Label] { loaded?.labels ?? [] }

    /// The text of `marked` without its markers, the UTF-16 ranges between
    /// `⟦` and `⟧`, and the UTF-16 offset of `‸`.
    static func unmark(_ marked: String) -> (text: String, ranges: [Range<Int>], anchor: Int?) {
        var text = ""
        var ranges: [Range<Int>] = []
        var start = 0
        var anchor: Int?
        for scalar in marked.unicodeScalars {
            switch scalar {
            case "⟦": start = text.utf16.count
            case "⟧": ranges.append(start..<text.utf16.count)
            case "‸": anchor = text.utf16.count
            default: text.unicodeScalars.append(scalar)
            }
        }
        return (text, ranges, anchor)
    }

    @Test func theFixtureIsPresent() {
        #expect(Self.cases.count >= 50)
        #expect(Self.labels.count >= 9)
    }

    @Test(arguments: cases) func findAgreesWithTheSharedCases(_ test: Case) {
        let (text, ranges, anchor) = Self.unmark(test.marked)
        let options = TextFind.Options(matchCase: test.matchCase ?? false, wholeWord: test.wholeWord ?? false,
                                       limit: test.limit ?? TextFind.defaultLimit)
        let result = TextFind.find(test.query, in: text, options: options)
        #expect(result == TextFind.Result(matches: ranges, capped: test.capped ?? false))
        if let current = test.current {
            #expect(TextFind.following(result.matches, offset: anchor ?? 0) == current)
        }
    }

    @Test(arguments: labels) func labelsAgreeWithTheSharedCases(_ test: Label) {
        #expect(TextFind.label(current: test.current, total: test.total, capped: test.capped ?? false,
                               searchedLines: test.searchedLines ?? 0) == test.label)
    }

    @Test func rangesAddressTheTextAsNSString() {
        let text = "😀 Handler(handler)"
        let ranges = TextFind.find("handler", in: text).matches
        #expect(ranges.map { (text as NSString).substring(with: NSRange($0)) } == ["Handler", "handler"])
    }

    @Test func tenThousandMatchesAreCountedByDefault() {
        let exactly = String(repeating: "ab ", count: TextFind.defaultLimit)
        let all = TextFind.find("ab", in: exactly)
        #expect(all.matches.count == TextFind.defaultLimit)
        #expect(all.matches.last == 3 * (TextFind.defaultLimit - 1)..<3 * (TextFind.defaultLimit - 1) + 2)
        #expect(!all.capped)
        let more = TextFind.find("ab", in: exactly + "ab")
        #expect(more.matches.count == TextFind.defaultLimit)
        #expect(more.capped)
    }

    @Test func followingWithoutMatchesIsNone() {
        #expect(TextFind.following([], offset: 0) == nil)
    }

    /// A file at the most a client receives, 5,000 lines of about 200
    /// characters, searches in well under the time between keystrokes,
    /// whether the search reads it all or stops at the limit.
    @Test func aLargeFileSearchesQuickly() {
        let line = String(repeating: "func (s *Server) handler(next http.Handler) http.Handler { return next } ", count: 3)
        let text = String(repeating: line + "\n", count: 5_000)
        let clock = ContinuousClock()
        var none = TextFind.Result(), many = TextFind.Result()
        let elapsed = clock.measure {
            none = TextFind.find("handlers", in: text, options: .init(wholeWord: true))
            many = TextFind.find("e", in: text)
        }
        #expect(none.matches.isEmpty)
        #expect(many.matches.count == TextFind.defaultLimit && many.capped)
        #expect(elapsed < .seconds(1), "searching \(text.utf16.count) characters twice took \(elapsed)")
    }
}
