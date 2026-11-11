import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The menu a right-click on a link in rendered Markdown shows: a web link
/// opens in Kit's browser as a click does, or in the default browser, or
/// copies; other links and plain text keep AppKit's text menu.
@MainActor struct TextLinkMenuTests {
    static let source = "Read [the docs](https://example.com/guide) or [the notes](/srv/notes.md) before you start, then https://example.com/next."

    /// Rendered Markdown whose web links open in `workspace` beside the
    /// conversation, as the transcript's do.
    @MainActor struct Menus {
        let rendered: TranscriptHoverTests.Rendered
        let workspace = WorkspaceState()
        let engines = FakeBrowserEngines()
        let pasteboard = NSPasteboard(name: NSPasteboard.Name("TextLinkMenuTests-" + UUID().uuidString))
        let defaultBrowser = DefaultBrowser()

        @MainActor final class DefaultBrowser {
            var opened: [URL] = []
        }

        init() {
            _ = NSApplication.shared
            rendered = TranscriptHoverTests.Rendered(TextLinkMenuTests.source)
            workspace.makeBrowserEngine = engines.make
            let open = OpenURLAction.transcript(workspace, beside: .conversation, sessionLink: nil)
            rendered.hover.linkActions = .init(open: { open($0) },
                                               openInDefaultBrowser: { [defaultBrowser] in defaultBrowser.opened.append($0) },
                                               pasteboard: pasteboard)
        }

        func close() {
            pasteboard.releaseGlobally()
            rendered.close()
        }

