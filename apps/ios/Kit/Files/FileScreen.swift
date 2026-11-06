import KitClient
import SwiftUI

/// A file in the session's project, read-only, with the composer below so a
/// follow-up can go straight back to the agent. Code shows by line; a
/// Markdown file shows rendered, with its source one tap away.
struct FileScreen: View {
    let path: String
    /// The line to bring into view and mark, counted from 1.
    var line: Int?
    let client: ServerClient
    let feed: SessionFeed
    let composer: SessionComposer
    let actions: SessionActions
    let interactions: SessionInteractions
    @State private var reader: FileReader
    @State private var colored: (revision: String, lines: [AttributedString])?
    @State private var source: Bool
    /// The line brought into view and marked: the one opened at.
    @State private var focus: Int?
    @AppStorage("fileWrapLines") private var wrapLines = false
    @Environment(\.scenePhase) private var scenePhase

    init(path: String, line: Int? = nil, client: ServerClient, feed: SessionFeed, composer: SessionComposer,
         actions: SessionActions, interactions: SessionInteractions) {
        self.path = path
        self.line = line
        self.client = client
        self.feed = feed
        self.composer = composer
        self.actions = actions
        self.interactions = interactions
        _reader = State(initialValue: FileReader(path: path))
        // A Markdown file opened at a line shows its source.
        _source = State(initialValue: line != nil)
        _focus = State(initialValue: line)
    }

    private var session: String { feed.identity.session }
    private var markdown: Bool { MarkdownFilePaths.isMarkdown(path) }

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
                if !feed.unavailable {
                    ComposerBar(composer: composer, actions: actions, interactions: interactions, feed: feed) { feed.reconnect() }
                }
            }
            .task { await load() }
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
                        FileLines(lines: reader.lines,
                                  colored: colored?.revision == file.revision ? colored?.lines : nil,
                                  wrap: wrapLines || markdown, marked: focus)
                        .refreshable { await load() }
                        .task(id: "\(file.revision)|\(focus ?? 0)") {
                            guard let focus, focus <= reader.lines.count else { return }
                            proxy.scrollTo(focus, anchor: UnitPoint(x: 0, y: 0.35))
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

/// A file to show over the session, and the line to bring into view.
struct OpenedFile: Hashable, Identifiable {
    let path: String
    var line: Int?
    var id: String { "\(path):\(line ?? 0)" }
}
