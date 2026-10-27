import AppKit
import Foundation
import Synchronization
import SwiftUI
import Testing
@testable import Kit

@MainActor struct SessionMentionTests {
    /// Mid-morning to mid-evening in every time zone, so 26 hours earlier is
    /// always yesterday.
    private static let now = Date(timeIntervalSince1970: 1_800_043_200)

    private func session(_ id: String, _ title: String, cwd: String, minutesAgo: Double) -> SessionExcerpt {
        let date = Self.now.addingTimeInterval(-minutesAgo * 60)
        return SessionExcerpt(id: id, title: title, sourceTitle: "Kit server", model: "p/m", thinking: "high",
                              workspace: URL(fileURLWithPath: cwd).lastPathComponent, cwd: cwd,
                              date: date.ISO8601Format(), lastActivity: date.ISO8601Format(), messages: [])
    }

    /// The server's sessions, the attached one among them, listed out of
    /// recency order.
    private var listed: [SessionExcerpt] {
        [session("attached", "This session", cwd: "/srv/kit", minutesAgo: 1),
         session("ses_flaky", "Untitled session", cwd: "/srv/api", minutesAgo: 60 * 26),
         session("ses_release", "Release 0.40 notes", cwd: "/srv/kit", minutesAgo: 60),
         session("ses_branch", "Prepare the release branch", cwd: "/srv/kit", minutesAgo: 5)]
    }

