import AppKit
@preconcurrency import CodeEditSourceEditor
import Foundation
import SwiftUI
import Testing
@testable import Kit

enum DiffTestData {
    static let observation = #"{"sessionId":"session_test","target":{"id":"difftarget_test","workspaceId":"workspace_test","kind":"working_tree","repositoryPath":"/tmp","base":{"kind":"commit","oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"head":{"kind":"working_tree"}},"revision":"diffrev_test","head":{"state":"commit","oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"indexSummary":"index_test","complete":true,"omissions":[]}"#
    static let file = #"{"path":"main.swift","fileRevision":"diff_file_test","change":"modified","old":{"kind":"regular","mode":33188},"new":{"kind":"regular","mode":33188},"contentState":"text","additions":1,"deletions":1}"#
    static let hunk = #"{"oldStart":1,"oldCount":3,"newStart":1,"newCount":3,"continuedBefore":false,"continuedAfter":false,"lines":[{"kind":"context","oldLine":1,"newLine":1,"content":"func greet() {","hasTerminatingLF":true},{"kind":"deletion","oldLine":2,"content":"    print(\"Hello\")","hasTerminatingLF":true},{"kind":"addition","newLine":2,"content":"    print(\"Bonjour 🌿\")","hasTerminatingLF":true},{"kind":"context","oldLine":3,"newLine":3,"content":"}","hasTerminatingLF":true}]}"#
    static var pageJSON: String { "{\"observation\":\(observation),\"file\":\(file),\"computation\":{\"state\":\"complete\"},\"hunks\":[\(hunk)]}" }
    static func page() throws -> WireFileDiffPage { try JSONDecoder().decode(WireFileDiffPage.self, from: Data(pageJSON.utf8)) }
    static func document() throws -> DiffDocument {
        let page = try page(); return DiffDocument(observation: page.observation, file: page.file, hunks: page.hunks!)
    }
    static func catalog() throws -> WireDiffTargetCatalog {
        try JSONDecoder().decode(WireDiffTargetCatalog.self, from: Data(#"{"sessionId":"session_test","workspaceId":"workspace_test","targets":[{"reference":"difftargetref_test.opaque","targetId":"difftarget_test","kind":"working_tree","base":{"kind":"commit","oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"head":{"kind":"working_tree"},"metadata":{"label":"Working tree"}}],"diagnostics":[]}"#.utf8))
    }
}

actor DiffTestClient: DiffClient, AnnotationClient {
    let serverID = "test"
    let isDemo = false
    var reads: [WireReadFileDiffInput] = []
    var observations: [WireObserveDiffInput] = []
    var longLines = false
    func setLongLines() { longLines = true }
    var multipleFiles = false
    func setMultipleFiles() { multipleFiles = true }
    var revision = "diffrev_test"
    func advanceRevision() { revision = "diffrev_updated" }
    func page(path: String = "main.swift") throws -> WireFileDiffPage {
        try JSONDecoder().decode(WireFileDiffPage.self, from: Data(DiffTestData.pageJSON.replacingOccurrences(of: "diffrev_test", with: revision).replacingOccurrences(of: "main.swift", with: path).replacingOccurrences(of: "Bonjour 🌿", with: longLines ? String(repeating: "Long greeting ", count: 18) : "Bonjour 🌿").utf8))
    }
    var repeatCursor = false
    var slow = false
    func setRepeatCursor() { repeatCursor = true }
    func setSlow() { slow = true }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func annotations(_ session: String) async throws -> [FileAnnotation] { [] }
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(503) }
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(503) }
    func deleteAnnotation(_ session: String, id: UInt64) async throws {}
    func diffTargets(_ session: String) async throws -> WireDiffTargetCatalog { try DiffTestData.catalog() }
    func observeDiff(_ session: String, input: WireObserveDiffInput) async throws -> WireWorkingTreePage {
        observations.append(input)
        let page = try page()
        return .init(observation: page.observation, files: input.cursor == nil ? (multipleFiles ? [page.file, try self.page(path: "greeting.swift").file] : [page.file]) : [], nextCursor: input.cursor == nil ? "files-next" : nil)
    }
    func readDiff(_ session: String, input: WireReadFileDiffInput) async throws -> WireFileDiffPage {
        reads.append(input)
        if slow { try await Task.sleep(for: .milliseconds(150)) }
        let page = try page(path: input.path)
        return .init(observation: page.observation, file: page.file, computation: page.computation,
            hunks: page.hunks, nextCursor: repeatCursor ? "repeated" : nil)
    }
}

