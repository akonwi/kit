import AppKit
import Foundation
import SwiftUI
import Testing
@testable import Kit

/// Markdown files in file tabs (#278): which show rendered, the per-tab
/// choice of source, where their links lead, and how they're laid out.
@MainActor struct MarkdownFileTests {
    @Test func markdownFilesAreKnownByTheirExtension() {
        #expect(MarkdownFiles.isMarkdown("README.md"))
        #expect(MarkdownFiles.isMarkdown("docs/Guide.MARKDOWN"))
        #expect(MarkdownFiles.isMarkdown("~/notes.mdown"))
        #expect(!MarkdownFiles.isMarkdown("notes.txt"))
        #expect(!MarkdownFiles.isMarkdown(".md"))
        #expect(!MarkdownFiles.isMarkdown("md"))
        #expect(WorkspacePane.file("README.md").isMarkdown)
        #expect(WorkspacePane.hostFile("/srv/docs/a.md").isMarkdown)
        #expect(WorkspacePane.localFile("/Users/me/a.markdown").isMarkdown)
        #expect(!WorkspacePane.file("main.swift").isMarkdown)
        #expect(!WorkspacePane.scratchpad.isMarkdown)
    }

    @Test func aWorkspaceFilesLinksResolveAgainstItsDirectoryOnTheServer() {
        let file = WorkspacePane.file("docs/guide/a.md")
        func pane(_ link: String) -> WorkspacePane? { MarkdownFiles.pane(forLink: link, in: file, cwd: "/repo") }
        #expect(pane("b.md") == .file("docs/guide/b.md"))
        #expect(pane("./img/../c.md") == .file("docs/guide/c.md"))
        #expect(pane("../../README.md") == .file("README.md"))
        #expect(pane("../../../other/x.md") == .hostFile("/other/x.md"))
        #expect(pane("/repo/Sources/main.swift") == .file("Sources/main.swift"))
        #expect(pane("/etc/hosts") == .hostFile("/etc/hosts"))
        #expect(pane("~/notes.md") == .hostFile("~/notes.md"))
        #expect(MarkdownFiles.pane(forLink: "b.md", in: file, cwd: nil) == nil)
        #expect(MarkdownFiles.pane(forLink: "/etc/hosts", in: file, cwd: nil) == .hostFile("/etc/hosts"))
        #expect(MarkdownFiles.pane(forLink: "b.md", in: .file("a.md"), cwd: "/repo") == .file("b.md"))
    }

    @Test func aServerFilesLinksStayOnTheServer() {
        let file = WorkspacePane.hostFile("/srv/docs/a.md")
        #expect(MarkdownFiles.pane(forLink: "img/b.md", in: file, cwd: "/repo") == .hostFile("/srv/docs/img/b.md"))
        #expect(MarkdownFiles.pane(forLink: "../../repo/c.md", in: file, cwd: "/repo") == .file("c.md"))
        #expect(MarkdownFiles.pane(forLink: "/var/log/app.log", in: file, cwd: "/repo") == .hostFile("/var/log/app.log"))
        // Only the server knows its home, so its paths stay home-relative.
        let home = WorkspacePane.hostFile("~/notes/a.md")
        #expect(MarkdownFiles.pane(forLink: "../b.md", in: home, cwd: "/repo") == .hostFile("~/b.md"))
        #expect(MarkdownFiles.pane(forLink: "./c/./d.md", in: home, cwd: "/repo") == .hostFile("~/notes/c/d.md"))
        #expect(MarkdownFiles.pane(forLink: "../../x.md", in: home, cwd: "/repo") == .hostFile("~/../x.md"))
    }

    @Test func aMacFilesLinksStayOnThisMac() {
        let file = WorkspacePane.localFile("/Users/me/docs/a.md")
        func pane(_ link: String) -> WorkspacePane? { MarkdownFiles.pane(forLink: link, in: file, cwd: "/repo", home: "/Users/me") }
        #expect(pane("b.md") == .localFile("/Users/me/docs/b.md"))
        #expect(pane("../c.md") == .localFile("/Users/me/c.md"))
        #expect(pane("~/d.md") == .localFile("/Users/me/d.md"))
        #expect(pane("/repo/e.md") == .localFile("/repo/e.md"))
    }

    @Test func linkFragmentsAreDropped() {
        #expect(FilePaths.linkedPath(destination: "CONTRIBUTING.md#setup") == "CONTRIBUTING.md")
        #expect(FilePaths.linkedPath(destination: "../a.md#L3-L9") == "../a.md")
        #expect(FilePaths.linkedPath(destination: "My%23Notes.md") == "My#Notes.md")
        #expect(FilePaths.linkedPath(destination: "#usage") == nil)
    }

