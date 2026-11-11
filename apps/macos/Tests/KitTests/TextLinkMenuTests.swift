import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The menu a right-click on a link in rendered Markdown shows: a web link
/// opens in Kit's browser as a click does, or in the default browser, or
/// copies; a file link opens or copies its path; a session mention opens
/// its session. A transcript message's copy items follow. Plain text keeps
/// the menu it had, and ⌘-clicking a web link opens the default browser.
@MainActor struct TextLinkMenuTests {
    static let source = "Read [the docs](https://example.com/guide) or [the notes](/srv/notes.md) or [the guide](docs/guide.md) before you start, then https://example.com/next. Ask #[session:ses_release]."
    static let fileItems = ["Open File", "Copy Path", "—", "Reveal in Finder", "Open in Default App", "Open With"]
    static let web = ["Open Link", "Open in Default Browser", "Copy Link"]
    static let message = ["—", "Copy Message as Markdown", "Copy Message as Plain Text"]

    /// Rendered Markdown whose web links open in `workspace` beside the
    /// conversation, as the transcript's do.
    @MainActor struct Menus {
        let rendered: TranscriptHoverTests.Rendered
        let workspace = WorkspaceState()
        let engines = FakeBrowserEngines()
        let pasteboard = NSPasteboard(name: NSPasteboard.Name("TextLinkMenuTests-" + UUID().uuidString))
        let defaultBrowser = DefaultBrowser()
        let sessions = DefaultBrowser()

        @MainActor final class DefaultBrowser {
            var opened: [URL] = []
            var sessions: [String] = []
        }

        init() {
            _ = NSApplication.shared
            let sessions = sessions
            let sessionLink = TranscriptSessionLink(serverID: "local", title: { $0 == "ses_release" ? "Release notes" : nil }) { id in
                MainActor.assumeIsolated { sessions.sessions.append(id) }
            }
            rendered = TranscriptHoverTests.Rendered(TextLinkMenuTests.source, message: true, sessionLink: sessionLink)
            workspace.makeBrowserEngine = engines.make
            workspace.currentDirectory = { "/work" }
            let open = OpenURLAction.transcript(workspace, beside: .conversation, sessionLink: sessionLink)
            rendered.hover.linkActions = .init(open: { open($0) },
                                               openInDefaultBrowser: { [defaultBrowser] in defaultBrowser.opened.append($0) },
                                               pasteboard: pasteboard,
                                               file: { [workspace] in workspace.fileActions(forLink: $0) })
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
        #expect(menus.titles(menus.rendered.hover.linkMenu(for: right)) == Self.web + Self.message)
        let control = try menus.click(.leftMouseDown, on: "the docs", modifiers: .control)
        #expect(menus.titles(menus.rendered.hover.linkMenu(for: control)) == Self.web + Self.message)
        // The transcript's own menus give way to it.
        #expect(TextLinkHover.takes(right))
        // A bare address is a link too.
        let bare = try menus.click(.rightMouseDown, on: "https://example.com/next")
        let menu = try #require(menus.rendered.hover.linkMenu(for: bare))
        #expect(menus.titles(menu) == Self.web + Self.message)
        menu.performActionForItem(at: 0)
        #expect(menus.workspace.panes.last.flatMap(menus.workspace.browser)?.url == URL(string: "https://example.com/next"))
    }

    @Test func aFileLinkOpensItsFileOrCopiesItsPath() throws {
        let menus = Menus()
        defer { menus.close() }
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the notes")))
        #expect(menus.titles(menu) == Self.fileItems + Self.message)
        menu.performActionForItem(at: 1)
        #expect(menus.pasteboard.string(forType: .string) == "/srv/notes.md")
        #expect(menus.workspace.panes == [.conversation])
        menu.performActionForItem(at: 0)
        #expect(menus.workspace.panes == [.conversation, .hostFile("/srv/notes.md")])
        #expect(menus.defaultBrowser.opened == [])
        // A relative link's path is the file's on the server.
        let relative = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the guide")))
        relative.performActionForItem(at: 1)
        #expect(menus.pasteboard.string(forType: .string) == "/work/docs/guide.md")
    }