struct DiffTests {
    @Test func semanticRowsKeepOldAndNewCoordinates() throws {
        let document = try DiffTestData.document()
        #expect(document.rows.map(\.kind) == ["hunk", "context", "deletion", "addition", "context"])
        #expect(document.rows.map(\.oldLine) == [nil, 1, 2, nil, 3])
        #expect(document.rows.map(\.newLine) == [nil, 1, nil, 2, 3])
        let old = try #require(document.anchor(rows: 2...2, side: "old")?.workingTreeDiff)
        #expect(old.side == "old" && old.startLine == 2 && old.endLine == 2)
        #expect(old.targetRevision == "diffrev_test" && old.fileRevision == "diff_file_test")
        let new = try #require(document.anchor(rows: 1...4, side: "new"))
        #expect(new.workingTreeDiff?.startLine == 1 && new.workingTreeDiff?.endLine == 3)
        #expect(document.range(new) == 1...4)
        #expect(document.anchor(rows: 0...4, side: "new") == nil)
        #expect(document.anchor(rows: 2...2, side: "new") == nil)
    }

    @MainActor @Test func continuationPinsRevisionAndRejectsRepeatedCursor() async throws {
        let client = DiffTestClient(), model = DiffState()
        await client.setRepeatCursor()
        await model.refresh(client: client, session: "session_test")
        #expect(model.document?.file.path == "main.swift")
        #expect(model.lineCursor == "repeated")
        let file = try #require(model.document?.file)
        await model.read(file, client: client, session: "session_test", more: true)
        #expect(model.stale)
        #expect(model.document?.rows.count == 5)
        let reads = await client.reads
        #expect(reads.last?.cursor == "repeated")
        #expect(reads.last?.targetRevision == "diffrev_test")
        #expect(reads.last?.expectedFileRevision == "diff_file_test")
        let target = try #require(model.target)
        await model.select(target, client: client, session: "session_test", more: true)
        #expect(await client.observations.last?.expectedTargetRevision == "diffrev_test")
        #expect(await client.observations.last?.cursor == "files-next")
    }

    @MainActor @Test func invalidatedReadCannotOverwriteNewWorkspace() async throws {
        let client = DiffTestClient(), model = DiffState()
        await client.setSlow()
        let task = Task { await model.refresh(client: client, session: "session_test") }
        try await Task.sleep(for: .milliseconds(50))
        model.invalidate()
        await task.value
        #expect(model.catalog == nil && model.document == nil)
        #expect(model.loading == false)
    }

