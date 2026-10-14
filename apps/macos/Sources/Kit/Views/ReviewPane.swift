import SwiftUI
@preconcurrency import CodeEditSourceEditor

/// The Diff pane: a 30 pt header row naming what is compared, with its
/// counts and a ⋯ view menu, over one continuous document of files. Each
/// file's header pins while its lines scroll and folds the file away when
/// clicked. Lines load as files come into view, and a shown pane checks the
/// target every few seconds, keeping the reader's place. Targets and evidence
/// are supplied by the session server.
struct ReviewPane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    @State private var width: CGFloat = 0
    @State private var scrolled = false
    @State private var popover = AnchoredPopover()
    @State private var openMenu: DiffHeaderMenu?
    @State private var lastClosedMenu: DiffHeaderMenu?
    @State private var targetAnchor = ComposerPickerAnchor()
    @State private var viewAnchor = ComposerPickerAnchor()
    private var model: DiffState { state.ui.workspace.diff }
    private var client: (any DiffClient)? { state.catalogClient as? any DiffClient }
    private var active: Bool { state.ui.workspace.isVisible(.review) }
    private var editing: Bool { state.annotationState.editor != nil }
    /// Side by side, when chosen and the pane is wide enough for it.
    private var sideBySide: Bool { model.layout == .split && DiffState.splitFits(width) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Rule()
            banners
            if model.files.isEmpty { emptyState } else { document }
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .onChange(of: state.selectedID) { popover.dismiss() }
        .onDisappear { popover.dismiss() }
        .onChange(of: state.annotationState.evidenceConflict) {
            guard active, state.annotationState.selectionDraft != nil,
                  state.annotationState.evidenceConflict == .staleTarget || state.annotationState.evidenceConflict == .staleWorkspace,
                  let client else { return }
            let model = model, session = state.selectedID
            Task { await model.refresh(client: client, session: session) }
        }
        .task(id: "\(state.selectedID)\(active)\(state.selected?.cwd ?? "")") {
            guard active, let client else { return }
            let model = model, session = state.selectedID, annotations = state.annotationState
            while !Task.isCancelled {
                do { try await Task.sleep(for: DiffState.pollInterval) } catch { return }
                let canRefresh = { annotations.editor == nil && annotations.selectionDraft == nil && !annotations.pending }
                guard !Task.isCancelled else { return }
                if canRefresh() {
                    await model.poll(client: client, session: session, canApply: canRefresh)
                    if !Task.isCancelled, canRefresh(), let client = client as? any AnnotationClient {
                        try? await annotations.refresh(client: client, session: session)
                    }
                }
            }
        }
        .task(id: "\(active)\(state.selected?.cwd ?? "")\(model.navigationToken.uuidString)") {
            guard active, let client else { return }
            let model = model, session = state.selectedID, annotations = state.annotationState
            if let note = model.navigation {
                model.navigation = nil
                let opened = await model.openAnnotation(note, client: client, session: session)
                guard !Task.isCancelled else { return }
                if opened {
                    model.collapsed.remove(note.path)
                    annotations.revealed = note.id; annotations.revealToken = UUID()
                } else { annotations.inspected = note }
            } else if model.catalog == nil && model.document == nil {
                await model.refresh(client: client, session: session)
            }
        }
    }

    // MARK: Header

    private var header: some View {
        HStack(spacing: 8) {
            DiffHeaderButton(open: openMenu == .target, action: { toggle(.target) }) {
                HStack(spacing: 6) {
                    DiffTargetName(target: model.target, fallback: model.observation == nil ? "Working tree" : "Captured target")
                    Image(systemName: "chevron.down").font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(theme.muted)
                }
            }
            .background(ComposerPickerAnchorView(anchor: targetAnchor))
            .disabled(editing || model.catalog == nil)
            .help("Choose what to compare")
            .accessibilityLabel("Diff target")
            .layoutPriority(-1)
            if !model.files.isEmpty {
                HStack(spacing: 8) {
                    Text(Self.fileCount(model.files.count, more: model.fileCursor != nil))
                        .font(.kit(size: 11)).foregroundStyle(theme.muted)
                    if model.additions != nil || model.deletions != nil {
                        Text("+\(model.additions ?? 0)").foregroundStyle(theme.success)
                        Text("−\(model.deletions ?? 0)").foregroundStyle(theme.danger)
                    }
                }
                .font(.kit(size: 11, design: .monospaced)).lineLimit(1).fixedSize()
            }
            Spacer(minLength: 8)
            DiffHeaderButton(open: openMenu == .view, square: true, action: { toggle(.view) }) {
                Image(systemName: "ellipsis").font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(openMenu == .view ? theme.text : theme.muted)
            }
            .background(ComposerPickerAnchorView(anchor: viewAnchor))
            .help("View options")
            .accessibilityLabel("View options")
        }
        .padding(.horizontal, 6)
        .frame(height: 30)
    }

    static func fileCount(_ count: Int, more: Bool) -> String {
        "\(count)\(more ? "+" : "") \(count == 1 && !more ? "file" : "files")"
    }

    private func toggle(_ menu: DiffHeaderMenu) {
        if popover.isOpen {
            let same = openMenu == menu
            popover.dismiss()
            if same { return }
        } else if lastClosedMenu == menu, let closedAt = popover.closedAt,
                  Date().timeIntervalSince(closedAt) < 0.3 {
            // The click that reached this control already closed its popover.
            return
        }
        let anchor = menu == .target ? targetAnchor : viewAnchor
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        let popover = popover, theme = theme, model = model, session = state.selectedID, client = client
        let content: AnyView
        switch menu {
        case .target:
            if let client { Task { await model.refreshCatalog(client: client, session: session) } }
            content = AnyView(DiffTargetPicker(model: model, theme: theme, choose: { target in
                popover.dismiss()
                guard let client, target.targetId != model.target?.targetId || model.observation == nil else { return }
                model.collapsed = []
                Task { await model.select(target, client: client, session: session) }
            }, dismiss: { popover.dismiss() }))
        case .view:
            content = AnyView(DiffViewMenu(model: model, theme: theme, splitFits: DiffState.splitFits(width),
                dismiss: { popover.dismiss() }))
        }
        openMenu = menu
        popover.show(anchor: frame, parent: window, edge: .below,
                     alignment: menu == .target ? .leading : .trailing, content: content) {
            lastClosedMenu = menu
            if openMenu == menu { openMenu = nil }
        }
    }

    // MARK: Notices about an open comment

    @ViewBuilder private var banners: some View {
        if let error = model.error, !model.files.isEmpty {
            DiffBanner(text: error + (model.errorDetail.map { ". " + $0 } ?? ""))
        }
        if let editor = state.annotationState.editor, editor.anchor.workingTreeDiff != nil,
           model.document?.range(editor.anchor) == nil,
           !model.fileStates.values.contains(where: { $0.document?.range(editor.anchor) != nil }) {
            DiffBanner(text: "Your unsaved comment is kept at the version it was written on.") {
                Button("Select new range") { state.annotationState.reselectEditor() }
                Button("Cancel") { state.annotationState.cancelEditor() }
            }
        }
        if state.annotationState.selectionDraft != nil || state.annotationState.replacement?.isDiff == true {
            DiffBanner(text: state.annotationState.evidenceConflict == .staleTarget ?
                "The diff changed. Select new lines for your comment." :
                "Select new lines for your comment.") {
                Button("Cancel") { state.annotationState.cancelEditor() }
            }
        }
    }

    // MARK: Content

    @ViewBuilder private var emptyState: some View {
        Group {
            if let error = model.error {
                DiffEmptyMessage(title: error, detail: model.errorDetail)
            } else if model.loading || model.catalog == nil {
                KitSpinner("Loading diff…").font(.kit(size: 12)).foregroundStyle(theme.muted)
            } else if model.target == nil, let notice = model.notice {
                DiffEmptyMessage(title: notice)
            } else {
                DiffEmptyMessage(title: model.target.map { $0.kind == "working_tree" } ?? true
                    ? "Working tree is clean" : "No changes")
            }
        }
        .padding(.bottom, 30)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var document: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0, pinnedViews: [.sectionHeaders]) {
                    ForEach(model.files, id: \.path) { file in
                        let collapsed = model.collapsed.contains(file.path)
                        Section {
                            if !collapsed {
                                DiffFileBody(file: file, model: model, state: state, active: active, sideBySide: sideBySide)
                            }
                        } header: {
                            DiffFileHeader(file: file, collapsed: collapsed, scrolled: scrolled) { pinned in
                                if collapsed { model.collapsed.remove(file.path) } else { model.collapsed.insert(file.path) }
                                // Folding from a pinned header brings the file's
                                // header back down so the reader keeps their place.
                                if pinned { proxy.scrollTo(file.path, anchor: .top) }
                            }.id(file.path)
                        }
                    }
                    if model.fileCursor != nil {
                        DiffNoteRow(text: "Loading more…", loading: true)
                            .task(id: "\(model.fileCursor ?? "")\(editing)\(model.loading)") { loadMoreFiles() }
                    }
                }
            }
            .onScrollGeometryChange(for: Bool.self) { $0.contentOffset.y > 0.5 } action: { scrolled = $1 }
            .onChange(of: state.annotationState.revealToken) {
                if let path = model.selectedPath { proxy.scrollTo(path, anchor: .top) }
            }
        }
    }

    private func loadMoreFiles() {
        guard active, !model.loading, !editing, let client, let target = model.target else { return }
        let model = model, session = state.selectedID
        Task { await model.select(target, client: client, session: session, more: true) }
    }

    /// What a file with no text diff says under its header, as in the TUI.
    static func contentNote(_ file: WireDiffFileSummary) -> String {
        switch file.contentState {
        case "binary": "Binary file changed"
        case "conflict": "File has unresolved conflicts"
        case "intent_to_add": "Intent-to-add file has no textual diff"
        case "unsupported_transform": "File uses an unsupported content transform"
        case "unsupported_kind": "File kind is not available for textual diff"
        case "too_large": "File diff exceeds the configured limit"
        case "unavailable": "File diff is unavailable"
        default: "No textual changes"
        }
    }
}

