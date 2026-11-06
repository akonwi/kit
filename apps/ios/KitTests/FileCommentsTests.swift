import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct FileCommentsTests {
    private static func note(_ id: UInt64, path: String = "Sources/main.swift", lines: ClosedRange<Int>,
                             stale: Bool = false) throws -> FileAnnotation {
        try FileAnnotation(id: id, anchor: .init(kind: .value0, workspaceFile: .init(
            workspaceId: "workspace_test", path: path, fileRevision: "file_old",
            startLine: lines.lowerBound, endLine: lines.upperBound), workingTreeDiff: nil),
            body: "Why?", source: "", stale: stale, complete: true)
    }

    @Test func aCommentIsOnTheFileAsRead() throws {
        let file = try FakeFileClient.file("Sources/main.swift", "a\nb\nc\n", revision: "file_new")
        let anchor = FileComments.anchor(file, lines: 2...3)
        #expect(anchor.kind == .value0)
        #expect(anchor.workspaceFile?.workspaceId == "workspace_test")
        #expect(anchor.workspaceFile?.path == "Sources/main.swift")
        #expect(anchor.workspaceFile?.fileRevision == "file_new")
        #expect(anchor.workspaceFile.map { $0.startLine...$0.endLine } == 2...3)
        #expect(DiffComments.title(anchor) == "Comment on lines 2–3")
        #expect(DiffComments.title(FileComments.anchor(file, lines: 3...3)) == "Comment on line 3")
    }

    @Test func aSelectionGrowsDownToTwoHundredLinesOrTheFileEnd() {
        #expect(FileComments.extend(from: 10, toward: 14, count: 500) == 10...14)
        #expect(FileComments.extend(from: 10, toward: 4, count: 500) == 10...10)
        #expect(FileComments.extend(from: 10, toward: 400, count: 500) == 10...209)
        #expect(FileComments.extend(from: 10, toward: 40, count: 25) == 10...25)
    }

    @Test func commentsSitUnderTheirLastLineAndThosePastTheEndLeadTheFile() throws {
        let diff = try SessionComposerTests.comment(9)
        let notes = [try Self.note(1, lines: 2...4), try Self.note(2, lines: 4...4), try Self.note(3, lines: 30...31, stale: true),
                     try Self.note(4, path: "README.md", lines: 1...1), try Self.note(5, lines: 1...1), diff]
        let placed = FileComments.placement(notes, path: "Sources/main.swift", count: 20, except: 5)
        #expect(placed.mapValues { $0.map(\.id) } == [4: [1, 2], 0: [3]])
    }
}