    @MainActor @Test func pollingPreservesExpandedPagesAndDefersDuringEditing() async throws {
        let client = DiffTestClient(), model = DiffState()
        await model.refresh(client: client, session: "session_test")
        await model.select(try #require(model.target), client: client, session: "session_test", more: true)
        #expect(model.fileCursor == nil)
        await model.poll(client: client, session: "session_test", canApply: { true })
        #expect(model.fileCursor == nil)
        #expect(model.document?.rows.count == 5)
        await client.advanceRevision()
        await model.poll(client: client, session: "session_test", canApply: { true })
        #expect(model.document?.observation.revision == "diffrev_updated")
        #expect(model.fileCursor == "files-next")
        let reads = await client.reads.count
        await model.poll(client: client, session: "session_test", canApply: { false })
        #expect(await client.reads.count == reads)
        await client.setSlow()
        var canApply = true
        let task = Task { await model.poll(client: client, session: "session_test", canApply: { canApply }) }
        try await Task.sleep(for: .milliseconds(50))
        canApply = false
        model.error = "Draft started"
        await task.value
        #expect(model.error == "Draft started")
    }

    @Test func splitRowsPairReplacementsAndKeepSideSpecificAnchors() throws {
        let document = try DiffTestData.document(), pair = document.split()
        #expect(pair.old.rows.map(\.kind) == ["hunk", "context", "deletion", "context"])
        #expect(pair.new.rows.map(\.kind) == ["hunk", "context", "addition", "context"])
        #expect(pair.old.rows.map(\.oldLine) == [nil, 1, 2, 3])
        #expect(pair.new.rows.map(\.newLine) == [nil, 1, 2, 3])
        let old = try #require(pair.old.anchor(rows: 1...2, side: "old"))
        let new = try #require(pair.new.anchor(rows: 2...3, side: "new"))
        #expect(old.workingTreeDiff?.startLine == 1 && old.workingTreeDiff?.endLine == 2)
        #expect(new.workingTreeDiff?.startLine == 2 && new.workingTreeDiff?.endLine == 3)
        #expect(pair.new.range(old) == nil && pair.old.range(new) == nil)
        #expect(pair.old.range(old) == 1...2 && pair.new.range(new) == 2...3)
        let addedOnly = DiffTestData.hunk.replacingOccurrences(of: #"{"kind":"deletion","oldLine":2,"content":"    print(\"Hello\")","hasTerminatingLF":true},"#, with: "")
        let hunk = try JSONDecoder().decode(WireDiffHunk.self, from: Data(addedOnly.utf8))
        let uneven = DiffDocument(observation: document.observation, file: document.file, hunks: [hunk]).split()
        #expect(uneven.old.rows[2].kind == "placeholder")
        #expect(uneven.old.rows[2].oldLine == nil)
        #expect(uneven.new.rows[2].newLine == 2)
        #expect(uneven.old.rows.count == uneven.new.rows.count)
    }

    @MainActor @Test func continuousFilesRetainIndependentDocumentsAndReveal() async throws {
        let client = DiffTestClient(), model = DiffState()
        await client.setMultipleFiles()
        await model.refresh(client: client, session: "session_test")
        for file in model.files { await model.loadFile(file, client: client, session: "session_test") }
        let second = try #require(model.fileStates["greeting.swift"]?.document)
        #expect(model.fileStates["main.swift"]?.document?.file.path == "main.swift")
        let note = try FileAnnotation(id: 7, anchor: #require(second.anchor(rows: 3...3, side: "new")), body: "Greeting", source: "source", stale: false, complete: true)
        #expect(await model.openAnnotation(note, client: client, session: "session_test"))
        #expect(model.files.map(\.path) == ["main.swift", "greeting.swift"])
        #expect(model.fileStates["main.swift"]?.document?.file.path == "main.swift")
        #expect(model.fileStates["greeting.swift"]?.document?.range(note.anchor) == 3...3)
        model.invalidate()
        #expect(model.fileStates.isEmpty)
    }

    @MainActor @Test func chipRevealUsesAnnotationAuthority() async throws {
        let client = DiffTestClient(), model = DiffState()
        let document = try DiffTestData.document()
        let note = try FileAnnotation(id: 7, anchor: #require(document.anchor(rows: 2...2, side: "old")), body: "Keep this", source: "old source", stale: false, complete: true)
        #expect(await model.openAnnotation(note, client: client, session: "session_test"))
        #expect(await client.reads.last?.annotationId == 7)
        #expect(model.document?.range(note.anchor) == 2...2)
    }
}

extension InlineAnnotationEditorTests {
    @Test func nativeDiffGutterCreatesSidePinnedRangeAndRendersInlineNote() async throws {
        _ = NSApplication.shared
        let document = try DiffTestData.document()
        for dark in [false, true] {
            let annotations = AnnotationState()
            let old = try #require(document.anchor(rows: 2...2, side: "old"))
            let note = try FileAnnotation(id: 1, anchor: old, body: "Keep the original greeting available.", source: "    print(\"Hello\")", stale: false, complete: true)
            annotations.observe([note])
            let host = NSHostingView(rootView: DiffHarness(document: document, annotations: annotations)
                .environment(\.mica, MicaTheme(dark: dark)).environment(\.colorScheme, dark ? .dark : .light))
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 860, height: 460), styleMask: [.titled, .closable], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false; window.contentView = host; window.makeKeyAndOrderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(650))
            let controller = try #require(InlineAnnotationEditorTests.controller(in: host))
            let gutter = try #require(controller.view.subviews.compactMap { $0 as? AnnotationGutterOverlay }.first)
            let manager = controller.textView.layoutManager!
            #expect(controller.textView.string == document.content)
            #expect(manager.edgeInsets.left == 108)
            #expect(manager.blockHeights[2, default: 0] > 40)
            #expect(gutter.diffRows?.map(\.newLine) == [nil, 1, nil, 2, 3])
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:])).write(to: URL(fileURLWithPath: "/tmp/kit-native-diff-\(dark ? "dark" : "light").png"))
            func event(_ type: NSEvent.EventType, row: Int, x: CGFloat = 40) throws -> NSEvent {
                let line = try #require(manager.lineStorage.getLine(atIndex: row))
                let point = gutter.convert(NSPoint(x: x, y: line.yPos + 5), to: nil)
                return try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [], timestamp: 0,
                    windowNumber: window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
            }
            let button = try #require(gutter.buttonRect(for: 1))
            #expect(button.maxX == gutter.railWidth)
            gutter.mouseMoved(with: try event(.mouseMoved, row: 1, x: button.midX))
            #expect(NSCursor.current == NSCursor.pointingHand)
            let down = try event(.leftMouseDown, row: 1)
            #expect(host.hitTest(host.superview!.convert(down.locationInWindow, from: nil)) === gutter)
            gutter.mouseDown(with: down)
            gutter.mouseDragged(with: try event(.leftMouseDragged, row: 2))
            gutter.mouseUp(with: try event(.leftMouseUp, row: 2))
            let anchor = try #require(annotations.editor?.anchor.workingTreeDiff)
            #expect(anchor.side == "old" && anchor.startLine == 1 && anchor.endLine == 2)
            #expect(anchor.targetId == "difftarget_test" && anchor.targetRevision == "diffrev_test")
            _ = try await Self.waitForFocusedInput(in: host, window: window)
            #expect(manager.blockHeights[2, default: 0] > 100)
        }
    }

    @Test func diffPaneShowsServerFileAndTargetInBothAppearances() async throws {
        _ = NSApplication.shared
        for dark in [false, true] {
            let client = DiffTestClient()
            await client.setMultipleFiles()
            let state = SessionStore(fixture: Fixture(sessions: []), client: client, sessionID: "session_test")
            state.ui.workspace.open(.review)
            await state.ui.workspace.diff.refresh(client: client, session: "session_test")
            let host = NSHostingView(rootView: ReviewPane(state: state)
                .background(MicaTheme(dark: dark).surface)
                .environment(\.mica, MicaTheme(dark: dark)).environment(\.colorScheme, dark ? .dark : .light))
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 560), styleMask: [.titled, .closable], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false; window.contentView = host; window.makeKeyAndOrderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(650))
            #expect(state.ui.workspace.diff.target?.metadata.label == "Working tree")
            #expect(state.ui.workspace.diff.selectedPath == "main.swift")
            let controller = try #require(Self.controller(in: host))
            #expect(controller.textView.string.contains("Bonjour 🌿"))
            #expect(state.ui.workspace.diff.fileStates["greeting.swift"]?.document?.file.path == "greeting.swift")
            func editors(in view: NSView) -> [TextViewController] {
                if let editor = view.nextResponder as? TextViewController { return [editor] }
                return view.subviews.flatMap { editors(in: $0) }
            }
            let rendered = editors(in: host)
            #expect(rendered.count == 2)
            for editor in rendered {
                #expect(editor.forwardsVerticalScrollToParent)
                let gutter = try #require(editor.view.subviews.compactMap { $0 as? AnnotationGutterOverlay }.first)
                #expect(gutter.frame.height > 70)
                #expect(gutter.bounds.minY < 10)
                #expect(gutter.diffRows?.count == 5)

                #expect(abs(editor.view.frame.height - editor.textView.layoutManager.estimatedHeight() - 8) < 2)
            }

            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:])).write(to: URL(fileURLWithPath: "/tmp/kit-diff-pane-\(dark ? "dark" : "light").png"))
            // The outer hosting scroll view can draw with a negative dirty origin.
            // Both file sections must still paint their semantic diff backgrounds.
            for editor in rendered {
                func color(row: Int) throws -> NSColor {
                    let line = try #require(editor.textView.layoutManager.lineStorage.getLine(atIndex: row))
                    let point = host.convert(NSPoint(x: 600, y: line.yPos + 5), from: editor.textView)
                    let y = host.isFlipped ? point.y : host.bounds.height - point.y
                    return try #require(bitmap.colorAt(x: Int(point.x * CGFloat(bitmap.pixelsWide) / host.bounds.width),
                        y: Int(y * CGFloat(bitmap.pixelsHigh) / host.bounds.height))?.usingColorSpace(.deviceRGB))
                }
                let deletion = try color(row: 2), addition = try color(row: 3)
                #expect(deletion.redComponent > deletion.greenComponent)
                #expect(addition.greenComponent > addition.redComponent)
            }
            let second = try #require(rendered.last)
            let gutter = try #require(second.view.subviews.compactMap { $0 as? AnnotationGutterOverlay }.first)
            let button = try #require(gutter.buttonRect(for: 3))
            let point = gutter.convert(NSPoint(x: button.midX, y: button.midY), to: nil)
            #expect(host.hitTest(host.superview!.convert(point, from: nil)) === gutter)
            func click(_ type: NSEvent.EventType) throws -> NSEvent {
                try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [], timestamp: 0,
                    windowNumber: window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
            }
            gutter.mouseDown(with: try click(.leftMouseDown))
            gutter.mouseUp(with: try click(.leftMouseUp))
            try await Task.sleep(for: .milliseconds(250))
            #expect(state.annotationState.editor?.anchor.workingTreeDiff?.path == "greeting.swift")
            #expect(second.view.frame.height > 180)
            #expect(abs(second.view.frame.height - second.textView.layoutManager.estimatedHeight() - 8) < 2)
            #expect(Self.input(in: host) != nil)
            state.annotationState.cancelEditor()
            window.setContentSize(NSSize(width: 900, height: 280))
            try await Task.sleep(for: .milliseconds(250))
            let outer = try #require(controller.scrollView.enclosingScrollView)
            outer.contentView.scroll(to: .zero)
            let cgEvent = try #require(CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 1, wheel1: -100, wheel2: 0, wheel3: 0))
            controller.scrollView.scrollWheel(with: try #require(NSEvent(cgEvent: cgEvent)))
            try await Task.sleep(for: .milliseconds(200))
            #expect(outer.contentView.bounds.minY > 0)
        }
    }

    @Test func splitAndWrappedDiffsAlignRowsAndPreserveDraftAcrossModes() async throws {
        for dark in [false, true] {
            let client = DiffTestClient()
            await client.setLongLines()
            let state = SessionStore(fixture: Fixture(sessions: []), client: client, sessionID: "session_test")
            state.ui.workspace.open(.review)
            let model = state.ui.workspace.diff
            await model.refresh(client: client, session: "session_test")
            let host = NSHostingView(rootView: ReviewPane(state: state).background(MicaTheme(dark: dark).surface)
                .environment(\.mica, MicaTheme(dark: dark)).environment(\.colorScheme, dark ? .dark : .light))
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1000, height: 650), styleMask: [.titled, .closable], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false; window.contentView = host; window.makeKeyAndOrderFront(nil)
            defer { window.close() }
            func editors(in view: NSView) -> [TextViewController] {
                if let editor = view.nextResponder as? TextViewController { return [editor] }
                return view.subviews.flatMap { editors(in: $0) }
            }
            try await Task.sleep(for: .milliseconds(500))
            let unified = try #require(Self.controller(in: host))
            let source = unified.textView.string
            let singleHeight = try #require(unified.textView.layoutManager.lineStorage.getLine(atIndex: 3)).height
            model.wrapLines = true
            try await Task.sleep(for: .milliseconds(400))
            #expect(unified.configuration.appearance.wrapLines)
            #expect(unified.textView.string == source)
            #expect(try #require(unified.textView.layoutManager.lineStorage.getLine(atIndex: 3)).height > singleHeight)
            model.layout = .split
            try await Task.sleep(for: .milliseconds(650))
            let split = editors(in: host)
            #expect(split.count == 2)
            let old = try #require(split.first), new = try #require(split.last)
            func gutter(_ editor: TextViewController) throws -> AnnotationGutterOverlay {
                try #require(editor.view.subviews.compactMap { $0 as? AnnotationGutterOverlay }.first)
            }
            #expect(try gutter(old).fixedSide == "old")
            #expect(try gutter(new).fixedSide == "new")
            func assertAligned() throws {
                for index in 0..<4 {
                    let a = try #require(old.textView.layoutManager.lineStorage.getLine(atIndex: index))
                    let b = try #require(new.textView.layoutManager.lineStorage.getLine(atIndex: index))
                    #expect(abs(a.yPos - b.yPos) < 1)
                    #expect(abs(a.height - b.height) < 1)
                }
            }
            try assertAligned()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:])).write(to: URL(fileURLWithPath: "/tmp/kit-diff-split-\(dark ? "dark" : "light").png"))
            let oldGutter = try gutter(old), button = try #require(try gutter(old).buttonRect(for: 2))
            let point = oldGutter.convert(NSPoint(x: button.midX, y: button.midY), to: nil)
            for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
                let event = try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [], timestamp: 0,
                    windowNumber: window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
                if type == .leftMouseDown { oldGutter.mouseDown(with: event) } else { oldGutter.mouseUp(with: event) }
            }
            state.annotationState.draft = "Keep this comment while changing layout."
            let anchor = try #require(state.annotationState.editor?.anchor.workingTreeDiff)
            #expect(anchor.side == "old" && anchor.startLine == 2)
            try await Task.sleep(for: .milliseconds(400))
            try assertAligned()
            #expect(old.textView.layoutManager.blockHeights[2, default: 0] > 60)
            model.layout = .unified
            model.wrapLines = false
            try await Task.sleep(for: .milliseconds(450))
            #expect(editors(in: host).count == 1)
            #expect(state.annotationState.draft == "Keep this comment while changing layout.")
            #expect(state.annotationState.editor?.anchor.workingTreeDiff?.side == "old")
            #expect(Self.input(in: host)?.string == state.annotationState.draft)
        }
    }

    private struct DiffHarness: View {
        let document: DiffDocument
        let annotations: AnnotationState
        @State private var position = SourceEditorState()
        var body: some View {
            AnnotatedFileEditor(diff: document, position: $position, annotations: annotations,
                client: DiffTestClient(), session: "session_test", canAnnotate: true)
        }
    }
}