/// Which header control opened the pane's popover.
enum DiffHeaderMenu: Equatable { case target, view }

/// A header-row control: plain until hovered or open.
private struct DiffHeaderButton<Label: View>: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let open: Bool
    var square = false
    let action: () -> Void
    @ViewBuilder let label: Label
    @State private var hovered = false

    var body: some View {
        Button(action: action) {
            label
                .padding(.horizontal, square ? 0 : 6)
                .frame(width: square ? 22 : nil, height: 22)
                .background(open || hovered ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .opacity(enabled ? 1 : 0.6)
        .onHover { hovered = enabled && $0 }
    }
}

/// How a target is named: a commit by its short hash and subject, anything
/// else by the server's label.
struct DiffTargetName: View {
    @Environment(\.mica) private var theme
    let target: WireDiffTargetEntry?
    var fallback = "Working tree"

    var body: some View {
        HStack(spacing: 6) {
            if let target, target.kind == "commit", let oid = target.metadata.abbreviatedOid, !oid.isEmpty {
                Text(oid).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted).fixedSize()
                Text(target.metadata.subject ?? target.metadata.label).font(.kit(size: 12))
                    .foregroundStyle(theme.text).lineLimit(1).truncationMode(.tail)
            } else {
                Text(Self.label(target) ?? fallback).font(.kit(size: 12))
                    .foregroundStyle(theme.text).lineLimit(1).truncationMode(.tail)
            }
        }
    }

    static func label(_ target: WireDiffTargetEntry?) -> String? {
        guard let target else { return nil }
        if !target.metadata.label.isEmpty { return target.metadata.label }
        return target.kind == "working_tree" ? "Working tree" : nil
    }
}

