import KitClient
import Testing
@testable import Kit

struct TranscriptTailTests {
    private static func message(_ id: String, _ role: String, _ text: String = "Text") -> TranscriptMessage {
        TranscriptMessage(id: id, role: role, text: text, tools: [])
    }

    private static let asked = [message("u1", "user", "Why does it drop?")]

    private static func arrived(_ old: [TranscriptMessage], _ new: [TranscriptMessage],
                                active: (Bool, Bool) = (true, false)) -> String? {
        TranscriptTail.arrivedReply(after: TranscriptTail.of(old, active: active.0),
                                    in: TranscriptTail.of(new, active: active.1))
    }

    @Test func aReplyARunAddsShowsFromItsTop() {
        let next = Self.asked + [Self.message("tools-1", "tools", ""), Self.message("a1", "assistant")]
        #expect(Self.arrived(Self.asked, next) == "a1")
        #expect(Self.arrived(Self.asked, next, active: (false, true)) == "a1")
    }

    @Test func outputThatIsNotAReplyFollowsTheEnd() {
        #expect(Self.arrived(Self.asked, Self.asked + [Self.message("tools-1", "tools", "")]) == nil)
        #expect(Self.arrived(Self.asked, Self.asked + [Self.message("u2", "user")]) == nil)
        #expect(Self.arrived(Self.asked, Self.asked + [Self.message("a1", "assistant", "")]) == nil)
    }

    @Test func aReplyThatGetsItsTextShowsFromItsTop() {
        let pending = Self.asked + [Self.message("a1", "assistant", "")]
        #expect(Self.arrived(pending, Self.asked + [Self.message("a1", "assistant")]) == "a1")
    }

    @Test func theSameReplyGrowingIsNotANewArrival() {
        let shown = Self.asked + [Self.message("a1", "assistant", "First")]
        #expect(Self.arrived(shown, Self.asked + [Self.message("a1", "assistant", "First and more")]) == nil)
    }

    @Test func aSentMessageSavedAsTheReplyArrivesStillShowsItFromItsTop() {
        let sent = [Self.message("a0", "assistant"), Self.message("live-user-turn_1", "user")]
        let saved = [Self.message("a0", "assistant"), Self.message("u1", "user"), Self.message("a1", "assistant")]
        #expect(Self.arrived(sent, saved) == "a1")
    }

    @Test func earlierPagesAreNotAnArrival() {
        let shown = Self.asked + [Self.message("a1", "assistant")]
        #expect(Self.arrived(shown, [Self.message("a0", "assistant")] + shown) == nil)
    }

    @Test func aTranscriptThatLoadsOrChangesWithoutARunIsNotAnArrival() {
        let reply = Self.asked + [Self.message("a1", "assistant")]
        #expect(Self.arrived([], reply) == nil)
        #expect(Self.arrived([Self.message("a0", "assistant")], reply, active: (false, false)) == nil)
    }
}
