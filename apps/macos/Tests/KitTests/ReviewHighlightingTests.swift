import Foundation
import Testing
@testable import Kit

struct ReviewHighlightingTests {
    @Test func multilineCapturesUseFullDocumentsAndDiffCoordinates() async throws {
        let before = "/*\nold 🌿\n*/\nlet value = 1\n"
        let after = "/*\nnew 🌿\n*/\nlet value = 2\n"
        let patch = "@@ -2 +2 @@\n-old 🌿\n+new 🌿\n@@ -4 +4 @@\n-let value = 1\n+let value = 2"
        let old = try await CodeSyntaxHighlighter.shared.spans(for: .init(source: before, language: "swift"))
        let new = try await CodeSyntaxHighlighter.shared.spans(for: .init(source: after, language: "swift"))
        let rows = ReviewHighlighting.rows(patch: patch, before: before, after: after, beforeSpans: old, afterSpans: new)
        #expect(rows[1] == [SyntaxSpan(range: NSRange(location: 1, length: 6), role: "comment")])
        #expect(rows[2] == [SyntaxSpan(range: NSRange(location: 1, length: 6), role: "comment")])
        #expect(rows[4].contains { $0.role == "number" && $0.range == NSRange(location: 13, length: 1) })
        #expect(rows[5].contains { $0.role == "number" && $0.range == NSRange(location: 13, length: 1) })
    }
}
