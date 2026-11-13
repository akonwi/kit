import AppKit
@preconcurrency import CodeEditSourceEditor
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// Find in a file tab: the find bar's state, its matches drawn in the code
/// editor and in rendered Markdown, and its keys.
@MainActor struct FileFindTests {
    /// 200 lines; `handler` is on every tenth, from line 10.
    static let source = (1...200).map { $0 % 10 == 0 ? "    s.mux.Handle(\"/v\($0)\", s.handler(h\($0)))" : "let value\($0) = \($0)" }
        .joined(separator: "\n") + "\n"

    /// The UTF-16 range of `handler` on line `line` of `source`.
    static func handler(onLine line: Int) -> Range<Int> {
        let lines = source.components(separatedBy: "\n")
        let start = lines[..<(line - 1)].reduce(0) { $0 + $1.utf16.count + 1 }
        let column = (lines[line - 1] as NSString).range(of: "handler").location
        return start + column..<start + column + 7
    }

    static func found(_ find: FileFind) -> [Range<Int>] { find.matches.map(\.range) }

    // MARK: The bar's state

    @Test func aSearchFindsEveryMatchAndCountsThem() async {
        let find = FileFind()
        find.setPieces(["handler := s.Handler(HANDLER)"], corpus: .source)
        find.open()
        find.query = "handler"
        await find.refresh()
        #expect(Self.found(find) == [0..<7, 13..<20, 21..<28])
        #expect(find.current == 0)
        #expect(find.label == "1 of 3")
        #expect(find.revealRequest == 1)
    }

    @Test func matchCaseKeepsOnlyTheExactCase() async {
        let find = FileFind()
        find.setPieces(["handler := s.Handler(HANDLER)"], corpus: .source)
        find.open()
        find.query = "Handler"
        find.matchCase = true
        await find.refresh()
        #expect(Self.found(find) == [13..<20])
        #expect(find.label == "1 of 1")
        find.matchCase = false
        await find.refresh()
        #expect(Self.found(find) == [0..<7, 13..<20, 21..<28])
    }

    @Test func wholeWordSkipsMatchesInsideWords() async {
        let find = FileFind()
        find.setPieces(["log logger catalog log(x)"], corpus: .source)
        find.open()
        find.query = "log"
        find.wholeWord = true
        await find.refresh()
        #expect(Self.found(find) == [0..<3, 19..<22])
    }

    @Test func nextAndPreviousWrap() async {
        let find = FileFind()
        find.setPieces(["a a a"], corpus: .source)
        find.open()
        find.query = "a"
        await find.refresh()
        #expect(find.current == 0)
        find.next(); #expect(find.current == 1)
        find.next(); #expect(find.current == 2)
        find.next(); #expect(find.current == 0)
        find.previous(); #expect(find.current == 2)
        #expect(find.label == "3 of 3")
        #expect(find.revealRequest == 5)
    }

    @Test func aSearchStartsAtTheTopOfTheView() async {
        let find = FileFind()
        find.setPieces([Self.source], corpus: .source)
        find.visibleAnchor = { FindPosition(piece: 0, offset: Self.handler(onLine: 100).lowerBound - 20) }
        find.open()
        find.query = "handler"
        await find.refresh()
        #expect(find.current == 9)
        #expect(find.currentMatch?.range == Self.handler(onLine: 100))
        #expect(find.label == "10 of 20")
    }

    @Test func typingMoreKeepsTheCurrentMatch() async {
        let find = FileFind()
        find.setPieces(["hand handle handler"], corpus: .source)
        find.open()
        find.query = "hand"
        await find.refresh()
        find.next(); find.next()
        #expect(find.currentMatch?.range == 12..<16)
        find.query = "handl"
        await find.refresh()
        #expect(find.currentMatch?.range == 12..<17)
        #expect(find.label == "2 of 2")
    }

    @Test func noMatchesSaySoAndHaveNoCurrent() async {
        let find = FileFind()
        find.setPieces(["handler"], corpus: .source)
        find.open()
        find.query = "handlr"
        await find.refresh()
        #expect(find.matches.isEmpty)
        #expect(find.current == nil)
        #expect(find.label == "No matches")
        find.query = ""
        #expect(find.label == "")
    }

