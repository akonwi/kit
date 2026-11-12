import AppKit
@preconcurrency import CodeEditSourceEditor
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// Links that name a file's line or lines, as GitHub writes them in the
/// target (`a.go#L42`, `a.go#L42-L50`), open the file's tab scrolled to the
/// lines and highlighted: selected in code, tinted in rendered Markdown.
@MainActor @Suite(.serialized) struct FileLineLinkTests {
    static func lines(_ start: Int, _ end: Int? = nil) -> FileLineRange { FileLineRange(start: start, end: end)! }

    // MARK: Opening

    @Test func aLineLinkOpensItsFileAndAsksForItsLines() throws {
        let workspace = WorkspaceState()
        workspace.currentDirectory = { "/repo" }
        #expect(workspace.openLink(try #require(FilePaths.link("/repo/src/a.go", lines: Self.lines(42, 50))), beside: .conversation))
        let pane = WorkspacePane.file("src/a.go")
        #expect(workspace.panes == [.conversation, pane])
        #expect(workspace.selected == pane)
        let first = try #require(workspace.lineReveal(for: pane))
        #expect(first.lines == Self.lines(42, 50))

        // The open tab is reused, and moves to the new lines.
        workspace.select(.conversation)
        #expect(workspace.openLink(try #require(FilePaths.link("/repo/src/a.go", lines: Self.lines(7))), beside: .conversation))
        #expect(workspace.panes == [.conversation, pane])
        #expect(workspace.selected == pane)
        let second = try #require(workspace.lineReveal(for: pane))
        #expect(second.lines == Self.lines(7))
        #expect(second.id != first.id)

        // An earlier request finishing leaves the later one; its own finishes it.
        workspace.finishLineReveal(first, in: pane)
        #expect(workspace.lineReveal(for: pane) == second)
        workspace.finishLineReveal(second, in: pane)
        #expect(workspace.lineReveal(for: pane) == nil)

        // The same lines again ask again, as following a link twice does.
        #expect(workspace.openLink(try #require(FilePaths.link("/repo/src/a.go", lines: Self.lines(7))), beside: .conversation))
        #expect(workspace.lineReveal(for: pane)?.lines == Self.lines(7))
        workspace.close(pane)
        #expect(workspace.lineReveal(for: pane) == nil)

        // A link without lines asks for none.
        #expect(workspace.openLink(try #require(FilePaths.link("/etc/hosts")), beside: .conversation))
        #expect(workspace.lineReveal(for: .hostFile("/etc/hosts")) == nil)
    }

