import AppKit
@preconcurrency import CodeEditSourceEditor
@preconcurrency import CodeEditTextView
import SwiftUI
import Testing
@testable import Kit

private struct InlineAnnotationClient: AnnotationClient {
    let serverID = "preview"
    let isDemo = true
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func annotations(_ session: String) async throws -> [FileAnnotation] { [] }
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(503) }
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(503) }
    func deleteAnnotation(_ session: String, id: UInt64) async throws {}
}

@MainActor @Suite(.serialized) struct InlineAnnotationEditorTests {
    static func file(_ source: String) throws -> WireWorkspaceFileRead {
        var payload = try #require(JSONSerialization.jsonObject(with: Data(#"{"sessionId":"session_test","workspace":{"sessionId":"session_test","cwd":"/tmp","workspaceId":"workspace_test","state":"ready","limits":{"maxPathBytes":4096,"maxPathComponents":64,"defaultDirectoryPageSize":100,"maxDirectoryPageSize":200,"maxDirectoryEntries":10000,"maxDirectoryResponseBytes":524288,"maxDirectoryObservationBytes":8388608,"maxPreviewBytes":1000000,"maxPreviewLines":5000,"maxActiveRequests":1,"maxPendingRequests":0}},"path":"sample.swift","revision":"file_test","size":0,"encoding":"utf-8","content":"","returnedBytes":0,"returnedLines":0,"truncated":false}"#.utf8)) as? [String: Any])
        payload["content"] = source; payload["size"] = source.utf8.count
        payload["returnedBytes"] = source.utf8.count; payload["returnedLines"] = source.split(separator: "\n").count
        return try JSONDecoder().decode(WireWorkspaceFileRead.self, from: JSONSerialization.data(withJSONObject: payload))
    }
    static func controller(in view: NSView) -> TextViewController? {
        if let controller = view.nextResponder as? TextViewController { return controller }
        return view.subviews.compactMap { controller(in: $0) }.first
    }