    @Test func aFileCutShortSaysHowFarItWasSearched() async {
        let find = FileFind()
        find.setPieces(["a\na\n"], corpus: .source)
        find.searchedLines = FileFind.searchedLines("a\na\n", truncated: true)
        find.open()
        find.query = "a"
        await find.refresh()
        #expect(find.label == "1 of 2 in the first 2 lines")
        #expect(FileFind.searchedLines("a\nb", truncated: true) == 2)
        #expect(FileFind.searchedLines("a\nb", truncated: false) == 0)
    }

    @Test func closingKeepsTheQueryAndFindNextResumesAfterTheLastMatch() async {
        let find = FileFind()
        find.setPieces(["ab ab ab ab"], corpus: .source)
        find.open()
        find.query = "ab"
        await find.refresh()
        find.next()
        #expect(find.currentMatch?.range == 3..<5)
        find.close()
        #expect(!find.isOpen)
        #expect(find.dismissRequest == 1)
        #expect(find.query == "ab")

        // Find Next reopens the bar and goes on from where it was.
        find.next()
        #expect(find.isOpen)
        await find.refresh()
        #expect(find.currentMatch?.range == 6..<8)
        find.close()
        find.previous()
        await find.refresh()
        #expect(find.currentMatch?.range == 3..<5)
    }

    @Test func reopeningSearchesFromTheTopOfTheView() async {
        let find = FileFind()
        find.setPieces(["ab ab ab ab"], corpus: .source)
        find.open()
        find.query = "ab"
        await find.refresh()
        find.next(); find.next()
        find.close()
        find.visibleAnchor = { FindPosition(piece: 0, offset: 4) }
        find.open()
        #expect(find.focusRequest == 2)
        await find.refresh()
        #expect(find.currentMatch?.range == 6..<8)
    }

    @Test func useSelectionSearchesForItFromWhereItIs() async {
        let find = FileFind()
        find.setPieces(["ab cd ab cd ab"], corpus: .source)
        find.selectedText = { ("cd\nmore", FindPosition(piece: 0, offset: 9)) }
        find.useSelection()
        #expect(find.query == "cd")
        #expect(!find.isOpen)
        find.next()
        await find.refresh()
        // The used selection is the second cd; Find Next goes on to the first.
        #expect(find.currentMatch?.range == 3..<5)
    }

    @Test func showingSomethingElseForgetsTheCurrentMatch() async {
        let find = FileFind()
        find.setPieces(["x ab ab"], corpus: .source)
        find.open()
        find.query = "ab"
        await find.refresh()
        find.next()
        find.setPieces(["ab", "ab"], corpus: .rendered)
        #expect(find.current == nil)
        await find.refresh()
        #expect(find.matches == [FindMatch(piece: 0, range: 0..<2), FindMatch(piece: 1, range: 0..<2)])
        #expect(find.current == 0)
    }

    @Test func piecesAreSearchedApartWithOneLimit() {
        let pieces = ["ab a", "b ab", "ab"]
        let all = FileFind.search(pieces, query: "ab", options: .init())
        #expect(all.matches == [.init(piece: 0, range: 0..<2), .init(piece: 1, range: 2..<4), .init(piece: 2, range: 0..<2)])
        #expect(!all.capped)
        let two = FileFind.search(pieces, query: "ab", options: .init(limit: 2))
        #expect(two.matches == [.init(piece: 0, range: 0..<2), .init(piece: 1, range: 2..<4)])
        #expect(two.capped)
        let three = FileFind.search(pieces, query: "ab", options: .init(limit: 3))
        #expect(three.matches.count == 3)
        #expect(!three.capped)
    }

    @Test func onlyFileTabsHaveAFindBarAndClosingOneDropsIt() {
        let workspace = WorkspaceState()
        let pane = WorkspacePane.file("a.go")
        workspace.open(pane)
        let find = workspace.find(for: pane)
        #expect(find != nil)
        #expect(workspace.find(for: pane) === find)
        #expect(workspace.find(for: .hostFile("/etc/hosts")) != nil)
        #expect(workspace.find(for: .localFile("/tmp/a")) != nil)
        #expect(workspace.find(for: .conversation) == nil)
        #expect(workspace.find(for: .review) == nil)
        #expect(workspace.find(for: .scratchpad) == nil)
        workspace.close(pane)
        workspace.open(pane)
        #expect(workspace.find(for: pane) !== find)
    }