    @Test func aMarkdownFilesLineLinkOpensItsFileAtTheLines() throws {
        _ = NSApplication.shared
        let workspace = WorkspaceState()
        workspace.currentDirectory = { "/work" }
        let readme = WorkspacePane.file("docs/README.md")
        workspace.open(readme)
        let link = try #require(FilePaths.link("guide.md", lines: Self.lines(4, 6)))
        OpenURLAction.markdownFile(workspace, file: readme, cwd: "/work", externally: { _ in false })(link)
        #expect(workspace.panes == [.conversation, readme, .file("docs/guide.md")])
        #expect(workspace.lineReveal(for: .file("docs/guide.md"))?.lines == Self.lines(4, 6))
        // A link to lines in the file itself moves within it.
        OpenURLAction.markdownFile(workspace, file: readme, cwd: "/work", externally: { _ in false })(
            try #require(FilePaths.link("README.md", lines: Self.lines(2))))
        #expect(workspace.panes == [.conversation, readme, .file("docs/guide.md")])
        #expect(workspace.selected == readme)
        #expect(workspace.lineReveal(for: readme)?.lines == Self.lines(2))
    }

    // MARK: Highlighting

    @Test func theLinesRangeCoversThemWithoutTheirBreaks() {
        let text = "one\ntwo\nthree\n"
        #expect(FileLineRevealer.range(of: Self.lines(1), in: text) == NSRange(location: 0, length: 3))
        #expect(FileLineRevealer.range(of: Self.lines(2), in: text) == NSRange(location: 4, length: 3))
        #expect(FileLineRevealer.range(of: Self.lines(2, 3), in: text) == NSRange(location: 4, length: 9))
        // A range past the end stops at the last line; one starting past it is none.
        #expect(FileLineRevealer.range(of: Self.lines(3, 99), in: text) == NSRange(location: 8, length: 5))
        #expect(FileLineRevealer.range(of: Self.lines(4), in: text) == nil)
        #expect(FileLineRevealer.range(of: Self.lines(2), in: "one\r\ntwo\r\n") == NSRange(location: 5, length: 3))
        #expect(FileLineRevealer.range(of: Self.lines(2), in: "a\n\nc") == NSRange(location: 2, length: 0))
        #expect(FileLineRevealer.range(of: Self.lines(2), in: "🌿\nb") == NSRange(location: 3, length: 1))
        #expect(FileLineRevealer.range(of: Self.lines(1), in: "") == NSRange(location: 0, length: 0))
    }

    static let source = (1...200).map { "let value\($0) = \($0)" }.joined(separator: "\n") + "\n"

    static func controller(in view: NSView) -> TextViewController? {
        if let controller = view.nextResponder as? TextViewController { return controller }
        return view.subviews.compactMap { controller(in: $0) }.first
    }

    /// Whether `controller` selects exactly `lines` of `source` and shows
    /// them in the middle of its view.
    static func expectRevealed(_ lines: FileLineRange, in controller: TextViewController,
                               sourceLocation: SourceLocation = #_sourceLocation) throws {
        let range = try #require(FileLineRevealer.range(of: lines, in: source), sourceLocation: sourceLocation)
        #expect(controller.textView.selectedRange() == range, sourceLocation: sourceLocation)
        let manager = controller.textView.layoutManager!
        let first = try #require(manager.textLineForIndex(lines.start - 1), sourceLocation: sourceLocation)
        let last = try #require(manager.textLineForIndex(lines.end - 1), sourceLocation: sourceLocation)
        let visible = controller.scrollView.contentView.bounds
        #expect(abs((first.yPos + last.yPos + last.height) / 2 - visible.midY) < 2, sourceLocation: sourceLocation)
        #expect(controller.view.window?.firstResponder === controller.textView, sourceLocation: sourceLocation)
    }

    @Test func aReadOnlyEditorSelectsAndCentresTheLinesOnce() async throws {
        _ = NSApplication.shared
        let model = RevealModel()
        model.reveal = FileLineReveal(lines: Self.lines(120, 122))
        let host = NSHostingView(rootView: ReadOnlyHarness(model: model))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 500, height: 300))
        window.contentView = host; window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await eventually { model.reveal == nil }
        let controller = try #require(Self.controller(in: host))
        try Self.expectRevealed(Self.lines(120, 122), in: controller)

