import KitClient
import KitTranscript
import SwiftUI

/// A file in the session's project, read-only, with the composer below so a
/// follow-up can go straight back to the agent. Code shows by line, where a
/// tap on a line number starts a comment; a Markdown file shows rendered,
/// with its source one tap away.
struct FileScreen: View {
    let path: String
    /// The lines to bring into view and mark, counted from 1.
    var lines: FileLineRange?
    let client: ServerClient
    let feed: SessionFeed
    let composer: SessionComposer
    let actions: SessionActions
    let interactions: SessionInteractions
    @State private var reader: FileReader
    @State private var colored: (revision: String, lines: [AttributedString])?
    @State private var source: Bool
    /// The lines brought into view and marked: those opened at, or a
    /// comment's whose chip was tapped.
    @State private var focus: ClosedRange<Int>?
    /// Opens a file a link in a Markdown preview names, at its lines.
    var openLink: (String, FileLineRange?) -> Void = { _, _ in }
    @AppStorage("fileWrapLines") private var wrapLines = false
    @Environment(\.scenePhase) private var scenePhase

    init(path: String, lines: FileLineRange? = nil, client: ServerClient, feed: SessionFeed, composer: SessionComposer,
         actions: SessionActions, interactions: SessionInteractions, openLink: @escaping (String, FileLineRange?) -> Void = { _, _ in }) {
        self.path = path
        self.lines = lines
        self.openLink = openLink
        self.client = client
        self.feed = feed
        self.composer = composer
        self.actions = actions
        self.interactions = interactions
        _reader = State(initialValue: FileReader(path: path))
        // Comments sit on lines, so a Markdown file opened at some shows its source.
        _source = State(initialValue: lines != nil)
        _focus = State(initialValue: lines.map { $0.start...$0.end })
    }

    private var session: String { feed.identity.session }
    private var markdown: Bool { MarkdownFilePaths.isMarkdown(path) }
    private var annotations: AnnotationState { composer.annotations }

    var body: some View {
        content
            .background(Theme.surface)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(.hidden, for: .tabBar)
            .toolbar {
                ToolbarItem(placement: .principal) { header }
                    .sharedBackgroundVisibility(.hidden)
                if markdown {
                    ToolbarItem(placement: .topBarTrailing) { PreviewSourceSwitch(source: $source) }
                        .sharedBackgroundVisibility(.hidden)
                } else {
                    ToolbarItem(placement: .topBarTrailing) { viewMenu }
                }
            }
            .safeAreaBar(edge: .bottom, spacing: 0) {
                // The comment box takes the keyboard while it's open.
                if !feed.unavailable && annotations.editor == nil {
                    ComposerBar(composer: composer, actions: actions, interactions: interactions, feed: feed,
                                revealComment: reveal, revealable: { !$0.isDiff && $0.path == path }) { feed.reconnect() }
                }
            }
            .task { await load() }
            // Whether comments' lines changed is known when they're read.
            .task(id: reader.file?.revision) {
                guard reader.file != nil else { return }
                try? await annotations.refresh(client: client, session: session)
            }
            // Read again when a turn ends, since the agent may have changed it.
            .onChange(of: feed.session?.activeRunID == nil) { _, idle in
                if idle { Task { await load() } }
            }
            .task(id: scenePhase == .active) {
                guard scenePhase == .active else { return }
                await feed.view()
            }
            .task(id: reader.file?.revision) {
                guard let file = reader.file, colored?.revision != file.revision,
                      let language = DiffHighlighting.language(path) else { return }
                let lines = reader.lines
                guard let spans = await CodeColoring.spans(for: .init(source: lines.joined(separator: "\n"), language: language)),
                      !Task.isCancelled else { return }
                colored = (file.revision, CodeColoring.lines(lines, spans: spans))
            }
    }

    private func load() async {
        await reader.load(client: client, session: session, cwd: feed.session?.cwd)
    }

    /// Brings a comment on this file into view, in the source.
    private func reveal(_ note: FileAnnotation) {
        source = true
        focus = note.startLine...note.startLine
    }

    // MARK: Header

