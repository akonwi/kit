import AppKit
import SwiftUI
import Testing
@testable import Kit

struct SessionPickerTests {
    private static func session(_ id: String, _ title: String, parent: String? = nil, folder: String,
                                at time: String) -> SessionExcerpt {
        SessionExcerpt(id: id, title: title, parentSessionID: parent, sourceTitle: "Kit server", model: "model",
                       thinking: "high", workspace: folder, cwd: "/work/\(folder)", date: "2026-09-10T\(time):00Z",
                       messages: [])
    }

    /// A family with a grandchild, a lone session, and an orphan whose
    /// parent isn't listed. Listed out of order; the latest activity is a
    /// child's.
    private static let sessions = [
        session("r", "Release notes", folder: "kit", at: "11:30"),
        session("c2", "Explore ghostty resize", parent: "p", folder: "ghostty", at: "11:00"),
        session("p", "Tiling the native shell", folder: "kit", at: "12:00"),
        session("o", "Parser fuzzing", parent: "deleted", folder: "ard", at: "09:00"),
        session("g", "Measure tile gaps", parent: "c1", folder: "kit", at: "10:00"),
        session("c1", "Review the tile layout", parent: "p", folder: "kit", at: "12:05"),
    ]

    /// Each row as the list shows it: indentation, title, disclosure, and
    /// status markers (⋯ working, ? awaiting a response), with `(context)`
    /// for an ancestor shown only for a search match.
    private static func lines(_ rows: [SessionPicker.Row]) -> [String] {
        rows.map { row in
            var line = String(repeating: "  ", count: row.depth) + row.session.title
            if let children = row.children, children > 0 { line += " \(row.collapsed ? "▸" : "▾") \(children)" }
            if let hidden = row.hidden { line += " hides \(marker(hidden))" }
            if row.status != .idle { line += " \(marker(row.status))" }
            if row.context { line += " (context)" }
            return line
        }
    }

    private static func marker(_ status: SessionTabStatus) -> String {
        switch status {
        case .running: "⋯"
        case .awaitingResponse: "?"
        case .idle: ""
        }
    }

