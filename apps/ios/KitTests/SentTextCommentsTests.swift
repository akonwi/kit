import KitMermaid
import KitTranscript
import Testing
@testable import Kit

/// A sent message's comments on selected text (ADR 0070).
struct SentTextCommentsTests {
    @Test func theDisclosureCountsTheComments() {
        let one = TextAnnotationText.Sent(number: 1, comment: "Back off.", source: "Assistant reply",
                                          quote: "every 2 seconds", cut: false)
        #expect(SentTextComments.title([one]) == "1 comment")
        #expect(SentTextComments.title([one, one]) == "2 comments")
    }

    @Test func commentsAreSplitFromAMessageBeforeItsDiagramComments() {
        let message = TextAnnotationText.join("Fix it.", TextAnnotationText.text([
            TextAnnotation(comment: "Back off.", source: "Assistant reply", quote: "every 2 seconds", nonce: "n1"),
        ]))
        let commented = TextAnnotationText.split(message)
        #expect(DiagramAnnotationText.split(commented.text).text == "Fix it.")
        #expect(commented.annotations.map(\.comment) == ["Back off."])
    }
}