    @Test func clickingAFileLinkOpensItBesideTheFileInItsTile() {
        let workspace = WorkspaceState()
        workspace.open(.file("docs/a.md"))
        workspace.split(.file("docs/a.md"), .right)
        let tile = workspace.tile(containing: .file("docs/a.md"))
        workspace.focus(tile: workspace.tile(containing: .conversation)!)
        let open = OpenURLAction.markdownFile(workspace, file: .file("docs/a.md"), cwd: "/repo")
        open(FilePaths.link("../README.md")!)
        #expect(workspace.panes == [.conversation, .file("docs/a.md"), .file("README.md")])
        #expect(workspace.tile(containing: .file("README.md")) == tile)
        #expect(workspace.selected == .file("README.md"))
    }

    @Test func eachMarkdownTabOpensRenderedAndKeepsItsChoice() {
        let workspace = WorkspaceState()
        let readme = WorkspacePane.file("README.md"), guide = WorkspacePane.hostFile("/srv/guide.md")
        workspace.open(readme)
        workspace.open(guide)
        workspace.open(.file("main.swift"))
        #expect(!workspace.showsMarkdownSource(readme) && !workspace.showsMarkdownSource(guide))
        workspace.toggleMarkdownSource(guide)
        #expect(workspace.showsMarkdownSource(guide) && !workspace.showsMarkdownSource(readme))
        // Only Markdown tabs have the choice.
        workspace.toggleMarkdownSource(.file("main.swift"))
        #expect(!workspace.showsMarkdownSource(.file("main.swift")))
        workspace.close(guide)
        workspace.open(guide)
        #expect(!workspace.showsMarkdownSource(guide), "a reopened tab opens rendered")
    }

    @Test func theChoiceOfSourceSurvivesWindowRestoration() throws {
        let workspace = WorkspaceState()
        workspace.open(.file("README.md"))
        workspace.open(.localFile("/Users/me/notes.md"))
        workspace.toggleMarkdownSource(.localFile("/Users/me/notes.md"))
        let saved = workspace.arrangement
        #expect(saved.markdownSources == ["local-file:/Users/me/notes.md"])
        let restored = WorkspaceState()
        restored.restore(try JSONDecoder().decode(TileArrangement.self, from: JSONEncoder().encode(saved)))
        #expect(restored.showsMarkdownSource(.localFile("/Users/me/notes.md")))
        #expect(!restored.showsMarkdownSource(.file("README.md")))
        #expect(restored.arrangement == saved)
        // Without any, the field is left out, as layouts saved before it were.
        let rendered = WorkspaceState()
        rendered.open(.file("README.md"))
        let json = String(decoding: try JSONEncoder().encode(rendered.arrangement), as: UTF8.self)
        #expect(!json.contains("markdownSources"))
        // A saved choice for a pane that isn't open or isn't Markdown is dropped.
        let repaired = WorkspaceState()
        repaired.restore(TileArrangement(root: .tile(.init(panes: ["conversation", "file:main.swift"], selected: "conversation")),
                                         focused: 0, zoomed: false, markdownSources: ["file:main.swift", "file:gone.md"]))
        #expect(repaired.arrangement.markdownSources == nil)
    }

    @Test func filesNestedTooDeeplyOrTooLargeDoNotRender() {
        #expect(MarkdownFiles.renderable("# Title\n\n> - [x] done\n>   1. nested\n"))
        #expect(MarkdownFiles.renderable(String(repeating: ">", count: 64) + " deep"))
        #expect(!MarkdownFiles.renderable(String(repeating: ">", count: 10_000) + " deep"))
        #expect(!MarkdownFiles.renderable(String(repeating: "- ", count: 65) + "deep"))
        #expect(!MarkdownFiles.renderable(String(repeating: String(repeating: "> ", count: 60) + "x\n", count: 900)))
        #expect(!MarkdownFiles.renderable(String(repeating: "x", count: (1 << 20) + 1)))
    }

    @Test func blocksAndListItemsKnowTheirSourceLines() {
        let source = "# Title\n\nPara one\nline two\n\n- a\n- b\n  more\n\n```go\nx\n```\n\nSetext\n===\n"
        let content = MarkdownContent(source: source)
        #expect(content.lines == [1...1, 3...4, 6...8, 10...12, 14...15])
        guard case .list(let items) = content.blocks[2] else { Issue.record("not a list"); return }
        #expect(items.map(\.lines) == [6...6, 7...8])
        let rows = MarkdownDocumentRow.rows(content)
        #expect(rows.map(\.lines) == [1...1, 3...4, 6...6, 7...8, 10...12, 14...15])
        // A note sits under the row showing its last line; a line between
        // rows belongs to the row before, and one before them all to the first.
        #expect(MarkdownDocumentRow.row(of: 8, in: rows) == 3)
        #expect(MarkdownDocumentRow.row(of: 9, in: rows) == 3)
        #expect(MarkdownDocumentRow.row(of: 15, in: rows) == 5)
        #expect(MarkdownDocumentRow.row(of: 0, in: rows) == 0)
    }