    private func enter() throws -> NSEvent {
        try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "\r", charactersIgnoringModifiers: "\r", isARepeat: false, keyCode: 36))
    }

    // MARK: Opening

    @Test func typedHashOpensASessionMentionAtABoundary() {
        for (previous, next, pasted, expected) in [
            ("", "#", false, true), ("see ", "see #", false, true), ("see\n", "see\n#", false, true),
            ("issue", "issue#", false, false), ("", "#", true, false), ("", "#rel", true, false)
        ] {
            let state = ComposerMentionState()
            #expect(state.observe(previous: previous, next: next, pasted: pasted) == expected)
            #expect(state.isOpen == expected)
            if expected { #expect(state.kind == .session) }
        }
        let file = ComposerMentionState()
        file.observe(previous: "", next: "@", pasted: false)
        #expect(file.kind == .file)
    }

    @Test func theQueryFollowsTypingAndEndsAtWhitespaceOrAnotherHash() {
        let state = ComposerMentionState()
        state.observe(previous: "see ", next: "see #", pasted: false)
        state.observe(previous: "see #", next: "see #rel", pasted: false)
        #expect(state.query == "rel")
        #expect(state.range == NSRange(location: 4, length: 4))
        // An @ is part of a session query; only # and whitespace end it.
        state.observe(previous: "see #rel", next: "see #rel@x", pasted: false)
        #expect(state.isOpen && state.query == "rel@x")
        state.observe(previous: "see #rel@x", next: "see #rel@x#", pasted: false)
        #expect(state.isOpen == false)
        state.observe(previous: "", next: "#", pasted: false)
        state.observe(previous: "#", next: "#a ", pasted: false)
        #expect(state.isOpen == false)
    }

    // MARK: Listing and ranking

    @Test func candidatesAreTheOtherSessionsMostRecentFirstWithWhatEachIsDoing() {
        let candidates = SessionMentions.candidates(listed, activity: ["ses_branch": .running, "ses_flaky": .awaitingResponse],
                                                    excluding: "attached")
        #expect(candidates.map(\.id) == ["ses_branch", "ses_release", "ses_flaky"])
        // Each row: title, folder, then what it is doing or its age.
        #expect(candidates.map { [$0.title, $0.folder, $0.trailing(now: Self.now)] } == [
            ["Prepare the release branch", "kit", "running"],
            ["Release 0.40 notes", "kit", "1 hr ago"],
            ["Untitled session", "api", "waiting"]
        ])
        let idle = SessionMentions.candidates(listed, activity: [:], excluding: "attached")
        #expect(idle.map { $0.trailing(now: Self.now) } == ["5 min ago", "1 hr ago", "yesterday"])
    }

    @Test func rankingWeighsTitlesThenMatchesIDsAndDirectoriesLikeTheTUI() {
        let candidates = SessionMentions.candidates(listed + [session("ses_reload", "Flaky reload test", cwd: "/srv/kit", minutesAgo: 30)],
                                                    activity: [:], excluding: "attached")
        #expect(SessionMentions.ranked(candidates, query: " ").map(\.id) == candidates.map(\.id))
        // A title prefix outranks a word inside a title; ties keep recency.
        #expect(SessionMentions.ranked(candidates, query: "REL").map(\.title) ==
            ["Release 0.40 notes", "Flaky reload test", "Prepare the release branch"])
        // An untitled session is found by its ID or its directory.
        #expect(SessionMentions.ranked(candidates, query: "ses_flaky").map(\.id) == ["ses_flaky"])
        #expect(SessionMentions.ranked(candidates, query: "/srv/api").map(\.id) == ["ses_flaky"])
        #expect(SessionMentions.ranked(candidates, query: "zzz").isEmpty)
    }

    // MARK: Loading states

    @Test func eachMentionListsSessionsAfreshAndNamesItsState() async throws {
        let state = ComposerMentionState()
        let loads = Mutex(0)
        let gate = AsyncStream<Void>.makeStream()
        state.observe(previous: "", next: "#", pasted: false)
        state.loadSessions {
            loads.withLock { $0 += 1 }
            for await _ in gate.stream { break }
            return []
        }
        #expect(state.loading)
        #expect(state.emptyText == "Loading sessions…")
        gate.continuation.yield()
        try await eventually { !state.loading }
        #expect(state.emptyText == "No other sessions")

        state.close()
        state.observe(previous: "", next: "#", pasted: false)
        let candidates = SessionMentions.candidates(listed, activity: [:], excluding: "attached")
        state.loadSessions { loads.withLock { $0 += 1 }; return candidates }
        try await eventually { !state.loading }
        #expect(loads.withLock { $0 } == 2)
        #expect(state.visibleMatches == ["ses_branch", "ses_release", "ses_flaky"])
        #expect(state.selected == "ses_branch")
        state.observe(previous: "#", next: "#zzz", pasted: false)
        #expect(state.emptyText == "No results")

        struct Refused: LocalizedError { var errorDescription: String? { "connection refused" } }
        state.close()
        state.observe(previous: "", next: "#", pasted: false)
        // A new mention starts without the previous list.
        #expect(state.sessions.isEmpty)
        state.loadSessions { throw Refused() }
        try await eventually { !state.loading }
        #expect(state.emptyText == "Couldn’t load sessions: connection refused")
    }

    @Test func aClosedMentionIgnoresALateSessionList() async throws {
        let state = ComposerMentionState()
        state.observe(previous: "", next: "#", pasted: false)
        let candidates = SessionMentions.candidates(listed, activity: [:], excluding: "attached")
        state.loadSessions { try? await Task.sleep(for: .milliseconds(30)); return candidates }
        state.close()
        try await Task.sleep(for: .milliseconds(60))
        #expect(state.sessions.isEmpty)
        #expect(state.matches.isEmpty)
    }

    // MARK: Insertion

    @Test func enterReplacesTheQueryWithASessionReferenceWithoutSending() async throws {
        let state = ComposerMentionState()
        state.observe(previous: "compare ", next: "compare #", pasted: false)
        let candidates = SessionMentions.candidates(listed, activity: [:], excluding: "attached")
        state.loadSessions { candidates }
        try await eventually { !state.loading }
        state.observe(previous: "compare #", next: "compare #flaky", pasted: false)
        #expect(state.selected == "ses_flaky")
        state.move(1)
        #expect(state.selected == "ses_flaky")

        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        editor.string = "compare #flaky"; editor.mentions = state
        editor.setSelectedRange(NSRange(location: 14, length: 0))
        editor.submit = { Issue.record("Choosing a session must not send the message") }
        editor.keyDown(with: try enter())
        #expect(editor.string == "compare #[session:ses_flaky] ")
        #expect(editor.selectedRange() == NSRange(location: 29, length: 0))
        #expect(state.isOpen == false)
    }

    @Test func theComposerDrawsEachTokenInAccentOnTheMentionTint() {
        let theme = MicaTheme(dark: true)
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 600, height: 100))
        editor.string = "see #[session:ses_a] and #[session:ses_b]"
        #expect(SessionMentions.matches(in: editor.string) == [
            .init(range: NSRange(location: 4, length: 16), id: "ses_a"),
            .init(range: NSRange(location: 25, length: 16), id: "ses_b")
        ])
        editor.highlightSessionMentions(theme: theme)
        let layout = try! #require(editor.layoutManager)
        func color(_ key: NSAttributedString.Key, at index: Int) -> NSColor? {
            layout.temporaryAttribute(key, atCharacterIndex: index, effectiveRange: nil) as? NSColor
        }
        for index in [4, 19, 25, 40] {
            #expect(color(.foregroundColor, at: index) == NSColor(theme.accent))
            #expect(color(.backgroundColor, at: index) == ComposerTextView.sessionMentionTint(theme))
        }
        for index in [0, 3, 20, 24] {
            #expect(color(.foregroundColor, at: index) == nil)
            #expect(color(.backgroundColor, at: index) == nil)
        }
        // Editing the token away removes its highlight.
        editor.string = "see #[session:ses_a"
        editor.highlightSessionMentions(theme: theme)
        #expect(color(.foregroundColor, at: 4) == nil)
    }

    // MARK: Transcript

    private func paragraph(_ source: String) throws -> AttributedString {
        guard case .paragraph(let text)? = MarkdownContent(source: source).blocks.first else {
            Issue.record("Expected a paragraph"); return AttributedString()
        }
        return text
    }

    private func links(_ text: AttributedString) -> [(String, URL)] {
        text.runs[\.link].compactMap { url, range in url.map { (String(text[range].characters), $0) } }
    }

    @Test func mentionsInProseBecomeSessionLinksButCodeKeepsItsText() throws {
        let text = try paragraph("Compare #[session:ses_release] with `#[session:ses_code]`, or [#[session:ses_label]](https://example.com).")
        #expect(String(text.characters) == "Compare #[session:ses_release] with #[session:ses_code], or #[session:ses_label].")
        let found = links(text)
        #expect(found.map(\.0) == ["#[session:ses_release]", "#[session:ses_label]"])
        #expect(found.map(\.1.absoluteString) == ["kit-session:ses_release", "https://example.com"])
        #expect(SessionMentions.session(in: found[0].1) == "ses_release")
        #expect(SessionMentions.session(in: found[1].1) == nil)
    }

    @Test func aTranscriptNamesKnownSessionsAndMutesUnknownOnes() throws {
        let theme = MicaTheme(dark: true)
        let link = TranscriptSessionLink(serverID: "local", title: { $0 == "ses_release" ? "Release 0.40 notes" : nil }) { _ in }
        let text = try paragraph("Compare #[session:ses_release] with what #[session:ses_gone] decided.")
        let shown = SessionMentions.presented(text, link: link, theme: theme)
        #expect(String(shown.characters) == "Compare #Release 0.40 notes with what #unknown session decided.")
        #expect(links(shown).map { "\($0.0) → \($0.1.absoluteString)" } == ["#Release 0.40 notes → kit-session:ses_release"])
        let runs = shown.runs.map { run -> (String, Color?, Color?) in
            let foreground: Color? = run.foregroundColor, background: Color? = run.backgroundColor
            return (String(shown[run.range].characters), foreground, background)
        }
        #expect(runs.contains { $0.0 == "#Release 0.40 notes" && $0.1 == theme.accent && $0.2 == theme.accent.opacity(0.14) })
        #expect(runs.contains { $0.0 == "#unknown session" && $0.1 == theme.muted && $0.2 == theme.raised })

        // Outside a transcript the token reads as written and links nowhere.
        let plain = SessionMentions.presented(text, link: nil, theme: theme)
        #expect(String(plain.characters) == "Compare #[session:ses_release] with what #[session:ses_gone] decided.")
        #expect(links(plain).isEmpty)
    }

    @Test func aTranscriptMentionLinkOpensItsSessionAndOtherLinksOpenAsBefore() {
        _ = NSApplication.shared
        let workspace = WorkspaceState()
        let opened = Mutex<[String]>([])
        let link = TranscriptSessionLink(serverID: "local") { id in opened.withLock { $0.append(id) } }
        let open = OpenURLAction.transcript(workspace, beside: .conversation, sessionLink: link)
        open(SessionMentions.link("ses_release")!)
        #expect(opened.withLock { $0 } == ["ses_release"])
        #expect(workspace.panes == [.conversation])

        open(FilePaths.link("/srv/notes.md")!)
        #expect(opened.withLock { $0 } == ["ses_release"])
        #expect(workspace.panes == [.conversation, .hostFile("/srv/notes.md")])
    }
}