/// A file's 34 pt header: a caret, the path, and its counts. It pins to the
/// top while the file's lines scroll under it.
private struct DiffFileHeader: View {
    @Environment(\.mica) private var theme
    let file: WireDiffFileSummary
    let collapsed: Bool
    let scrolled: Bool
    /// Folds or unfolds the file; told whether the header was pinned.
    let toggle: (_ pinned: Bool) -> Void
    @State private var atTop = false

    var body: some View {
        Button { toggle(scrolled && atTop) } label: {
            HStack(spacing: 8) {
                Image(systemName: collapsed ? "chevron.right" : "chevron.down")
                    .font(.system(size: 9, weight: .semibold)).foregroundStyle(theme.muted).frame(width: 10)
                Text(file.path).font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.text)
                    .lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 8)
                if file.additions != nil || file.deletions != nil {
                    HStack(spacing: 8) {
                        Text("+\(file.additions ?? 0)").foregroundStyle(theme.success)
                        Text("−\(file.deletions ?? 0)").foregroundStyle(theme.danger)
                    }.font(.kit(size: 12, design: .monospaced)).fixedSize()
                }
            }
            .padding(.horizontal, 16)
            .frame(height: 34)
            .background(theme.raised)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .overlay(alignment: .bottom) { Rule() }
        .onGeometryChange(for: Bool.self) { $0.frame(in: .scrollView).minY < 0.5 } action: { atTop = $0 }
        .help(collapsed ? "Expand" : "Collapse")
        .accessibilityLabel("\(collapsed ? "Expand" : "Collapse") \(file.path)")
    }
}

/// A file's lines, or why it has none.
private struct DiffFileBody: View {
    let file: WireDiffFileSummary
    let model: DiffState
    let state: SessionStore
    let active: Bool
    let sideBySide: Bool
    @State private var position = SourceEditorState()
    @State private var contentHeight: CGFloat = 100
    private var reader: DiffState? { model.fileStates[file.path] }
    private var textual: Bool { file.contentState == "text" }