    /// Wait for SwiftUI to mount the input and AppKit to apply its queued focus
    /// request. Do not make it first responder here: that is behavior under test.
    static func waitForFocusedInput(in host: NSView, window: NSWindow,
                                    sourceLocation: SourceLocation = #_sourceLocation) async throws -> ComposerTextView {
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: .seconds(2))
        while clock.now < deadline {
            if let input = input(in: host), window.firstResponder === input { return input }
            try await Task.sleep(for: .milliseconds(20))
        }
        let input = try #require(input(in: host), sourceLocation: sourceLocation)
        try #require(window.firstResponder === input, sourceLocation: sourceLocation)
        return input
    }

    static func input(in view: NSView) -> ComposerTextView? {
        if let input = view as? ComposerTextView { return input }
        return view.subviews.compactMap { input(in: $0) }.first
    }

    @Test func inlineBlocksPreserveSourceSelectionLineNumbersAndScroll() async throws {
        _ = NSApplication.shared
        let source = "func send() {\n    submit(\"🌿\")\n}\n\nfunc cancel() {}\n" + String(repeating: "// More source\n", count: 80)
        let file = try Self.file(source)
        let note = try FileAnnotation(id: 1, anchor: .init(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: file.path, fileRevision: file.revision, startLine: 1, endLine: 3), workingTreeDiff: nil),
            body: "Preserve the draft when acceptance is uncertain.", source: "func send() {\n    submit(\"🌿\")\n}", stale: false, complete: true)
        for dark in [false, true] {
            let annotations = AnnotationState(); annotations.observe([note])
            let host = NSHostingView(rootView: Harness(file: file, annotations: annotations)
                .environment(\.mica, MicaTheme(dark: dark)).environment(\.colorScheme, dark ? .dark : .light))
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 460), styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false; window.contentView = host; window.makeKeyAndOrderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(400))
            let controller = try #require(Self.controller(in: host))
            let manager = controller.textView.layoutManager!
            #expect(controller.textView.string == source)
            #expect(manager.lineCount == 86)
            let gap = try #require(manager.blockHeights[2])
            #expect(gap >= 56)
            let anchored = try #require(manager.lineStorage.getLine(atIndex: 2))
            let next = try #require(manager.lineStorage.getLine(atIndex: 3))
            #expect(abs(next.yPos - anchored.yPos - anchored.data.lineFragments.height - gap) < 1)
            let selection = NSRange(location: 0, length: 36)
            controller.setCursorPositions([CursorPosition(range: selection)])
            #expect(controller.textView.selectedRange() == selection)
            #expect((controller.textView.string as NSString).substring(with: selection) == (source as NSString).substring(with: selection))
            let noteHost = try #require(controller.textView.subviews.first { $0 is NSHostingView<AnyView> })
            #expect(noteHost.frame.width > 600)
            #expect(noteHost.frame.minX >= manager.edgeInsets.left)
            #expect(noteHost.frame.minY >= anchored.yPos + anchored.data.lineFragments.height)
            #expect(noteHost.frame.maxY <= next.yPos)
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-inline-editor-\(dark ? "dark" : "light").png"))
            window.setContentSize(NSSize(width: 380, height: 460))
            try await Task.sleep(for: .milliseconds(200))
            #expect(noteHost.frame.width < 380)
            #expect(manager.blockHeights[2, default: 0] > gap)
            window.setContentSize(NSSize(width: 760, height: 460))
            try await Task.sleep(for: .milliseconds(150))
            annotations.begin(anchor: note.anchor, editing: note)
            try await Task.sleep(for: .milliseconds(200))
            #expect(annotations.draft == note.body)
            #expect(manager.blockHeights[2, default: 0] > gap)
            let input = try #require(Self.input(in: host))
            #expect(window.firstResponder === input)
            input.setSelectedRange(NSRange(location: input.string.utf16.count, length: 0))
            input.keyDown(with: try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: .command,
                timestamp: 0, windowNumber: window.windowNumber, context: nil, characters: "\r",
                charactersIgnoringModifiers: "\r", isARepeat: false, keyCode: 36)))
            #expect(annotations.draft == note.body + "\n")
            input.keyDown(with: try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [],
                timestamp: 0, windowNumber: window.windowNumber, context: nil, characters: "\u{1b}",
                charactersIgnoringModifiers: "\u{1b}", isARepeat: false, keyCode: 53)))
            #expect(annotations.editor == nil)
            try await Task.sleep(for: .milliseconds(150))
            controller.scrollView.contentView.scroll(to: CGPoint(x: 0, y: 350))
            controller.scrollView.reflectScrolledClipView(controller.scrollView.contentView)
            try await Task.sleep(for: .milliseconds(150))
            #expect(controller.scrollView.contentView.bounds.minY >= 349)
            annotations.observe([])
            try await Task.sleep(for: .milliseconds(150))
            #expect(manager.blockHeights == [:])
            #expect(controller.textView.string == source)
            #expect(controller.textView.selectedRange() == selection)
        }
    }

    @Test func gutterHoverClickAndDragOpenInlineComments() async throws {
        _ = NSApplication.shared
        let source = "func send() {\n    submit(\"🌿\")\n}\n\nfunc cancel() {}\n"
        let file = try Self.file(source)
        for dark in [false, true] {
            let annotations = AnnotationState()
            let host = NSHostingView(rootView: Harness(file: file, annotations: annotations)
                .environment(\.mica, MicaTheme(dark: dark)).environment(\.colorScheme, dark ? .dark : .light))
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 460), styleMask: [.titled, .closable], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false; window.contentView = host; window.makeKeyAndOrderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(250))
            let controller = try #require(Self.controller(in: host))
            func findGutter(_ view: NSView) -> AnnotationGutterOverlay? {
                if let gutter = view as? AnnotationGutterOverlay { return gutter }
                return view.subviews.compactMap { findGutter($0) }.first
            }
            let gutter = try #require(findGutter(controller.view))
            let manager = controller.textView.layoutManager!
            func event(_ type: NSEvent.EventType, line: Int, x: CGFloat = 10) throws -> NSEvent {
                let row = try #require(manager.lineStorage.getLine(atIndex: line))
                let point = gutter.convert(NSPoint(x: x, y: row.yPos + 5), to: nil)
                return try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [], timestamp: 0,
                    windowNumber: window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
            }
            // Hovering the code row also exposes the gutter affordance.
            gutter.mouseMoved(with: try event(.mouseMoved, line: 1, x: 200))
            #expect(gutter.hovered == 1)
            let click = try event(.leftMouseDown, line: 1)
            #expect(host.hitTest(host.superview!.convert(click.locationInWindow, from: nil)) === gutter)
            let finalLine = try #require(manager.lineStorage.getLine(atIndex: 5))
            #expect(gutter.line(at: NSPoint(x: 10, y: finalLine.yPos + 5)) == nil)
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-gutter-hover-\(dark ? "dark" : "light").png"))
            gutter.mouseDown(with: try event(.leftMouseDown, line: 1))
            gutter.mouseUp(with: try event(.leftMouseUp, line: 1))
            #expect(annotations.editor?.anchor.workspaceFile?.startLine == 2)
            #expect(annotations.editor?.anchor.workspaceFile?.endLine == 2)
            try await Task.sleep(for: .milliseconds(200))
            #expect(window.firstResponder === Self.input(in: host))
            let row = try #require(manager.lineStorage.getLine(atIndex: 1))
            #expect(gutter.line(at: NSPoint(x: 10, y: row.yPos + row.height - 4)) == nil)
            // Click the Cancel label in the rendered footer (12pt padding, then
            // Save, a 12pt gap, and Cancel), through the window's event routing.
            let inlineHost = try #require(controller.textView.subviews.first { $0 is NSHostingView<AnyView> })
            let font = NSFont.systemFont(ofSize: 11)
            let saveWidth = ("Save" as NSString).size(withAttributes: [.font: font]).width
            let cancelWidth = ("Cancel" as NSString).size(withAttributes: [.font: font]).width
            let point = controller.textView.convert(NSPoint(
                x: inlineHost.frame.maxX - 12 - saveWidth - 12 - cancelWidth / 2,
                y: inlineHost.frame.maxY - 18), to: nil)
            let target = try #require(host.hitTest(host.superview!.convert(point, from: nil)))
            try #require(target === inlineHost || target.isDescendant(of: inlineHost))
            for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
                let mouse = try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [],
                    timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber,
                    context: nil, eventNumber: 0, clickCount: 1, pressure: type == .leftMouseDown ? 1 : 0))
                window.sendEvent(mouse)
            }
            try await Task.sleep(for: .milliseconds(150))
            #expect(annotations.editor == nil)
            #expect(annotations.draft == "")
            #expect(manager.blockHeights == [:])
            #expect(window.firstResponder === controller.textView)
            for (start, end) in [(0, 2), (2, 0)] {
                gutter.mouseDown(with: try event(.leftMouseDown, line: start))
                gutter.mouseDragged(with: try event(.leftMouseDragged, line: end))
                #expect(gutter.selection == 0...2)
                if start == 0 {
                    let rangeBitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
                    host.cacheDisplay(in: host.bounds, to: rangeBitmap)
                    try #require(rangeBitmap.representation(using: .png, properties: [:]))
                        .write(to: URL(fileURLWithPath: "/tmp/kit-gutter-range-\(dark ? "dark" : "light").png"))
                }
                gutter.mouseUp(with: try event(.leftMouseUp, line: end))
                #expect(annotations.editor?.anchor.workspaceFile?.startLine == 1)
                #expect(annotations.editor?.anchor.workspaceFile?.endLine == 3)
                try await Task.sleep(for: .milliseconds(150))
                #expect(manager.blockHeights[2, default: 0] > 40)
                #expect(controller.textView.string == source)
                annotations.cancelEditor()
                try await Task.sleep(for: .milliseconds(150))
            }
        }
    }

    @Test func gutterDragScrollsAndBoundsRange() async throws {
        _ = NSApplication.shared
        let file = try Self.file(String(repeating: "let value = 1\n", count: 260))
        let annotations = AnnotationState()
        let host = NSHostingView(rootView: Harness(file: file, annotations: annotations))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 460), styleMask: [.titled], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false; window.contentView = host; window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await Task.sleep(for: .milliseconds(250))
        let controller = try #require(Self.controller(in: host))
        let gutter = try #require(controller.view.subviews.compactMap { $0 as? AnnotationGutterOverlay }.first)
        func event(_ type: NSEvent.EventType, y: CGFloat) throws -> NSEvent {
            try #require(NSEvent.mouseEvent(with: type, location: gutter.convert(NSPoint(x: 10, y: y), to: nil),
                modifierFlags: [], timestamp: 0, windowNumber: window.windowNumber, context: nil,
                eventNumber: 0, clickCount: 1, pressure: 1))
        }
        gutter.mouseDown(with: try event(.leftMouseDown, y: 5))
        gutter.mouseDragged(with: try event(.leftMouseDragged, y: gutter.bounds.maxY + 12))
        try await Task.sleep(for: .milliseconds(250))
        #expect(controller.scrollView.contentView.bounds.minY > 0)
        #expect(gutter.bounds.minY > 0)
        let destination = try #require(controller.textView.layoutManager.lineStorage.getLine(atIndex: 250))
        controller.scrollView.contentView.scroll(to: CGPoint(x: 0, y: destination.yPos - 100))
        controller.scrollView.reflectScrolledClipView(controller.scrollView.contentView)
        try await Task.sleep(for: .milliseconds(150))
        let last = try #require(controller.textView.layoutManager.lineStorage.getLine(atIndex: 250))
        gutter.mouseDragged(with: try event(.leftMouseDragged, y: last.yPos + 5))
        #expect(gutter.selection == 0...199)
        gutter.mouseUp(with: try event(.leftMouseUp, y: last.yPos + 5))
        #expect(annotations.editor?.anchor.workspaceFile?.startLine == 1)
        #expect(annotations.editor?.anchor.workspaceFile?.endLine == 200)
        try await Task.sleep(for: .milliseconds(250))
        let input = try #require(Self.input(in: host))
        #expect(window.firstResponder === input)
        #expect(controller.textView.visibleRect.contains(input.convert(input.bounds, to: controller.textView)))
    }

    private struct Harness: View {
        let file: WireWorkspaceFileRead
        let annotations: AnnotationState
        @State private var position = SourceEditorState()
        var body: some View {
            // Read observable inputs here so hosted block updates participate in view invalidation.
            let _ = annotations.records
            let _ = annotations.editor
            let _ = annotations.draft
            AnnotatedFileEditor(file: file, position: $position, annotations: annotations,
                client: InlineAnnotationClient(), session: "session_test")
        }
    }
}