    @Test func childrenFollowTheirParentAndFamiliesKeepRecencyOrder() {
        let rows = SessionPicker().rows(Self.sessions, activity: ["c1": .running, "r": .awaitingResponse])
        #expect(Self.lines(rows) == [
            "Tiling the native shell ▾ 2",
            "  Review the tile layout ▾ 1 ⋯",
            "    Measure tile gaps",
            "  Explore ghostty resize",
            "Release notes ?",
            "Parser fuzzing",
        ])
    }

    @Test func equalSessionIDsOnTwoServersStayInTheirOwnFamilies() {
        let sessions = [
            Self.session("a", "Local parent", folder: "kit", at: "10:00"),
            Self.session("devbox/a", "Devbox parent", folder: "kit", at: "09:00"),
            Self.session("devbox/b", "Devbox child", parent: "devbox/a", folder: "kit", at: "11:00"),
        ]
        #expect(Self.lines(SessionPicker().rows(sessions)) == [
            "Devbox parent ▾ 1",
            "  Devbox child",
            "Local parent",
        ])
    }

    @Test func aParentCycleListsEachSessionOnce() {
        let sessions = [
            Self.session("a", "First", parent: "b", folder: "kit", at: "10:00"),
            Self.session("b", "Second", parent: "a", folder: "kit", at: "09:00"),
        ]
        #expect(Self.lines(SessionPicker().rows(sessions)) == ["Second ▾ 1", "  First"])
        var picker = SessionPicker(query: "first")
        #expect(Self.lines(picker.rows(sessions)) == ["Second (context)", "  First"])
        picker.query = "second"
        #expect(Self.lines(picker.rows(sessions)) == ["Second"])
    }

    @Test func arrowsCollapseAndExpandTheHighlightedFamily() {
        var picker = SessionPicker(highlightedID: "p")
        let activity: [String: SessionTabStatus] = ["c1": .running, "g": .awaitingResponse]
        let handled1 = picker.disclose(expand: false, in: picker.rows(Self.sessions, activity: activity))
        #expect(handled1)
        // The collapsed family shows the most urgent status it hides.
        #expect(Self.lines(picker.rows(Self.sessions, activity: activity)) == [
            "Tiling the native shell ▸ 2 hides ?",
            "Release notes",
            "Parser fuzzing",
        ])
        let handled2 = picker.disclose(expand: true, in: picker.rows(Self.sessions, activity: activity))
        #expect(handled2)
        #expect(Self.lines(picker.rows(Self.sessions, activity: activity)) == [
            "Tiling the native shell ▾ 2",
            "  Review the tile layout ▾ 1 ⋯",
            "    Measure tile gaps ?",
            "  Explore ghostty resize",
            "Release notes",
            "Parser fuzzing",
        ])

        // A row without children has nothing to collapse.
        picker.highlightedID = "c2"
        let handled3 = picker.disclose(expand: false, in: picker.rows(Self.sessions))
        #expect(handled3)
        #expect(picker.collapsed.isEmpty)
        #expect(picker.highlightedID == "c2")
    }

    @Test func collapsingAnAncestorMovesTheHighlightToIt() {
        var picker = SessionPicker(highlightedID: "g")
        picker.toggle("c1")
        var rows = picker.rows(Self.sessions)
        picker.reveal(in: rows, sessions: Self.sessions)
        #expect(picker.highlightedID == "c1")
        #expect(Self.lines(rows) == [
            "Tiling the native shell ▾ 2",
            "  Review the tile layout ▸ 1",
            "  Explore ghostty resize",
            "Release notes",
            "Parser fuzzing",
        ])
        picker.toggle("c1")
        rows = picker.rows(Self.sessions)
        picker.reveal(in: rows, sessions: Self.sessions)
        #expect(picker.highlightedID == "c1")
    }

    @Test func searchShowsMatchesUnderTheirAncestorsAndIgnoresCollapsedFamilies() {
        var picker = SessionPicker(highlightedID: "p", collapsed: ["p"])
        picker.query = "tile"
        var rows = picker.rows(Self.sessions, activity: ["c1": .running])
        picker.queryChanged(rows: rows, sessions: Self.sessions)
        #expect(Self.lines(rows) == [
            "Tiling the native shell (context)",
            "  Review the tile layout ⋯",
            "    Measure tile gaps",
        ])
        // The first match takes the highlight, not the ancestor above it.
        #expect(picker.highlightedID == "c1")
        // While searching, ←/→ belong to the search field.
        let handled4 = picker.disclose(expand: true, in: rows)
        #expect(!handled4)
        #expect(picker.collapsed == ["p"])

        picker.query = "ghostty"
        rows = picker.rows(Self.sessions)
        picker.queryChanged(rows: rows, sessions: Self.sessions)
        #expect(Self.lines(rows) == ["Tiling the native shell (context)", "  Explore ghostty resize"])
        #expect(picker.highlightedID == "c2")

        // Clearing the search returns to the highlighted row or, when its
        // family is collapsed, the nearest shown ancestor.
        picker.query = ""
        rows = picker.rows(Self.sessions)
        picker.queryChanged(rows: rows, sessions: Self.sessions)
        #expect(Self.lines(rows) == ["Tiling the native shell ▸ 2", "Release notes", "Parser fuzzing"])
        #expect(picker.highlightedID == "p")
    }

    @Test func searchMatchesFoldersAndOrphans() {
        var picker = SessionPicker(query: "ard")
        #expect(Self.lines(picker.rows(Self.sessions)) == ["Parser fuzzing"])
        picker.query = "absent"
        #expect(picker.rows(Self.sessions).isEmpty)
    }

    @MainActor @Test func withoutTheCurrentSessionTheFirstRowIsHighlighted() async throws {
        var opened: [String] = []
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let view = SessionPickerContent(sessions: Self.sessions, activity: [:], connecting: false, connected: true,
                                        error: nil, currentID: "missing", open: { opened.append($0.id) }, refresh: {})
            .environment(\.mica, theme).frame(width: PaletteMetrics.width, height: PaletteMetrics.height)
        let window = FocusIsolatedWindow(contentRect: NSRect(x: 0, y: 0, width: PaletteMetrics.width, height: PaletteMetrics.height),
                                         styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: view)
        window.orderFront(nil)
        defer { window.close() }
        try await eventually { (window.firstResponder as? NSTextView)?.isFieldEditor == true }
        // ↓ moves from the first row to the second.
        for (key, text) in [(UInt16(125), "\u{f701}"), (UInt16(36), "\r")] {
            window.sendEvent(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
                                              windowNumber: window.windowNumber, context: nil, characters: text,
                                              charactersIgnoringModifiers: text, isARepeat: false, keyCode: key)!)
        }
        try await eventually { !opened.isEmpty }
        #expect(opened == ["c1"])
    }

    @MainActor @Test func keyboardOpensCollapsesAndSearches() async throws {
        let sessions = Self.sessions + [Self.session("abc", "abc", folder: "kit", at: "08:00"),
                                        Self.session("acb", "acb", folder: "kit", at: "07:00")]
        for dark in [false, true] {
            var opened: [String] = []
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let view = SessionPickerContent(sessions: sessions, activity: ["c1": .running, "r": .awaitingResponse],
                                            connecting: false, connected: true, error: nil, currentID: "c2",
                                            open: { opened.append($0.id) }, refresh: {})
                .environment(\.mica, theme).environment(\.colorScheme, dark ? .dark : .light)
                .foregroundStyle(theme.text).background(theme.surface)
                .frame(width: PaletteMetrics.width, height: PaletteMetrics.height)
            let host = NSHostingView(rootView: view)
            let window = FocusIsolatedWindow(contentRect: NSRect(x: 0, y: 0, width: PaletteMetrics.width, height: PaletteMetrics.height),
                                             styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            try await eventually { (window.firstResponder as? NSTextView)?.isFieldEditor == true }
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            let data = try #require(bitmap.representation(using: .png, properties: [:]))
            try data.write(to: URL(fileURLWithPath: "/tmp/kit-session-picker-\(dark ? "dark" : "light").png"))

            func press(_ key: UInt16, _ text: String) {
                let event = NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
                                             windowNumber: window.windowNumber, context: nil, characters: text,
                                             charactersIgnoringModifiers: text, isARepeat: false, keyCode: key)!
                window.sendEvent(event)
            }
            let up: (UInt16, String) = (126, "\u{f700}"), down: (UInt16, String) = (125, "\u{f701}")
            let left: (UInt16, String) = (123, "\u{f702}"), enter: (UInt16, String) = (36, "\r")

            // The highlight starts on the current session.
            press(enter.0, enter.1)
            try await eventually { opened.count == 1 }
            #expect(opened == ["c2"])

            // ← on a row without children changes nothing; on its parent it
            // collapses the family, so ↓ reaches the next family.
            press(left.0, left.1)
            for _ in 0..<3 { press(up.0, up.1) }
            press(left.0, left.1)
            press(down.0, down.1)
            press(enter.0, enter.1)
            try await eventually { opened.count == 2 }
            #expect(opened.last == "r")

            // While searching, ← moves the cursor instead: "ac", ←, "b" reads "abc".
            for (key, text) in [(UInt16(0), "a"), (UInt16(8), "c")] { press(key, text) }
            press(left.0, left.1)
            press(11, "b")
            press(enter.0, enter.1)
            try await eventually { opened.count == 3 }
            #expect(opened.last == "abc")
        }
    }
}