    @Test func aBlockCommentAnchorsToItsSourceLines() throws {
        let state = AnnotationState()
        let annotations = MarkdownPreviewAnnotations(document: AnnotationDocument(file: try InlineAnnotationEditorTests.file("# A\n\nB\nC\n")),
                                                     state: state, client: PreviewAnnotationClient(), session: "session_test", canAnnotate: true)
        annotations.begin(3...4)
        let anchor = try #require(state.editor?.anchor.workspaceFile)
        #expect(anchor.path == "sample.swift" && anchor.fileRevision == "file_test")
        #expect(anchor.startLine == 3 && anchor.endLine == 4)
    }

    /// A saved comment sits under the block holding its last line, and a new
    /// one's editor under the block it comments on. Set KIT_SNAPSHOT_DIR to
    /// write the preview there for review.
    @Test func thePreviewShowsCommentsUnderTheirBlocks() async throws {
        let source = "# Plan\n\nShip it.\n\n- one\n- two\n"
        let file = try InlineAnnotationEditorTests.file(source)
        let state = AnnotationState()
        let note = try FileAnnotation(id: 1, anchor: .init(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "sample.swift", fileRevision: "file_test", startLine: 3, endLine: 3), workingTreeDiff: nil),
            body: "Why now?", source: "Ship it.", stale: false, complete: true)
        state.observe([note])
        let annotations = MarkdownPreviewAnnotations(document: AnnotationDocument(file: file), state: state,
                                                     client: PreviewAnnotationClient(), session: "session_test", canAnnotate: true)
        annotations.begin(5...5)
        let view = MarkdownDocumentView(source: source, annotations: annotations)
        #expect(view.entries(MarkdownDocumentRow.rows(MarkdownContent(source: source))).map(\.id)
                == ["row-0", "row-1", "note-1", "row-2", "editor", "row-3"])
        guard let directory = ProcessInfo.processInfo.environment["KIT_SNAPSHOT_DIR"] else { return }
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let host = NSHostingView(rootView: view
            .environment(\.mica, theme).environment(\.colorScheme, .dark).background(theme.surface))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 720, height: 520), styleMask: [.borderless],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await Task.sleep(for: .milliseconds(300))
        host.layoutSubtreeIfNeeded()
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        try #require(bitmap.representation(using: .png, properties: [:]))
            .write(to: URL(fileURLWithPath: directory).appendingPathComponent("markdown-preview-comments.png"))
    }

    @Test func aDocumentIsLaidOutABlockOrListItemPerRow() {
        let rows = MarkdownDocumentRow.rows(MarkdownContent(source: "# Title\n\nIntro.\n\n- one\n- two\n- three\n\n> quoted\n"))
        #expect(rows.map(\.gap) == [0, 10, 10, 8, 8, 10])
        #expect(rows.map(\.id) == [0, 1, 2, 3, 4, 5])
        guard case .block(.heading(1, let title)) = rows[0].content, case .item(let item) = rows[3].content,
              case .block(.quote) = rows[5].content else {
            Issue.record("unexpected rows \(rows)"); return
        }
        #expect(String(title.characters) == "Title")
        #expect(item.marker == "•")
    }

    /// A long file renders in a scroll view that builds rows as they're
    /// shown: a 4,000-item changelog parses, lays out, and scrolls. Set
    /// KIT_SNAPSHOT_DIR to write its first screen there for review.
    @Test func aLongDocumentRendersInAScrollingColumn() async throws {
        let preamble = """
        # Changelog

        Every change, **newest first**. See [the guide](docs/guide.md) and `CONTRIBUTING.md`.

        | Version | Date |
        | --- | ---: |
        | 0.50 | 2026-10-26 |

        ```mermaid
        flowchart LR
          A[Change] --> B[Release]
        ```

        ```swift
        let kit = Kit()
        ```

        > Older changes are archived.


        """
        let source = preamble + (1...4_000).map { "- Change \($0) with `code` and a [link](https://e.co)" }.joined(separator: "\n")
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let host = NSHostingView(rootView: MarkdownDocumentView(source: source)
            .environment(\.mica, theme).environment(\.colorScheme, .dark).background(theme.surface))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1200, height: 400), styleMask: [.borderless],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        var scroll: NSScrollView?
        for _ in 0..<1_000 {
            host.layoutSubtreeIfNeeded()
            scroll = Self.scrollView(in: host)
            if let height = scroll?.documentView?.frame.height, height > 10_000 { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        let view = try #require(scroll)
        let document = try #require(view.documentView)
        #expect(document.frame.height > 10_000, "every row is in the scrolled extent")
        document.scroll(NSPoint(x: 0, y: document.frame.height / 2))
        host.layoutSubtreeIfNeeded()
        #expect(view.contentView.bounds.minY > 1_000)
        if let directory = ProcessInfo.processInfo.environment["KIT_SNAPSHOT_DIR"] {
            document.scroll(.zero)
            host.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(100))
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: directory).appendingPathComponent("markdown-file.png"))
        }
    }

    private static func scrollView(in view: NSView) -> NSScrollView? {
        if let scroll = view as? NSScrollView { return scroll }
        for child in view.subviews { if let found = scrollView(in: child) { return found } }
        return nil
    }
}

private struct PreviewAnnotationClient: AnnotationClient {
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
