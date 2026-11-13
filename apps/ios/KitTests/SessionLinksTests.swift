import Foundation
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

@MainActor struct SessionLinksTests {
    static let message = SessionMentions.linked(AttributedString("Ask #[session:s2] and #[session:gone] first."))

    static let links = SessionLinks(current: "s1", title: { ["s1": "This session", "s2": "Fix SSE reconnect"][$0] }, open: { _ in })

    /// The runs of `text` that are links, with what they read.
    static func linked(_ text: AttributedString) -> [(String, URL)] {
        text.runs.compactMap { run in run.link.map { (String(text[run.range].characters), $0) } }
    }

    @Test func aListedSessionReadsAsItsTitleAndLinksToIt() {
        let shown = SessionMentions.presented(Self.message, links: Self.links)
        #expect(String(shown.characters) == "Ask #Fix SSE reconnect and #unknown session first.")
        let links = Self.linked(shown)
        #expect(links.map(\.0) == ["#Fix SSE reconnect"])
        #expect(links.first.flatMap { SessionMentions.session(in: $0.1) } == "s2")
    }

    @Test func anUnlistedSessionReadsAsUnknownAndOpensNothing() {
        let shown = SessionMentions.presented(Self.message, links: Self.links)
        let unknown = shown.range(of: "#unknown session")!
        #expect(shown[unknown].runs.allSatisfy { $0.link == nil })
        #expect(shown[unknown].foregroundColor == Theme.muted)
    }

    @Test func outsideATranscriptAMentionStaysAsWritten() {
        let shown = SessionMentions.presented(Self.message, links: nil)
        #expect(String(shown.characters) == "Ask #[session:s2] and #[session:gone] first.")
        #expect(Self.linked(shown).isEmpty)
    }

    @Test func aMentionInCodeStaysAsWritten() {
        var code = AttributedString("#[session:s2]")
        code.inlinePresentationIntent = .code
        let shown = SessionMentions.presented(SessionMentions.linked(code), links: Self.links)
        #expect(String(shown.characters) == "#[session:s2]")
        #expect(Self.linked(shown).isEmpty)
    }

    @Test func theTranscriptsOwnSessionReadsAsItsTitleWithoutALink() {
        let shown = SessionMentions.presented(SessionMentions.linked(AttributedString("See #[session:s1].")), links: Self.links)
        #expect(String(shown.characters) == "See #This session.")
        #expect(Self.linked(shown).isEmpty)
    }

    @Test func aUserMessageLinksMentionsOutsideItsCode() {
        let text = "Ask #[session:s2], not `#[session:s2]`, nor\n```\n#[session:s2]\n```"
        let links = Self.linked(SessionMentions.linked(plain: text))
        #expect(links.map(\.0) == ["#[session:s2]"])
        let shown = SessionMentions.presented(SessionMentions.linked(plain: text), links: Self.links)
        #expect(String(shown.characters) == "Ask #Fix SSE reconnect, not `#[session:s2]`, nor\n```\n#[session:s2]\n```")
    }
}