    private var header: some View {
        let parts = ChangesFormat.split(path)
        return VStack(alignment: .leading, spacing: 1) {
            Text(parts.name)
                .font(.system(size: 16, weight: .semibold, design: .monospaced))
                .foregroundStyle(Theme.text)
                .lineLimit(1)
                .truncationMode(.middle)
            if !parts.folder.isEmpty {
                Text(String(parts.folder.dropLast()))
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                    .truncationMode(.head)
            }
        }
        .frame(minWidth: 0, idealWidth: 10_000, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
        .accessibilityLabel(path)
    }

    private var viewMenu: some View {
        Menu {
            Toggle("Wrap lines", systemImage: "text.word.spacing", isOn: $wrapLines)
            Button("Refresh", systemImage: "arrow.clockwise") { Task { await load() } }
                .disabled(reader.loading)
        } label: {
            Image(systemName: "ellipsis")
                .foregroundStyle(Theme.text)
                .accessibilityLabel("File options")
        }
        .accessibilityIdentifier("file-menu")
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if let file = reader.file {
            VStack(spacing: 0) {
                if let error = reader.error {
                    FileNote(text: "Couldn’t refresh. \(error)", warning: true)
                }
                if file.content.isEmpty {
                    ContentUnavailableView("This file is empty", systemImage: "doc")
                } else if markdown && !source {
                    ScrollView {
                        VStack(alignment: .leading, spacing: 12) {
                            MarkdownBlocks(source: file.content)
                                .environment(\.markdownDocument, true)
                                .environment(\.openURL, OpenURLAction { url in
                                    guard FilePaths.path(fromLink: url) != nil else { return .systemAction }
                                    // A relative link starts from this file's folder.
                                    let folder = (path as NSString).deletingLastPathComponent
                                    guard let file = OpenedFile.linked(url, cwd: feed.session?.cwd, folder: folder) else { return .discarded }
                                    openLink(file.path, file.lines)
                                    return .handled
                                })
                            if let notice = reader.truncationNotice { FileNote(text: notice).padding(.horizontal, -20) }
                        }
                        .padding(.horizontal, 20)
                        .padding(.top, 12)
                        .padding(.bottom, 16)
                    }
                    .refreshable { await load() }
                    .accessibilityIdentifier("file-preview")
                } else {
                    ScrollViewReader { proxy in
                        CommentableFile(file: file, lines: reader.lines,
                                        colored: colored?.revision == file.revision ? colored?.lines : nil,
                                        wrap: wrapLines || markdown, marked: focus, annotations: annotations) { anchor, editor in
                            Task {
                                await annotations.save(client: client, session: session, anchor: anchor,
                                                       editing: editor.editing, replacing: editor.replacing)
                            }
                        } remove: { note in
                            Task { await annotations.remove(client: client, session: session, id: note.id) }
                        } complete: { note in
                            if note.complete { return note }
                            do { try await annotations.refresh(client: client, session: session) } catch {
                                annotations.report(error)
                                return nil
                            }
                            return annotations.records.first { $0.id == note.id && $0.complete }
                        }
                        .refreshable { await load() }
                        .task(id: "\(file.revision)|\(focus?.description ?? "")") {
                            guard let focus, focus.lowerBound <= reader.lines.count else { return }
                            proxy.scrollTo(focus.lowerBound, anchor: UnitPoint(x: 0, y: 0.35))
                        }
                    }
                    if let notice = reader.truncationNotice { FileNote(text: notice) }
                }
            }
        } else if let error = reader.error {
            ContentUnavailableView {
                Label(error, systemImage: "exclamationmark.triangle")
            } actions: {
                Button("Try Again") { Task { await load() } }
            }
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

/// Preview and Source for a Markdown file, as two capsules in a track.
private struct PreviewSourceSwitch: View {
    @Binding var source: Bool

    var body: some View {
        HStack(spacing: 0) {
            segment("Preview", selected: !source) { source = false }
            segment("Source", selected: source) { source = true }
        }
        .padding(2)
        .frame(height: 36)
        .background(Theme.hover, in: Capsule())
        .fixedSize()
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("markdown-mode")
    }

    private func segment(_ title: String, selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: 14, weight: selected ? .semibold : .regular))
                .foregroundStyle(Theme.text)
                .padding(.horizontal, 12)
                .frame(maxHeight: .infinity)
                .background {
                    if selected {
                        Capsule().fill(Theme.surface)
                            .shadow(color: .black.opacity(0.1), radius: 3, y: 2)
                            .overlay { Capsule().strokeBorder(Theme.border, lineWidth: 0.5) }
                    }
                }
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A quiet line under or over a file: how much shows, or why it didn't
/// refresh.
private struct FileNote: View {
    let text: String
    var warning = false

    var body: some View {
        Text(text)
            .font(.system(size: 13))
            .foregroundStyle(warning ? Theme.warning : Theme.muted)
            .padding(.horizontal, 20)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.raised)
            .accessibilityIdentifier("file-note")
    }
}

/// A file to show over the session, and the lines to bring into view.
struct OpenedFile: Hashable, Identifiable {
    let path: String
    var lines: FileLineRange?
    var id: String { path + ":" + (lines?.description ?? "") }

    /// The file in the session's folder `cwd` a transcript's file link
    /// opens, at the lines it names; nil for another link or a file
    /// elsewhere, which this app can't open. A relative link starts from
    /// `folder`, a Markdown file's folder in the session's folder, or from
    /// the session's folder without one.
    static func linked(_ url: URL, cwd: String?, folder: String = "") -> OpenedFile? {
        // Only the server knows its home, which this app doesn't read.
        guard let link = FilePaths.path(fromLink: url), link != "~", !link.hasPrefix("~/") else { return nil }
        let relative = !link.hasPrefix("/")
        guard let path = ToolPresentation.workspacePath(relative && !folder.isEmpty ? folder + "/" + link : link, cwd: cwd)
        else { return nil }
        return OpenedFile(path: path, lines: FilePaths.lines(fromLink: url))
    }
}