    // MARK: The code editor

    /// A read-only editor showing `source` at 600 × 300 in a window, its
    /// painter following `find`.
    static func editor(_ source: String = source, find: FileFind) throws -> (FileFindPainter, TextViewController, NSWindow) {
        _ = NSApplication.shared
        let painter = FileFindPainter()
        let theme = MicaTheme(dark: true)
        let controller = TextViewController(string: source, language: .default,
            configuration: FileEditorTheme.configuration(theme), cursorPositions: [], highlightProviders: [],
            coordinators: [painter])
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentViewController = controller
        window.setContentSize(NSSize(width: 600, height: 300))
        window.makeKeyAndOrderFront(nil)
        controller.view.layoutSubtreeIfNeeded()
        painter.update(find: find, source: source, theme: theme)
        return (painter, controller, window)
    }

    /// The text view's rects for the matches starting on screen, current or not.
    static func expectedTints(_ find: FileFind, in controller: TextViewController) -> [CGRect] {
        guard let visible = controller.textView.visibleTextRange else { return [] }
        return find.matches.filter { NSLocationInRange($0.range.lowerBound, visible) }
            .flatMap { controller.textView.layoutManager.rectsFor(range: $0.nsRange) }
    }

    @Test func theCurrentMatchIsSelectedAndScrolledToTheMiddle() async throws {
        let find = FileFind()
        let (painter, controller, window) = try Self.editor(find: find)
        defer { window.close() }
        find.open()
        find.query = "handler"
        await find.refresh()
        painter.sync()
        // The view starts at the top: line 10's is current, already in view.
        let first = Self.handler(onLine: 10)
        #expect(controller.textView.selectedRange() == NSRange(location: first.lowerBound, length: 7))
        #expect(controller.scrollView.contentView.bounds.minY <= 0)

        // Line 150's is out of view; it scrolls to the middle.
        let responder = window.firstResponder
        for _ in 0..<14 { find.next() }
        painter.sync()
        let range = Self.handler(onLine: 150)
        #expect(find.currentMatch?.range == range)
        #expect(controller.textView.selectedRange() == NSRange(location: range.lowerBound, length: 7))
        let line = try #require(controller.textView.layoutManager.textLineForOffset(range.lowerBound))
        let clip = controller.scrollView.contentView
        #expect(abs(line.yPos + line.height / 2 - (clip.bounds.midY + clip.contentInsets.top / 2)) < 2)
        // Its box is laid out once it's there, and on screen.
        let box = try #require(controller.textView.layoutManager.rectsFor(range: NSRange(location: range.lowerBound, length: 7)).first)
        #expect(clip.bounds.contains(box))
        // Showing a match leaves the keyboard where it was, as in the bar's field.
        #expect(window.firstResponder === responder)
    }

    @Test func matchesOnScreenAreTintedTheCurrentOneMoreStrongly() async throws {
        let find = FileFind()
        let (painter, controller, window) = try Self.editor(find: find)
        defer { window.close() }
        find.open()
        find.query = "handler"
        await find.refresh()
        painter.sync()
        find.next()
        painter.sync()
        let tints = painter.tints(in: controller.textView.visibleRect)
        let expected = Self.expectedTints(find, in: controller)
        #expect(tints.map(\.rect) == expected)
        #expect(!tints.isEmpty)
        let current = try #require(controller.textView.layoutManager.rectsFor(range: NSRange(location: Self.handler(onLine: 20).lowerBound, length: 7)).first)
        #expect(tints.map(\.current) == tints.map { $0.rect == current })
        #expect(tints.filter(\.current).count == 1)

        // Drawn beneath the text: the accent, faint and strong.
        let (match, strong) = FileFindPainter.colors(MicaTheme(dark: true))
        #expect(painter.matchColor == match)
        #expect(painter.currentColor == strong)

        // A closed bar draws nothing.
        find.close()
        #expect(painter.tints(in: controller.textView.visibleRect).isEmpty)
    }

