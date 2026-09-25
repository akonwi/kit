import AppKit
import SwiftUI
import Testing
@testable import Kit

struct SessionLauncherTests {
    private func session(_ id: String, title: String, path: String, date: String) -> SessionExcerpt {
        SessionExcerpt(id: id, title: title, sourceTitle: "Kit server", model: "model", thinking: "high",
                       workspace: "kit", cwd: path, date: date, messages: [])
    }

    @Test func recentSessionsFilterByNameAndDirectory() {
        let sessions = [
            session("a", title: "Window restoration", path: "/work/kit", date: "2026-09-10T12:00:00Z"),
            session("b", title: "Markdown rendering", path: "/work/mica", date: "2026-09-11T12:00:00.000Z")
        ]
        var launcher = SessionLauncher()
        #expect(launcher.results(in: sessions).map(\.id) == ["b", "a"])
        #expect(launcher.highlightedID == nil)
        launcher.query = "WINDOW kit"
        #expect(launcher.results(in: sessions).map(\.id) == ["a"])
        launcher.query = "absent"
        #expect(launcher.results(in: sessions).count == 0)
        launcher.query = ""
        let results = launcher.results(in: sessions)
        launcher.move(1, in: results)
        #expect(launcher.highlightedID == "b")
        launcher.move(1, in: results)
        #expect(launcher.highlightedID == "a")
        launcher.move(-1, in: results)
        #expect(launcher.highlightedID == "b")
    }

    @MainActor @Test func renderLauncherForReview() async throws {
        let sessions = [
            session("a", title: "Native window restoration", path: "/Users/akonwi/Developer/agent/kit-macos", date: "2026-09-13T13:10:00Z"),
            session("b", title: "Refine the markdown renderer", path: "/Users/akonwi/Developer/agent/kit-v2", date: "2026-09-12T18:00:00Z"),
            session("c", title: "Add keyboard navigation to the interaction dock", path: "/Users/akonwi/Developer/agent/kit-macos", date: "2026-09-11T10:00:00Z")
        ]
        for dark in [false, true] {
            var opened: [String] = []
            var renamed: [String] = []
            var deleted: [String] = []
            #expect(try #require(SessionLauncherContent.logo(dark: dark)).size == NSSize(width: 1024, height: 768))
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let view = SessionLauncherContent(sessions: sessions, connecting: false, connected: true, error: nil,
                                               open: { opened.append($0.id) }, refresh: {},
                                               rename: { renamed.append($0.id) }, delete: { deleted.append($0.id) })
                .environment(\.mica, theme).environment(\.colorScheme, dark ? .dark : .light)
                .foregroundStyle(theme.text).background(theme.surface).frame(width: 900, height: 700)
            let host = NSHostingView(rootView: view)
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 700),
                                  styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = host
            window.makeKeyAndOrderFront(nil)
            defer { window.close() }
            for _ in 0..<100 {
                if let editor = window.firstResponder as? NSTextView, editor.isFieldEditor { break }
                try await Task.sleep(for: .milliseconds(20))
            }
            #expect((window.firstResponder as? NSTextView)?.isFieldEditor == true)
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            let data = try #require(bitmap.representation(using: .png, properties: [:]))
            try data.write(to: URL(fileURLWithPath: "/tmp/kit-launcher-\(dark ? "dark" : "light").png"))
            #expect(host.bounds.size == NSSize(width: 900, height: 700))
            #expect(opened == [])
            for (key, text) in [(UInt16(125), "\u{f701}"), (UInt16(125), "\u{f701}"), (UInt16(36), "\r")] {
                let event = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [],
                    timestamp: 0, windowNumber: window.windowNumber, context: nil,
                    characters: text, charactersIgnoringModifiers: text, isARepeat: false, keyCode: key))
                window.sendEvent(event)
                try await Task.sleep(for: .milliseconds(30))
            }
            #expect(opened == ["c"])
            for (key, text) in [(UInt16(15), "r"), (UInt16(51), "\u{7f}")] {
                let event = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [.command],
                    timestamp: 0, windowNumber: window.windowNumber, context: nil,
                    characters: text, charactersIgnoringModifiers: text, isARepeat: false, keyCode: key))
                #expect(window.performKeyEquivalent(with: event))
                try await Task.sleep(for: .milliseconds(30))
            }
            #expect(renamed == ["c"])
            #expect(deleted == ["c"])
        }
    }
}