extension DiffTests {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_DIFF_LIVE_TEST"] == "1"))
    func disposableRepositoryDiffTargetsAndAnnotations() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("kit-diff-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        func git(_ args: [String]) throws {
            let process = Process(); process.executableURL = URL(fileURLWithPath: "/usr/bin/git")
            process.arguments = ["-C", directory.path] + args
            process.standardOutput = FileHandle.nullDevice; process.standardError = FileHandle.nullDevice
            try process.run(); process.waitUntilExit()
            try #require(process.terminationStatus == 0)
        }
        try git(["init", "-b", "main"])
        try git(["config", "user.email", "kit-test@example.invalid"])
        try git(["config", "user.name", "Kit Test"])
        let file = directory.appendingPathComponent("main.swift")
        try "let value = 1\n".write(to: file, atomically: true, encoding: .utf8)
        try git(["add", "main.swift"]); try git(["commit", "-m", "Initial"])
        try git(["checkout", "-b", "feature"])
        try "let value = 2\n".write(to: file, atomically: true, encoding: .utf8)
        try git(["commit", "-am", "Change"])
        try "let value = 3\n".write(to: file, atomically: true, encoding: .utf8)
        let client = try await HTTPClient.local()
        let model = try #require(try await client.models().first)
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        _ = try await client.createSession(.init(id: id, cwd: directory.path, name: "Diff verification", model: model.id,
            thinkingLevel: model.thinkingLevels?.first?.rawValue ?? "off", temporary: true))
        do {
            let catalog = try await client.diffTargets(id)
            #expect(Set((catalog.targets ?? []).map(\.kind)) == ["working_tree", "branch", "commit"])
            for kind in ["working_tree", "branch", "commit"] {
                let target = try #require(catalog.targets?.first { $0.kind == kind })
                let page = try await client.observeDiff(id, input: .init(workspaceId: catalog.workspaceId,
                    targetReference: target.reference, expectedTargetId: target.targetId, expectedTargetRevision: nil, pageSize: 100, cursor: nil))
                let changed = try #require(page.files?.first { $0.path == "main.swift" })
                var diff = try await client.readDiff(id, input: .init(targetId: page.observation.target.id,
                    targetRevision: page.observation.revision, path: changed.path, expectedFileRevision: changed.fileRevision,
                    annotationId: nil, pageSize: 1, maxHunks: 1, cursor: nil))
                var hunks = diff.hunks ?? []
                for _ in 0..<10 {
                    guard let cursor = diff.nextCursor, !cursor.isEmpty else { break }
                    diff = try await client.readDiff(id, input: .init(targetId: page.observation.target.id,
                        targetRevision: page.observation.revision, path: changed.path, expectedFileRevision: changed.fileRevision,
                        annotationId: nil, pageSize: 1, maxHunks: 1, cursor: cursor))
                    hunks += diff.hunks ?? []
                }
                #expect(diff.nextCursor == nil || diff.nextCursor == "")
                let document = DiffDocument(observation: diff.observation, file: diff.file, hunks: hunks)
                let row = try #require(document.rows.firstIndex { $0.kind == "addition" })
                let anchor = try #require(document.anchor(rows: row...row, side: "new"))
                let note = try await client.createAnnotation(id, input: .init(anchor: anchor, body: "Review this revision."))
                #expect(note.diffAnchor?.targetRevision == page.observation.revision)
                #expect(note.source == document.rows[row].text)
                #expect(note.diffTarget?.kind == (kind == "working_tree" ? nil : kind))
                try await client.deleteAnnotation(id, id: note.id)
                if kind == "working_tree" {
                    try "let value = 4\n".write(to: file, atomically: true, encoding: .utf8)
                    await #expect(throws: DiffReadError.self) {
                        try await client.readDiff(id, input: .init(targetId: page.observation.target.id,
                            targetRevision: page.observation.revision, path: changed.path, expectedFileRevision: changed.fileRevision,
                            annotationId: nil, pageSize: 500, maxHunks: 10, cursor: nil))
                    }
                }
            }
            try await client.disposeSession(id)
        } catch {
            try? await client.disposeSession(id)
            throw error
        }
    }
}
