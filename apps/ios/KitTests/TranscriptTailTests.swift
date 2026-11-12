import KitClient
import Testing
@testable import Kit

struct TranscriptTailTests {
    private static func message(_ id: String, _ role: String, _ text: String = "Text") -> TranscriptMessage {
        TranscriptMessage(id: id, role: role, text: text, tools: [])
    }

    private static let asked = [message("u1", "user", "Why does it drop?")]

    @Test func aReplyAddedAfterTheLastMessageShowsFromItsTop() {
        let old = TranscriptTail.of(Self.asked)
        let next = Self.asked + [Self.message("tools-1", "tools", ""), Self.message("a1", "assistant")]
        #expect(TranscriptTail.arrivedReply(after: old, in: next) == "a1")
    }

    @Test func outputThatIsNotAReplyFollowsTheEnd() {
        let old = TranscriptTail.of(Self.asked)
        #expect(TranscriptTail.arrivedReply(after: old, in: Self.asked + [Self.message("tools-1", "tools", "")]) == nil)
        #expect(TranscriptTail.arrivedReply(after: old, in: Self.asked + [Self.message("u2", "user")]) == nil)
        #expect(TranscriptTail.arrivedReply(after: old, in: Self.asked + [Self.message("a1", "assistant", "")]) == nil)
    }

    @Test func aReplyThatGetsItsTextShowsFromItsTop() {
        let pending = Self.asked + [Self.message("a1", "assistant", "")]
        let done = Self.asked + [Self.message("a1", "assistant")]
        #expect(TranscriptTail.arrivedReply(after: TranscriptTail.of(pending), in: done) == "a1")
    }

    @Test func theSameReplyGrowingIsNotANewArrival() {
        let shown = Self.asked + [Self.message("a1", "assistant", "First")]
        let next = Self.asked + [Self.message("a1", "assistant", "First and more")]
        #expect(TranscriptTail.arrivedReply(after: TranscriptTail.of(shown), in: next) == nil)
    }

    @Test func theFirstReplyInAnEmptySessionShowsFromItsTop() {
        let next = Self.asked + [Self.message("a1", "assistant")]
        #expect(TranscriptTail.arrivedReply(after: TranscriptTail.of([]), in: next) == "a1")
    }

    @Test func aSentMessageSavedAsTheReplyArrivesStillShowsItFromItsTop() {
        let sent = [Self.message("a0", "assistant"), Self.message("live-user-turn_1", "user")]
        let saved = [Self.message("a0", "assistant"), Self.message("u1", "user"), Self.message("a1", "assistant")]
        #expect(TranscriptTail.arrivedReply(after: TranscriptTail.of(sent), in: saved) == "a1")
    }

    @Test func earlierPagesAreNotAnArrival() {
        let shown = Self.asked + [Self.message("a1", "assistant")]
        let earlier = [Self.message("a0", "assistant")] + shown
        #expect(TranscriptTail.arrivedReply(after: TranscriptTail.of(shown), in: earlier) == nil)
    }
}