        // The next request moves the selection; a click's selection stays.
        model.reveal = FileLineReveal(lines: Self.lines(30))
        try await eventually { model.reveal == nil }
        try Self.expectRevealed(Self.lines(30), in: controller)
        controller.setCursorPositions([CursorPosition(range: NSRange(location: 3, length: 0))])
        try await Task.sleep(for: .milliseconds(100))
        #expect(controller.textView.selectedRange() == NSRange(location: 3, length: 0))
    }

    @Test func anAnnotatedEditorSelectsAndCentresTheLines() async throws {
        _ = NSApplication.shared
        let model = RevealModel()
        model.reveal = FileLineReveal(lines: Self.lines(150))
        let host = NSHostingView(rootView: AnnotatedHarness(file: try InlineAnnotationEditorTests.file(Self.source), model: model))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentView = host; window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await eventually { model.reveal == nil }
        try Self.expectRevealed(Self.lines(150), in: try #require(Self.controller(in: host)))
    }

    @Test func renderedMarkdownHighlightsTheRowsShowingTheLines() {
        let source = "# Title\n\nPara one\nline two\n\n- a\n- b\n  more\n\n```go\nx\n```\n\nSetext\n===\n"
        let rows = MarkdownDocumentRow.rows(MarkdownContent(source: source))
        #expect(MarkdownDocumentRow.rows(showing: Self.lines(4), in: rows) == [1])
        #expect(MarkdownDocumentRow.rows(showing: Self.lines(4, 7), in: rows) == [1, 2, 3])
        #expect(MarkdownDocumentRow.rows(showing: Self.lines(8, 11), in: rows) == [3, 4])
        // A blank line between rows is the row before's.
        #expect(MarkdownDocumentRow.rows(showing: Self.lines(5), in: rows) == [1])
        #expect(MarkdownDocumentRow.rows(showing: Self.lines(99), in: rows) == [5])
    }

    @Test func renderedMarkdownScrollsTheLinesRowIntoViewAndTintsIt() async throws {
        _ = NSApplication.shared
        let model = RevealModel()
        model.reveal = FileLineReveal(lines: Self.lines(161))
        let source = (1...100).map { "Paragraph \($0)." }.joined(separator: "\n\n") + "\n"
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: MarkdownHarness(source: source, model: model)
            .environment(\.mica, theme).background(theme.surface).frame(width: 600, height: 300))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentView = host; window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await eventually { model.reveal == nil }
        // Line 161 is paragraph 81's; it shows in the middle of the view.
        try await eventually { Self.field(showing: "Paragraph 81.", in: host) != nil }
        let field = try #require(Self.field(showing: "Paragraph 81.", in: host))
        try await Task.sleep(for: .milliseconds(400))
        let frame = field.convert(field.bounds, to: host)
        #expect(abs(frame.midY - host.bounds.midY) < 20)
        // Its row is tinted in the accent, behind the text. (Displays blend
        // in their own colour space, so the tint is near the sRGB blend.)
        let image = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: image)
        let scale = CGFloat(image.pixelsWide) / host.bounds.width
        func color(at point: NSPoint) -> NSColor? {
            // Bitmap rows run top-down; the host is flipped like them.
            let y = host.isFlipped ? point.y : host.bounds.height - point.y
            return image.colorAt(x: Int(point.x * scale), y: Int(y * scale))?.usingColorSpace(.sRGB)
        }
        let tinted = try #require(color(at: NSPoint(x: frame.maxX + 40, y: frame.midY)))
        let accent = try #require(NSColor(theme.accent).usingColorSpace(.sRGB))
        let surface = try #require(NSColor(theme.surface).usingColorSpace(.sRGB))
        let expected = surface.blended(withFraction: 0.12, of: accent)!
        #expect(abs(tinted.redComponent - expected.redComponent) < 0.04)
        #expect(abs(tinted.greenComponent - expected.greenComponent) < 0.04)
        #expect(abs(tinted.blueComponent - expected.blueComponent) < 0.04)
        // The next row isn't.
        let next = try #require(color(at: NSPoint(x: frame.maxX + 40, y: frame.midY + (host.isFlipped ? 40 : -40))))
        #expect(abs(next.blueComponent - surface.blueComponent) < 0.01)

        // A click in the document clears the tint; one outside it doesn't.
        let monitor = try #require(Self.monitor(in: host))
        func click(_ point: NSPoint) throws -> NSEvent {
            try #require(NSEvent.mouseEvent(with: .leftMouseDown, location: point, modifierFlags: [], timestamp: 0,
                                            windowNumber: window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
        }
        monitor.observe(try click(NSPoint(x: -20, y: -20)))
        try await Task.sleep(for: .milliseconds(100))
        host.cacheDisplay(in: host.bounds, to: image)
        #expect(abs((color(at: NSPoint(x: frame.maxX + 40, y: frame.midY))?.blueComponent ?? 0) - tinted.blueComponent) < 0.01)
        monitor.observe(try click(host.convert(NSPoint(x: host.bounds.midX, y: host.bounds.midY), to: nil)))
        try await Task.sleep(for: .milliseconds(100))
        host.cacheDisplay(in: host.bounds, to: image)
        #expect(abs((color(at: NSPoint(x: frame.maxX + 40, y: frame.midY))?.blueComponent ?? 0) - surface.blueComponent) < 0.01)
    }

    static func monitor(in view: NSView) -> MouseDownMonitor.MonitorView? {
        if let monitor = view as? MouseDownMonitor.MonitorView { return monitor }
        return view.subviews.lazy.compactMap { monitor(in: $0) }.first
    }

    static func field(showing text: String, in view: NSView) -> NSTextField? {
        if let field = view as? NSTextField, field.stringValue == text { return field }
        return view.subviews.lazy.compactMap { field(showing: text, in: $0) }.first
    }

    // MARK: Menus and hover

    static let menuSource = "Read [the handler](/srv/server.go#L42-L50) and [the notes](/srv/notes.md)."

    @Test func aLineLinksMenuNamesItsLinesAndCopiesWithOrWithoutThem() throws {
        let menus = TextLinkMenuTests.Menus(source: Self.menuSource)
        defer { menus.close() }
        let menu = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the handler")))
        #expect(menus.titles(menu) == ["server.go#L42-L50", "Open File", "Copy Path", "Copy Path and Line"]
                + Array(TextLinkMenuTests.fileItems.dropFirst(2)) + TextLinkMenuTests.message)
        #expect(menu.items[0].isEnabled == false)
        menu.performActionForItem(at: 2)
        #expect(menus.pasteboard.string(forType: .string) == "/srv/server.go")
        menu.performActionForItem(at: 3)
        #expect(menus.pasteboard.string(forType: .string) == "/srv/server.go#L42-L50")
        menu.performActionForItem(at: 1)
        #expect(menus.workspace.panes == [.conversation, .hostFile("/srv/server.go")])
        #expect(menus.workspace.lineReveal(for: .hostFile("/srv/server.go"))?.lines == Self.lines(42, 50))
        // A link to a whole file keeps its menu.
        let whole = try #require(menus.rendered.hover.linkMenu(for: try menus.click(.rightMouseDown, on: "the notes")))
        #expect(menus.titles(whole) == TextLinkMenuTests.fileItems + TextLinkMenuTests.message)
    }

    @Test func aHoveredFileLinkShowsItsPathAndLines() throws {
        let menus = TextLinkMenuTests.Menus(source: Self.menuSource)
        defer { menus.close() }
        let field = try #require(menus.rendered.field)
        let handler = try #require(try menus.rendered.editorRects(menus.rendered.range(of: "the handler")).first)
        let notes = try #require(try menus.rendered.editorRects(menus.rendered.range(of: "the notes")).first)
        let plain = try #require(try menus.rendered.editorRects(menus.rendered.range(of: "Read")).first)
        menus.rendered.hover.update(at: NSPoint(x: handler.midX, y: handler.midY))
        #expect(field.toolTip == "/srv/server.go#L42-L50")
        menus.rendered.hover.update(at: NSPoint(x: notes.midX, y: notes.midY))
        #expect(field.toolTip == "/srv/notes.md")
        menus.rendered.hover.update(at: NSPoint(x: plain.midX, y: plain.midY))
        #expect(field.toolTip == nil)
        #expect(TextLinkHover.toolTip(for: URL(string: "https://example.com")!) == nil)
    }
}

@MainActor @Observable private final class RevealModel {
    var reveal: FileLineReveal?
    var position = SourceEditorState()
}

private struct ReadOnlyHarness: View {
    @Bindable var model: RevealModel
    var body: some View {
        ReadOnlyFileEditor(path: "test.swift", source: FileLineLinkTests.source, position: $model.position,
                           lineReveal: model.reveal, lineRevealed: { [model] done in if model.reveal == done { model.reveal = nil } })
    }
}

private struct AnnotatedHarness: View {
    let file: WireWorkspaceFileRead
    @Bindable var model: RevealModel
    @State private var annotations = AnnotationState()
    var body: some View {
        AnnotatedFileEditor(file: file, position: $model.position, annotations: annotations,
                            client: InlineAnnotationClient(), session: "session_test",
                            lineReveal: model.reveal, lineRevealed: { [model] done in if model.reveal == done { model.reveal = nil } })
    }
}

private struct MarkdownHarness: View {
    let source: String
    @Bindable var model: RevealModel
    var body: some View {
        MarkdownDocumentView(source: source, lineReveal: model.reveal,
                             lineRevealed: { [model] done in if model.reveal == done { model.reveal = nil } })
    }
}
