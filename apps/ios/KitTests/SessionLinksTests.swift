import Foundation
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

@MainActor struct SessionLinksTests {
    static let message = SessionMentions.linked(AttributedString("Ask #[session:s2] and #[session:gone] first."))

    static let links = SessionLinks(title: { $0 == "s2" ? "Fix SSE reconnect" : nil }, open: { _ in })

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
}