    @Test func aLargeFileTintsOnlyWhatsOnScreen() async throws {
        let source = (1...5_000).map { "handler \($0) handler" }.joined(separator: "\n")
        let find = FileFind()
        let (painter, controller, window) = try Self.editor(source, find: find)
        defer { window.close() }
        find.open()
        find.query = "handler"
        // The search itself is quick; the refresh runs it off the main actor.
        let elapsed = ContinuousClock().measure { _ = FileFind.search([source], query: "handler", options: .init()) }
        #expect(elapsed < .seconds(1))
        await find.refresh()
        #expect(find.matches.count == 10_000)
        #expect(!find.capped)
        painter.sync()
        // Only the matches on the lines on screen are drawn: two a line.
        let visible = try #require(controller.textView.visibleTextRange)
        let lines = source.components(separatedBy: "\n")
        var offset = 0, firstLine = 0, lastLine = 0
        for (index, line) in lines.enumerated() {
            if offset <= visible.location { firstLine = index }
            if offset < NSMaxRange(visible) { lastLine = index }
            offset += line.utf16.count + 1
        }
        #expect(painter.matchesOnScreen() == 2 * firstLine..<2 * (lastLine + 1))
        #expect(lastLine - firstLine < 30)
        let tints = painter.tints(in: controller.textView.visibleRect)
        #expect(tints.map(\.rect) == Self.expectedTints(find, in: controller))
    }

    // MARK: Keys

    /// A find bar over a read-only editor, as a file tab shows them.
    private struct BarHarness: View {
        let find: FileFind
        var body: some View {
            VStack(spacing: 0) {
                if find.isOpen { FileFindBar(find: find) }
                ReadOnlyFileEditor(path: "a.swift", source: FileFindTests.source, find: find)
            }
            .environment(\.mica, MicaTheme(dark: true))
        }
    }

    static func controller(in view: NSView) -> TextViewController? {
        if let controller = view.nextResponder as? TextViewController { return controller }
        return view.subviews.lazy.compactMap { controller(in: $0) }.first
    }