        /// A click of `type` at the middle of `text`'s first line.
        func click(_ type: NSEvent.EventType, on text: String, modifiers: NSEvent.ModifierFlags = []) throws -> NSEvent {
            let rect = try #require(try rendered.editorRects(rendered.range(of: text)).first)
            return try #require(NSEvent.mouseEvent(with: type, location: NSPoint(x: rect.midX, y: rect.midY), modifierFlags: modifiers,
                                                   timestamp: ProcessInfo.processInfo.systemUptime,
                                                   windowNumber: rendered.window.windowNumber, context: nil,
                                                   eventNumber: 0, clickCount: 1, pressure: 1))
        }

        func titles(_ menu: NSMenu?) -> [String] {
            menu?.items.map { $0.isSeparatorItem ? "—" : $0.title } ?? []
        }
    }

    @Test func aWebLinkOffersToOpenItInKitOrTheDefaultBrowserOrToCopyIt() throws {
        let menus = Menus()
        defer { menus.close() }
        let right = try menus.click(.rightMouseDown, on: "the docs")
        #expect(menus.titles(menus.rendered.hover.linkMenu(for: right)) == ["Open Link", "Open in Default Browser", "Copy Link"])
        let control = try menus.click(.leftMouseDown, on: "the docs", modifiers: .control)
        #expect(menus.titles(menus.rendered.hover.linkMenu(for: control)) == ["Open Link", "Open in Default Browser", "Copy Link"])
        // The transcript's own menus give way to it.
        #expect(TextLinkHover.showsLinkMenu(for: right))
        // A bare address is a link too.
        let bare = try menus.click(.rightMouseDown, on: "https://example.com/next")
        let menu = try #require(menus.rendered.hover.linkMenu(for: bare))
        #expect(menus.titles(menu) == ["Open Link", "Open in Default Browser", "Copy Link"])
        menu.performActionForItem(at: 0)
        #expect(menus.workspace.panes.last.flatMap(menus.workspace.browser)?.url == URL(string: "https://example.com/next"))
    }

    @Test func aFileLinkPlainTextAndAPlainClickKeepAppKitsMenu() throws {
        let menus = Menus()
        defer { menus.close() }
        let file = try menus.click(.rightMouseDown, on: "the notes")
        #expect(menus.rendered.hover.linkMenu(for: file) == nil)
        #expect(!TextLinkHover.showsLinkMenu(for: file))
        let plain = try menus.click(.rightMouseDown, on: "before you start")
        #expect(menus.rendered.hover.linkMenu(for: plain) == nil)
        #expect(!TextLinkHover.showsLinkMenu(for: plain))
        let click = try menus.click(.leftMouseDown, on: "the docs")
        #expect(menus.rendered.hover.linkMenu(for: click) == nil)
    }

    @Test func openLinkOpensABrowserTabAsAClickDoes() throws {
        let menus = Menus()
        defer { menus.close() }
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the docs")))
        menu.performActionForItem(at: 0)
        let pane = try #require(menus.workspace.panes.last)
        #expect(menus.workspace.panes == [.conversation, pane])
        #expect(menus.workspace.browser(pane)?.url == URL(string: "https://example.com/guide"))
        #expect(menus.workspace.selected == pane)
        #expect(menus.defaultBrowser.opened == [])
    }

    @Test func openInDefaultBrowserHandsTheLinkToTheSystem() throws {
        let menus = Menus()
        defer { menus.close() }
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the docs")))
        menu.performActionForItem(at: 1)
        #expect(menus.defaultBrowser.opened == [URL(string: "https://example.com/guide")!])
        #expect(menus.workspace.panes == [.conversation])
    }

    @Test func copyLinkCopiesTheAddress() throws {
        let menus = Menus()
        defer { menus.close() }
        menus.pasteboard.clearContents()
        menus.pasteboard.setString("before", forType: .string)
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the docs")))
        menu.performActionForItem(at: 2)
        #expect(menus.pasteboard.string(forType: .string) == "https://example.com/guide")
        #expect((menus.pasteboard.readObjects(forClasses: [NSURL.self]) as? [URL]) == [URL(string: "https://example.com/guide")!])
        #expect(menus.workspace.panes == [.conversation])
        #expect(menus.defaultBrowser.opened == [])
    }

    @Test func aSelectionKeepsAppKitsTextItemsAfterTheLinksOwn() throws {
        let menus = Menus()
        defer { menus.close() }
        let event = try menus.click(.rightMouseDown, on: "the docs")
        let field = try #require(menus.rendered.field)
        // Selecting in the field, as a drag does, edits it in the window's
        // field editor.
        let window = menus.rendered.window
        let editor = try #require(window.fieldEditor(true, for: field) as? NSTextView)
        let selection = try menus.rendered.range(of: "Read the docs")
        field.cell?.select(withFrame: field.bounds, in: field, editor: editor, delegate: field,
                           start: selection.location, length: selection.length)
        #expect(window.makeFirstResponder(editor))
        #expect(editor.selectedRange() == selection)
        defer { field.cell?.endEditing(editor) }
        let menu = try #require(menus.rendered.hover.linkMenu(for: event))
        let titles = menus.titles(menu)
        #expect(Array(titles.prefix(4)) == ["Open Link", "Open in Default Browser", "Copy Link", "—"])
        // The selection's Copy acts on the selection, through the field's editor.
        let copy = try #require(menu.items.first { $0.title == "Copy" })
        #expect(copy.action == #selector(NSText.copy(_:)))
        #expect(copy.target == nil)
        // AppKit's own items for the link, whose Open Link opens the default
        // browser, give way to the link's.
        #expect(titles.filter { ["Open Link", "Open With", "Copy Link"].contains($0) } == ["Open Link", "Copy Link"])
        #expect(titles.last != "—")
        #expect(!zip(titles, titles.dropFirst()).contains { $0 == "—" && $1 == "—" })

        // A caret alone selects nothing.
        editor.setSelectedRange(NSRange(location: selection.location, length: 0))
        #expect(menus.titles(menus.rendered.hover.linkMenu(for: event)) == ["Open Link", "Open in Default Browser", "Copy Link"])
    }

    @Test func aMarkdownPreviewsOpenLinkUsesItsLinkHandler() throws {
        _ = NSApplication.shared
        var opened: [URL] = []
        let host = NSHostingView(rootView: MarkdownView(source: Self.source)
            .frame(width: TranscriptHoverTests.width, alignment: .topLeading)
            .background(TextLinkHoverRegion(color: .accentColor))
            .environment(\.openURL, OpenURLAction { opened.append($0); return .handled }))
        let size = host.fittingSize
        host.sizingOptions = []
        let window = NSWindow(testContentRect: NSRect(origin: .zero, size: size))
        window.contentView = host
        host.frame = NSRect(origin: .zero, size: size)
        window.makeKeyAndOrderFront(nil)
        host.layoutSubtreeIfNeeded()
        defer { window.close() }
        let field = try #require(Self.field(in: host))
        let range = (field.attributedStringValue.string as NSString).range(of: "the docs")
        let point = try #require(Self.point(of: range, in: field))
        let event = try #require(NSEvent.mouseEvent(with: .rightMouseDown, location: point, modifierFlags: [],
                                                    timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber,
                                                    context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
        let menu = try #require(TextLinkHover.anyLinkMenu(for: event))
        #expect(menu.items.map(\.title) == ["Open Link", "Open in Default Browser", "Copy Link"])
        menu.performActionForItem(at: 0)
        #expect(opened == [URL(string: "https://example.com/guide")!])
    }

    private static func field(in view: NSView) -> NSTextField? {
        if let field = view as? NSTextField { return field }
        for child in view.subviews { if let field = field(in: child) { return field } }
        return nil
    }

    /// A point in window coordinates over `range` of `field`'s text.
    private static func point(of range: NSRange, in field: NSTextField) -> NSPoint? {
        guard let window = field.window, let editor = window.fieldEditor(true, for: field) as? NSTextView else { return nil }
        field.cell?.select(withFrame: field.bounds, in: field, editor: editor, delegate: nil, start: 0, length: 0)
        defer { field.cell?.endEditing(editor) }
        guard let manager = editor.layoutManager, let container = editor.textContainer else { return nil }
        let glyphs = manager.glyphRange(forCharacterRange: range, actualCharacterRange: nil)
        let rect = manager.boundingRect(forGlyphRange: glyphs, in: container)
        return editor.convert(NSPoint(x: rect.minX + 4 + editor.textContainerOrigin.x, y: rect.midY + editor.textContainerOrigin.y), to: nil)
    }
}
