import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct DiffCommentsTests {
    /// Rows: 0 `func greet() {` (1→1), 1 −old 2, 2 +new 2, 3 +new 3,
    /// 4 `}` (3→4), 5 ⋯, 6 context (10→11), 7 +new 12.
    static func document() throws -> DiffDocument {
        let observation = #"{"sessionId":"session_test","target":{"id":"difftarget_test","workspaceId":"workspace_test","kind":"working_tree","repositoryPath":"/tmp","base":{"kind":"commit","oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"head":{"kind":"working_tree"}},"revision":"diffrev_test","head":{"state":"commit","oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"indexSummary":"index_test","complete":true,"omissions":[]}"#
        let file = #"{"path":"Sources/greet.swift","fileRevision":"diff_file_test","change":"modified","old":{"kind":"regular","mode":33188},"new":{"kind":"regular","mode":33188},"contentState":"text","additions":3,"deletions":1}"#
        func line(_ kind: String, _ old: Int?, _ new: Int?, _ text: String) -> String {
            let numbers = [old.map { "\"oldLine\":\($0)" }, new.map { "\"newLine\":\($0)" }].compactMap { $0 }.joined(separator: ",")
            return #"{"kind":"\#(kind)",\#(numbers),"content":"\#(text)","hasTerminatingLF":true}"#
        }
        let first = [line("context", 1, 1, "func greet() {"), line("deletion", 2, nil, "  print(1)"),
                     line("addition", nil, 2, "  print(2)"), line("addition", nil, 3, "  print(3)"),
                     line("context", 3, 4, "}")]
        let second = [line("context", 10, 11, "let a = 1"), line("addition", nil, 12, "let b = 2")]
        let hunks = #"[{"oldStart":1,"oldCount":3,"newStart":1,"newCount":4,"continuedBefore":false,"continuedAfter":false,"lines":[\#(first.joined(separator: ","))]},{"oldStart":10,"oldCount":1,"newStart":11,"newCount":2,"continuedBefore":false,"continuedAfter":false,"lines":[\#(second.joined(separator: ","))]}]"#
        let page = try JSONDecoder().decode(WireFileDiffPage.self, from: Data(
            #"{"observation":\#(observation),"file":\#(file),"computation":{"state":"complete"},"hunks":\#(hunks)}"#.utf8))
        return DiffDocument(observation: page.observation, file: page.file, hunks: page.hunks!)
    }

    static func lines(_ anchor: WireAnnotationAnchor?) -> String? {
        anchor?.workingTreeDiff.map { "\($0.side) \($0.startLine)–\($0.endLine)" }
    }

    @Test func aSelectionTakesTheSideOfTheLineItStartsOn() throws {
        let rows = try Self.document().rows
        #expect(rows.map(DiffComments.side) == ["new", "old", "new", "new", "new", "new", "new", "new"])
        #expect(rows.map(DiffComments.selectable) == [true, true, true, true, true, false, true, true])
    }

    @Test func theHandleStretchesASelectionAsFarAsItsHunkAllows() throws {
        let document = try Self.document()
        // Over the removed line, which has no new number, to the hunk's end.
        #expect(Self.lines(DiffComments.extend(document, from: 0, toward: 4, side: "new")) == "new 1–4")
        // Stops before the gap between hunks.
        #expect(Self.lines(DiffComments.extend(document, from: 2, toward: 7, side: "new")) == "new 2–4")
        // A removed line reaches only other removed lines.
        #expect(Self.lines(DiffComments.extend(document, from: 1, toward: 4, side: "old")) == "old 2–2")
        // Pulled back to where it started, it's one line.
        #expect(Self.lines(DiffComments.extend(document, from: 3, toward: 3, side: "new")) == "new 3–3")
        #expect(DiffComments.extend(document, from: 5, toward: 7, side: "new") == nil)
    }

    @Test func theBoxSaysWhichLinesItsCommentIsOn() throws {
        let document = try Self.document()
        #expect(DiffComments.title(try #require(document.anchor(rows: 2...4, side: "new"))) == "Comment on lines 2–4")
        #expect(DiffComments.title(try #require(document.anchor(rows: 6...6, side: "new"))) == "Comment on line 11")
        #expect(DiffComments.title(try #require(document.anchor(rows: 1...1, side: "old"))) == "Comment on removed line 2")
    }

    @Test func aCommentKeepsItsRowsWhileItsFileIsUnchanged() throws {
        let document = try Self.document()
        func anchor(revision: String, file: String) -> WireAnnotationAnchor {
            .init(kind: .value1, workspaceFile: nil, workingTreeDiff: .init(
                targetId: "difftarget_test", targetRevision: revision, path: "Sources/greet.swift",
                fileRevision: file, side: "new", startLine: 2, endLine: 4))
        }
        #expect(DiffComments.rows(document, anchor(revision: "diffrev_test", file: "diff_file_test")) == 2...4)
        // Another file changed, so the diff's revision moved on.
        #expect(DiffComments.rows(document, anchor(revision: "diffrev_later", file: "diff_file_test")) == 2...4)
        // This file changed.
        #expect(DiffComments.rows(document, anchor(revision: "diffrev_later", file: "diff_file_other")) == nil)
    }

    @Test func aCommentWhoseCodeChangedSitsWhereItsLinesAreNow() throws {
        let document = try Self.document()
        func note(target: String = "difftarget_test", path: String = "Sources/greet.swift",
                  _ start: Int, _ end: Int) throws -> FileAnnotation {
            try FileAnnotation(id: 1, anchor: .init(kind: .value1, workspaceFile: nil, workingTreeDiff: .init(
                targetId: target, targetRevision: "diffrev_earlier", path: path,
                fileRevision: "diff_file_earlier", side: "new", startLine: start, endLine: end)),
                body: "Why?", source: "", stale: true, complete: true)
        }
        // The file changed, so its exact rows are gone, but its lines are here.
        #expect(DiffComments.rows(document, try note(2, 3).anchor) == nil)
        #expect(DiffComments.nearby(document, try note(2, 3).anchor) == 2...3)
        #expect(DiffComments.nearby(document, try note(11, 12).anchor) == 6...7)
        // Its lines left the diff: it leads the file instead.
        #expect(DiffComments.nearby(document, try note(40, 41).anchor) == nil)
        #expect(DiffComments.belongs(try note(40, 41), to: document))
        // Comments on another comparison or file aren't this file's.
        #expect(!DiffComments.belongs(try note(target: "difftarget_other", 2, 3), to: document))
        #expect(!DiffComments.belongs(try note(path: "README.md", 2, 3), to: document))
    }

    @Test func commentsSplitTheRowsAfterTheirLastLine() {
        #expect(DiffComments.segments(count: 8, after: []) == [0..<8])
        #expect(DiffComments.segments(count: 8, after: [4]) == [0..<5, 5..<8])
        #expect(DiffComments.segments(count: 8, after: [6, 1]) == [0..<2, 2..<7, 7..<8])
        // A comment on the last row leaves nothing after it.
        #expect(DiffComments.segments(count: 8, after: [7]) == [0..<8])
        #expect(DiffComments.segments(count: 0, after: []) == [0..<0])
    }

    @Test func aCardSaysWhichLinesItsCommentIsOn() throws {
        func note(_ side: String, _ start: Int, _ end: Int) throws -> FileAnnotation {
            try FileAnnotation(id: 1, anchor: .init(kind: .value1, workspaceFile: nil, workingTreeDiff: .init(
                targetId: "difftarget_test", targetRevision: "diffrev_test", path: "Sources/greet.swift",
                fileRevision: "diff_file_test", side: side, startLine: start, endLine: end)),
                body: "Why?", source: "", stale: false, complete: true)
        }
        #expect(DiffComments.caption(try note("new", 1671, 1673)) == "Lines 1671–1673 · goes with your next message")
        #expect(DiffComments.caption(try note("new", 12, 12)) == "Line 12 · goes with your next message")
        #expect(DiffComments.caption(try note("old", 2, 3)) == "Removed lines 2–3 · goes with your next message")
    }
}