    /// Presses a key in the find field, as its field editor receives it.
    static func key(_ characters: String, code: UInt16, shift: Bool = false, in window: NSWindow) throws {
        let editor = try #require(window.firstResponder as? FindFieldEditor)
        let event = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: shift ? [.shift] : [],
            timestamp: 0, windowNumber: window.windowNumber, context: nil, characters: characters,
            charactersIgnoringModifiers: characters, isARepeat: false, keyCode: code))
        editor.keyDown(with: event)
    }

    @Test func returnAndShiftReturnStepAndEscClosesBackToTheFile() async throws {
        _ = NSApplication.shared
        let find = FileFind()
        let host = NSHostingView(rootView: BarHarness(find: find))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 340))
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        find.open()
        // The field takes the keyboard; typing searches.
        try await eventually { window.firstResponder is FindFieldEditor }
        let editor = try #require(window.firstResponder as? NSTextView)
        editor.insertText("handler", replacementRange: editor.selectedRange())
        #expect(find.query == "handler")
        try await eventually { find.current == 0 }

        try Self.key("\r", code: 36, in: window)
        #expect(find.current == 1)
        try Self.key("\r", code: 36, shift: true, in: window)
        #expect(find.current == 0)
        try Self.key("\r", code: 36, shift: true, in: window)
        #expect(find.current == 19)
        try await eventually { Self.controller(in: host)?.textView.selectedRange().location == Self.handler(onLine: 200).lowerBound }

        // Esc closes the bar; the current match stays selected and the file
        // has the keyboard.
        try Self.key("\u{1b}", code: 53, in: window)
        #expect(!find.isOpen)
        let controller = try #require(Self.controller(in: host))
        try await eventually { window.firstResponder === controller.textView }
        #expect(controller.textView.selectedRange() == NSRange(location: Self.handler(onLine: 200).lowerBound, length: 7))
    }

    private struct AnnotatedHarness: View {
        let file: WireWorkspaceFileRead
        let find: FileFind
        @State private var position = SourceEditorState()
        @State private var annotations = AnnotationState()
        var body: some View {
            AnnotatedFileEditor(file: file, position: $position, annotations: annotations,
                                client: InlineAnnotationClient(), session: "session_test", find: find)
                .environment(\.mica, MicaTheme(dark: true))
        }
    }

    @Test func aWorkspaceFilesAnnotatedEditorShowsTheCurrentMatch() async throws {
        _ = NSApplication.shared
        let find = FileFind()
        let host = NSHostingView(rootView: AnnotatedHarness(file: try InlineAnnotationEditorTests.file(Self.source), find: find))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentView = host; window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await eventually { find.corpus == .source }
        #expect(find.pieces == [Self.source])
        find.open()
        find.query = "handler"
        await find.refresh()
        for _ in 0..<11 { find.next() }
        let range = Self.handler(onLine: 120)
        #expect(find.currentMatch?.range == range)
        let controller = try #require(Self.controller(in: host))
        try await eventually { controller.textView.selectedRange() == NSRange(location: range.lowerBound, length: 7) }
        let line = try #require(controller.textView.layoutManager.textLineForOffset(range.lowerBound))
        let clip = controller.scrollView.contentView
        #expect(clip.bounds.contains(NSPoint(x: clip.bounds.midX, y: line.yPos + line.height / 2)))
    }

    // MARK: Rendered Markdown

    static let markdown = """
        # Pairing a device

        A new device shows a *pairing code* until the server accepts it.

        > Codes last ten minutes.

        - Phones scan the pairing code.
        - Desktops paste the link.

        | Step | What |
        | --- | --- |
        | 1 | Read the pairing code |

        ```sh
        kit server pair --code
        ```

        ```mermaid
        graph TD; code-->pair
        ```

        """

    @Test func renderedMarkdownSearchesTheTextEachRowShows() {
        let rows = MarkdownDocumentRow.rows(MarkdownContent(source: Self.markdown))
        #expect(rows.map(MarkdownFind.pieces) == [
            ["Pairing a device"],
            ["A new device shows a pairing code until the server accepts it."],
            ["Codes last ten minutes."],
            ["Phones scan the pairing code."],
            ["Desktops paste the link."],
            ["Step", "What", "1", "Read the pairing code"],
            ["kit server pair --code\n"],
            [],
        ])
        let index = MarkdownFind.Index(rows: rows)
        #expect(index.starts == [0, 1, 2, 3, 4, 5, 9, 10, 10])
        #expect(index.row(of: 0) == 0)
        #expect(index.row(of: 7) == 5)
        #expect(index.row(of: 9) == 6)
        #expect(index.row(of: 10) == nil)
        #expect(index.pieces(of: 5) == 5..<9)
        #expect(index.pieces(of: 7) == 10..<10)
    }

    @Test func renderedMarkdownMarksMatchesInItsText() throws {
        let rows = MarkdownDocumentRow.rows(MarkdownContent(source: Self.markdown))
        let marked = MarkdownFind.marked(rows[1], marks: [0: [(21..<33, .current)]])
        guard case .block(.paragraph(let text)) = marked.row.content else { Issue.record("no paragraph"); return }
        let runs = text.runs.map { (String(text[$0.range].characters), $0[FindMarkAttribute.self]) }
        #expect(runs.map(\.0) == ["A new device shows a ", "pairing code", " until the server accepts it."])
        #expect(runs.map(\.1) == [nil, .current, nil])
        #expect(marked.code.isEmpty)

        let table = MarkdownFind.marked(rows[5], marks: [3: [(9..<21, .match)]])
        guard case .block(.table(let content)) = table.row.content else { Issue.record("no table"); return }
        let cell = content.rows[0][1]
        #expect(cell.runs.compactMap { $0[FindMarkAttribute.self] == .match ? String(cell[$0.range].characters) : nil } == ["pairing code"])

        let code = MarkdownFind.marked(rows[6], marks: [0: [(4..<10, .match)]])
        #expect(code.code == ["kit server pair --code\n": [FindCodeMark(range: NSRange(location: 4, length: 6), mark: .match)]])
    }

    @Test func markingCountsUTF16AcrossEmoji() {
        var text = AttributedString("😀 find 😀find")
        MarkdownFind.mark(&text, [(3..<7, .match), (10..<14, .current)])
        let runs = text.runs.map { (String(text[$0.range].characters), $0[FindMarkAttribute.self]) }
        #expect(runs.map(\.0) == ["😀 ", "find", " 😀", "find"])
        #expect(runs.map(\.1) == [nil, .match, nil, .current])
    }

    private struct MarkdownHarness: View {
        let source: String
        let find: FileFind
        var body: some View {
            MarkdownDocumentView(source: source, find: find)
        }
    }

    private struct RenderedBarHarness: View {
        let source: String
        let find: FileFind
        var body: some View {
            VStack(spacing: 0) {
                if find.isOpen { FileFindBar(find: find) }
                MarkdownDocumentView(source: source, find: find)
            }
            .environment(\.mica, MicaTheme(dark: true))
        }
    }

    @Test func escInARenderedMarkdownFileGivesTheKeyboardToTheDocument() async throws {
        _ = NSApplication.shared
        let find = FileFind()
        let source = (1...40).map { "Paragraph \($0) has a handler." }.joined(separator: "\n\n") + "\n"
        let host = NSHostingView(rootView: RenderedBarHarness(source: source, find: find))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 340))
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await eventually { find.corpus == .rendered && find.pieces.count == 40 }
        find.open()
        try await eventually { window.firstResponder is FindFieldEditor }
        try Self.key("\u{1b}", code: 53, in: window)
        #expect(!find.isOpen)
        // The document, not the window, has the keyboard, so Page Down and
        // the arrows scroll it.
        try await eventually {
            guard let view = window.firstResponder as? NSView else { return false }
            return view.isDescendant(of: host) && !(view is FindFieldEditor)
        }
    }

    static func field(showing text: String, in view: NSView) -> NSTextField? {
        if let field = view as? NSTextField, field.stringValue.contains(text) { return field }
        return view.subviews.lazy.compactMap { field(showing: text, in: $0) }.first
    }

    @Test func renderedMarkdownScrollsToTheCurrentMatchAndFillsIt() async throws {
        _ = NSApplication.shared
        let find = FileFind()
        let source = (1...100).map { $0 == 81 ? "Paragraph \($0) has the needle." : "Paragraph \($0)." }.joined(separator: "\n\n") + "\n"
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: MarkdownHarness(source: source, find: find)
            .environment(\.mica, theme).background(theme.surface).frame(width: 600, height: 300))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentView = host; window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await eventually { find.corpus == .rendered && find.pieces.count == 100 }
        find.open()
        find.query = "needle"
        await find.refresh()
        #expect(find.matches == [FindMatch(piece: 80, range: 21..<27)])
        #expect(find.label == "1 of 1")

        // Its row scrolls to the middle of the view.
        try await eventually { Self.field(showing: "has the needle", in: host) != nil }
        try await Task.sleep(for: .milliseconds(300))
        let field = try #require(Self.field(showing: "has the needle", in: host))
        let frame = field.convert(field.bounds, to: host)
        #expect(abs(frame.midY - host.bounds.midY) < 20)

        // The match is filled with the accent, and the rest of the row isn't.
        let image = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: image)
        let scale = CGFloat(image.pixelsWide) / host.bounds.width
        // Displays blend in their own colour space, so the fill reads near
        // the accent rather than exactly it: strongly blue, little red.
        func accentShare(_ rect: NSRect) -> Double {
            var hits = 0, total = 0
            for x in stride(from: rect.minX, to: rect.maxX, by: 1) {
                for y in stride(from: rect.minY, to: rect.maxY, by: 1) {
                    let row = host.isFlipped ? y : host.bounds.height - y
                    guard let color = image.colorAt(x: Int(x * scale), y: Int(row * scale))?.usingColorSpace(.sRGB) else { continue }
                    total += 1
                    if color.blueComponent > 0.8, color.redComponent < 0.4, color.greenComponent < 0.6 { hits += 1 }
                }
            }
            return total == 0 ? 0 : Double(hits) / Double(total)
        }
        // Measured with the field's own text, from its trailing edge.
        let shown = field.attributedStringValue
        let total = shown.size().width
        let before = shown.attributedSubstring(from: NSRange(location: 0, length: 21)).size().width
        let needle = shown.attributedSubstring(from: NSRange(location: 21, length: 6)).size().width
        let left = frame.maxX - total
        let match = NSRect(x: left + before + 2, y: frame.midY - 4, width: needle - 4, height: 8)
        let start = NSRect(x: left + 2, y: frame.midY - 4, width: before - 8, height: 8)
        #expect(accentShare(match) > 0.4)
        #expect(accentShare(start) < 0.05)
    }
}
