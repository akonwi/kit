import SwiftUI
@preconcurrency import CodeEditSourceEditor

/// A retained diff surface. Targets and evidence are supplied by the session server.
struct ReviewPane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    private var model: DiffState { state.ui.workspace.diff }
    private var client: (any DiffClient)? { state.catalogClient as? any DiffClient }
    private var active: Bool { state.ui.workspace.isVisible(.review) }
    private var editing: Bool { state.annotationState.editor != nil }

    var body: some View {
        @Bindable var options = model
        ScrollViewReader { proxy in
            VStack(spacing: 0) {
                HStack(spacing: 12) {
                    if model.loading { KitSpinner() }
                    Menu {
                        ForEach(model.catalog?.targets ?? [], id: \.targetId) { target in
                            Button {
                                guard let client else { return }
                                let model = model, session = state.selectedID
                                Task { await model.select(target, client: client, session: session) }
                            } label: {
                                Text(target.metadata.label + (target.annotationCount.map { $0 > 0 ? " · \($0) comments" : "" } ?? ""))
                            }
                        }
                    } label: { Text(model.target?.metadata.label ?? (model.observation == nil ? "Diff target" : "Captured target")) }
                        .menuStyle(.borderlessButton).fixedSize().disabled(editing || model.catalog == nil)
                        .accessibilityLabel("Diff target")
                    Menu {
                        ForEach(model.files, id: \.path) { file in
                            Button(file.path) { proxy.scrollTo(file.path, anchor: .top) }
                        }
                    } label: { Label("Files", systemImage: "list.bullet") }
                        .menuStyle(.borderlessButton).fixedSize().disabled(model.files.isEmpty)
                        .accessibilityLabel("Jump to changed file")
                    Text("\(model.files.count) changed \(model.files.count == 1 ? "file" : "files")" + (model.fileCursor == nil ? "" : " · More available"))
                        .font(.kit(size: 10)).foregroundStyle(theme.muted).lineLimit(1)
                    Spacer(minLength: 8)
                    Toggle(isOn: $options.wrapLines) {
                        Label("Wrap", systemImage: "arrow.turn.down.left")
                    }.toggleStyle(.button).controlSize(.small).help("Wrap long lines")
                        .accessibilityLabel("Wrap diff lines")
                    Picker("Diff layout", selection: $options.layout) {
                        ForEach(DiffLayout.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }.pickerStyle(.segmented).labelsHidden().frame(width: 140)
                }.font(.kit(size: 12)).padding(12)
                Rule()
                if let error = model.error {
                    Text(error).font(.kit(size: 12)).foregroundStyle(theme.warning)
                        .frame(maxWidth: .infinity, alignment: .leading).padding(12)
                }
                if let editor = state.annotationState.editor, editor.anchor.workingTreeDiff != nil,
                   model.document?.range(editor.anchor) == nil,
                   !model.fileStates.values.contains(where: { $0.document?.range(editor.anchor) != nil }) {
                    HStack {
                        Text("Your unsaved comment is preserved at its original diff revision.")
                        Spacer()
                        Button("Select new range") { state.annotationState.reselectEditor() }
                        Button("Cancel") { state.annotationState.cancelEditor() }
                    }.font(.kit(size: 11)).padding(12)
                }
                if state.annotationState.selectionDraft != nil || state.annotationState.replacement?.isDiff == true {
                    HStack {
                        Text("Select a new diff range for your comment.")
                        Spacer()
                        Button("Cancel") { state.annotationState.cancelEditor() }
                    }.font(.kit(size: 11)).padding(12)
                }
                if model.files.isEmpty {
                    VStack(spacing: 12) {
                        if model.loading { KitSpinner() }
                        Text(model.loading ? "Loading diff…" : model.error != nil ? "Retrying automatically…" : "No changed files")
                            .font(.kit(size: 13)).foregroundStyle(theme.muted)
                    }.frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    ScrollView {
                        LazyVStack(spacing: 16) {
                            ForEach(model.files, id: \.path) { file in
                                DiffFileSection(file: file, model: model, state: state, active: active)
                                    .id(file.path)
                            }
                            if model.fileCursor != nil, let target = model.target {
                                Button("Load more changed files") {
                                    guard let client else { return }
                                    let model = model, session = state.selectedID
                                    Task { await model.select(target, client: client, session: session, more: true) }
                                }.buttonStyle(.plain).padding(12).disabled(model.loading || editing)
                            }
                        }.padding(12)
                    }
                }
            }
            .onChange(of: state.annotationState.revealToken) {
                if let path = model.selectedPath { proxy.scrollTo(path, anchor: .top) }
            }
        }
        .task(id: state.selectedID + String(active) + (state.selected?.cwd ?? "")) {
            guard active, let client else { return }
            let model = model, session = state.selectedID, annotations = state.annotationState
            while !Task.isCancelled {
                do { try await Task.sleep(for: .seconds(30)) } catch { return }
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
        .task(id: String(active) + (state.selected?.cwd ?? "") + model.navigationToken.uuidString) {
            guard active, let client else { return }
            let model = model, session = state.selectedID, annotations = state.annotationState
            if let note = model.navigation {
                model.navigation = nil
                let opened = await model.openAnnotation(note, client: client, session: session)
                guard !Task.isCancelled else { return }
                if opened {
                    annotations.revealed = note.id; annotations.revealToken = UUID()
                } else { annotations.inspected = note }
            } else if model.catalog == nil && model.document == nil {
                await model.refresh(client: client, session: session)
            }
        }
    }
}

private struct DiffFileSection: View {
    @Environment(\.mica) private var theme
    let file: WireDiffFileSummary
    let model: DiffState
    let state: SessionStore
    let active: Bool
    @State private var position = SourceEditorState()
    @State private var contentHeight: CGFloat = 100
    private var reader: DiffState? { model.fileStates[file.path] }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "doc.text")
                Text(file.path).font(.kit(size: 12, design: .monospaced))
                    .lineLimit(2).truncationMode(.middle).textSelection(.enabled)
                Spacer(minLength: 8)
                if let additions = file.additions { Text("+\(additions)").foregroundStyle(theme.success) }
                if let deletions = file.deletions { Text("−\(deletions)").foregroundStyle(theme.danger) }
            }.font(.kit(size: 11)).padding(12).background(theme.raised)
            Rule()
            if let reader, let document = reader.document, !document.rows.isEmpty,
               let client = state.catalogClient as? any AnnotationClient {
                if model.layout == .split {
                    SplitDiffEditors(document: document, annotations: state.annotationState, client: client,
                        session: state.selectedID, canAnnotate: !model.stale && !reader.stale && !reader.loading,
                        wrapLines: model.wrapLines).id(document.identity)
                } else {
                    AnnotatedFileEditor(diff: document, position: $position, annotations: state.annotationState,
                        client: client, session: state.selectedID, canAnnotate: !model.stale && !reader.stale && !reader.loading,
                        contentHeightChanged: { contentHeight = $0 }, wrapLines: model.wrapLines)
                        .frame(height: contentHeight).id(document.identity).clipped()
                }
            } else if reader?.loading == true || reader == nil {
                KitSpinner().padding(24)
            }
            if let notice = reader?.notice {
                Text(notice).font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
            }
            if let error = reader?.error {
                Text(error).font(.kit(size: 12)).foregroundStyle(theme.warning).padding(12)
                Button("Retry file") { read(more: false) }.buttonStyle(.plain).padding(12)
            } else if reader?.lineCursor != nil {
                Button("Load more diff lines") { read(more: true) }
                    .buttonStyle(.plain).font(.kit(size: 12)).padding(12)
                    .disabled(reader?.loading == true || state.annotationState.editor != nil)
            }
        }.frame(maxWidth: .infinity).background(theme.surface)
            .clipShape(RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.border, lineWidth: 1))
            .task(id: String(active) + (model.observation?.revision ?? "")) {
                guard active, let client = state.catalogClient as? any DiffClient else { return }
                await model.loadFile(file, client: client, session: state.selectedID)
            }
    }
    private func read(more: Bool) {
        guard let reader, let client = state.catalogClient as? any DiffClient else { return }
        let session = state.selectedID
        Task { await reader.read(file, client: client, session: session, more: more) }
    }
}

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
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                Text("Before").frame(maxWidth: .infinity, alignment: .leading)
                Text("After").frame(maxWidth: .infinity, alignment: .leading)
            }.font(.kit(size: 10)).foregroundStyle(theme.muted).padding(8)
            Rule()
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
}