    var body: some View {
        VStack(spacing: 0) {
            if !textual {
                DiffNoteRow(text: ReviewPane.contentNote(file))
            } else if let reader, let document = reader.document, !document.rows.isEmpty,
                      let client = state.catalogClient as? any AnnotationClient {
                let canAnnotate = !model.stale && !reader.stale && !reader.loading
                if sideBySide {
                    SplitDiffEditors(document: document, annotations: state.annotationState, client: client,
                        session: state.selectedID, canAnnotate: canAnnotate, wrapLines: model.wrapLines)
                        .id(document.identity)
                } else {
                    AnnotatedFileEditor(diff: document, position: $position, annotations: state.annotationState,
                        client: client, session: state.selectedID, canAnnotate: canAnnotate,
                        contentHeightChanged: { contentHeight = $0 }, wrapLines: model.wrapLines)
                        .frame(height: contentHeight).id(document.identity).clipped()
                }
                if let notice = reader.notice { DiffNoteRow(text: notice) }
                if reader.lineCursor != nil {
                    DiffNoteRow(text: "Loading more…", loading: true)
                        .task(id: "\(reader.lineCursor ?? "")\(state.annotationState.editor == nil)\(reader.loading)") {
                            guard !reader.loading, state.annotationState.editor == nil else { return }
                            read(more: true)
                        }
                }
            } else if let error = reader?.error {
                HStack(spacing: 8) {
                    DiffNoteRow(text: "Could not load changes: " + error).fixedSize(horizontal: true, vertical: false)
                    Button("Try again") { read(more: false) }.buttonStyle(SmallButtonStyle())
                    Spacer(minLength: 0)
                }
            } else if let notice = reader?.notice, reader?.loading == false {
                DiffNoteRow(text: notice)
            } else {
                DiffNoteRow(text: "Loading…", loading: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .task(id: String(active) + (model.observation?.revision ?? "")) {
            guard active, textual, let client = state.catalogClient as? any DiffClient else { return }
            await model.loadFile(file, client: client, session: state.selectedID)
        }
    }

    private func read(more: Bool) {
        guard let reader, let client = state.catalogClient as? any DiffClient else { return }
        let session = state.selectedID
        Task { await reader.read(file, client: client, session: session, more: more) }
    }
}

/// A quiet line under a file's header: why there is no text, or what is loading.
private struct DiffNoteRow: View {
    @Environment(\.mica) private var theme
    let text: String
    var loading = false

    var body: some View {
        HStack(spacing: 8) {
            if loading { KitSpinner() }
            Text(text).lineLimit(1).truncationMode(.tail)
        }
        .font(.kit(size: 12)).foregroundStyle(theme.muted)
        .padding(.leading, 76).padding(.trailing, 16)
        .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
    }
}

/// The centered message an empty pane shows.
private struct DiffEmptyMessage: View {
    @Environment(\.mica) private var theme
    let title: String
    var detail: String? = nil

    var body: some View {
        VStack(spacing: 4) {
            Text(title).font(.kit(size: 13)).foregroundStyle(theme.text)
            if let detail { Text(detail).font(.kit(size: 12)).foregroundStyle(theme.muted) }
        }
        .multilineTextAlignment(.center)
        .padding(.horizontal, 24)
    }
}

/// A slim row under the header about the pane's state or an open comment.
private struct DiffBanner<Actions: View>: View {
    @Environment(\.mica) private var theme
    let text: String
    @ViewBuilder let actions: Actions

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text(text).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(2)
                Spacer(minLength: 8)
                actions.buttonStyle(SmallButtonStyle())
            }
            .padding(.horizontal, 12).frame(minHeight: 32)
            Rule()
        }
    }
}

private extension DiffBanner where Actions == EmptyView {
    init(text: String) { self.init(text: text) { EmptyView() } }
}

/// Old lines on the left, new on the right, kept row for row.
private struct SplitDiffEditors: View {
    @Environment(\.mica) private var theme
    let document: DiffDocument
    let annotations: AnnotationState
    let client: any AnnotationClient
    let session: String
    let canAnnotate: Bool
    let wrapLines: Bool
    @State private var oldPosition = SourceEditorState()
    @State private var newPosition = SourceEditorState()
    @State private var oldHeight: CGFloat = 100
    @State private var newHeight: CGFloat = 100
    @State private var alignment = DiffSplitAlignment()

    var body: some View {
        let pair = document.split()
        HStack(alignment: .top, spacing: 0) {
            AnnotatedFileEditor(diff: pair.old, position: $oldPosition, annotations: annotations,
                client: client, session: session, canAnnotate: canAnnotate,
                contentHeightChanged: { oldHeight = $0 }, wrapLines: wrapLines, splitAlignment: alignment)
                .frame(maxWidth: .infinity).frame(height: max(oldHeight, newHeight)).clipped()
            Rectangle().fill(theme.border).frame(width: 1)
            AnnotatedFileEditor(diff: pair.new, position: $newPosition, annotations: annotations,
                client: client, session: session, canAnnotate: canAnnotate,
                contentHeightChanged: { newHeight = $0 }, wrapLines: wrapLines, splitAlignment: alignment)
                .frame(maxWidth: .infinity).frame(height: max(oldHeight, newHeight)).clipped()
        }.frame(height: max(oldHeight, newHeight))
    }
}
