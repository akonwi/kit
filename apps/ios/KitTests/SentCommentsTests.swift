import Testing
@testable import Kit

/// The lines a sent comment's card quotes.
struct SentCommentsTests {
    @Test func quotesTheFirstThreeLinesLessTheirSharedIndent() {
        let source = "        if let id = cursor.last {\n            request.lastEventID = id\n        }\n        return request"
        #expect(SentCommentCard.lines(source) == [
            "if let id = cursor.last {",
            "    request.lastEventID = id",
            "}",
        ])
    }

    @Test func blankLinesDontSetTheIndentAndTabsAreFourSpaces() {
        #expect(SentCommentCard.lines("\tlet a = 1\n\n\t\tlet b = 2") == ["let a = 1", "", "    let b = 2"])
    }

    @Test func anEmptySourceQuotesNothing() {
        #expect(SentCommentCard.lines("").isEmpty)
    }
}