    @Test(arguments: [(ThisMacGate.needsThisMac, "⊘ needs this Mac"), (.unverified, "⊘ waiting for the server")])
    func aFileOnAnotherMachineKeepsThisMacsItemsDimmedWithTheReason(gate: ThisMacGate, reason: String) throws {
        let menus = Menus()
        defer { menus.close() }
        menus.workspace.thisMac = { gate }
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the notes")))
        #expect(menus.titles(menu) == Self.fileItems + Self.message)
        let local = menu.items.filter { ["Reveal in Finder", "Open in Default App", "Open With"].contains($0.title) }
        #expect(local.map(\.isEnabled) == [false, false, false])
        #expect(local.map(\.subtitle) == [reason, reason, reason])
        #expect(local.last?.submenu == nil)
        // The others work anywhere.
        #expect(menu.items.prefix(2).map(\.isEnabled) == [true, true])
    }

    @Test func aFileOnThisMacRevealsAndOpensInOtherApps() throws {
        let menus = Menus()
        defer { menus.close() }
        let opened = ThisMacGateTests.Opened()
        menus.workspace.thisMac = { .available }
        menus.workspace.localFiles = opened.opener(apps: ["Zed", "BBEdit"], default: "TextEdit")
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the notes")))
        let local = menu.items.filter { ["Reveal in Finder", "Open in Default App", "Open With"].contains($0.title) }
        #expect(local.map(\.isEnabled) == [true, true, true])
        #expect(local.map(\.subtitle) == [nil, nil, nil])
        menu.performActionForItem(at: 3)
        #expect(opened.revealed == [URL(fileURLWithPath: "/srv/notes.md")])
        menu.performActionForItem(at: 4)
        #expect(opened.opened == [URL(fileURLWithPath: "/srv/notes.md")])
        let openWith = try #require(local.last?.submenu)
        #expect(menus.titles(openWith) == ["TextEdit (default)", "—", "BBEdit", "Zed", "—", "Other…"])
        openWith.performActionForItem(at: 2)
        #expect(opened.openedWith == ["/srv/notes.md with BBEdit.app"])
    }

    @Test func aSessionMentionOpensItsSessionOrCopiesItsID() throws {
        let menus = Menus()
        defer { menus.close() }
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "#Release notes")))
        #expect(menus.titles(menu) == ["Open Session", "Copy ID"] + Self.message)
        menu.performActionForItem(at: 1)
        #expect(menus.pasteboard.string(forType: .string) == "ses_release")
        #expect(menus.sessions.sessions == [])
        menu.performActionForItem(at: 0)
        #expect(menus.sessions.sessions == ["ses_release"])
        #expect(menus.workspace.panes == [.conversation])
    }

    @Test func theMessagesCopyItemsCopyIt() throws {
        let menus = Menus()
        defer { menus.close() }
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the docs")))
        let copy = try #require(menu.items.first { $0.title == "Copy Message as Markdown" })
        #expect(copy.target is MessageCopyTarget)
        #expect((copy.target as? MessageCopyTarget)?.markdown == Self.source)
    }

    @Test func plainTextAndAPlainClickKeepTheMenuTheyHad() throws {
        let menus = Menus()
        defer { menus.close() }
        let plain = try menus.click(.rightMouseDown, on: "before you start")
        #expect(menus.rendered.hover.linkMenu(for: plain) == nil)
        #expect(!TextLinkHover.takes(plain))
        let click = try menus.click(.leftMouseDown, on: "the docs")
        #expect(menus.rendered.hover.linkMenu(for: click) == nil)
        #expect(!menus.rendered.hover.openCommandClicked(click))
        #expect(!TextLinkHover.takes(click))
    }

    @Test func aCommandClickedWebLinkOpensInTheDefaultBrowser() throws {
        let menus = Menus()
        defer { menus.close() }
        let web = try menus.click(.leftMouseDown, on: "the docs", modifiers: .command)
        #expect(TextLinkHover.takes(web))
        #expect(menus.rendered.hover.openCommandClicked(web))
        #expect(menus.defaultBrowser.opened == [URL(string: "https://example.com/guide")!])
        #expect(menus.workspace.panes == [.conversation])
        // A file opens in Kit however it's clicked.
        let file = try menus.click(.leftMouseDown, on: "the notes", modifiers: .command)
        #expect(!TextLinkHover.takes(file))
        #expect(!menus.rendered.hover.openCommandClicked(file))
        #expect(menus.defaultBrowser.opened == [URL(string: "https://example.com/guide")!])
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
        #expect(Array(titles.prefix(4)) == Self.web + ["—"])
        // The selection's items take the message's place.
        #expect(!titles.contains("Copy Message as Markdown"))
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
        #expect(menus.titles(menus.rendered.hover.linkMenu(for: event)) == Self.web + Self.message)
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
