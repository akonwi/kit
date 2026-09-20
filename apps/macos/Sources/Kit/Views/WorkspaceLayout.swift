@preconcurrency import CodeEditSourceEditor
import AppKit
import SwiftUI

struct WorkspaceLayout: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    var body: some View {
        @Bindable var workspace = state.ui.workspace
        VStack(spacing: 0) {
            GeometryReader { geometry in
                let split = workspace.groups.count == 2
                let left = split ? ((geometry.size.width - 5) * workspace.splitFraction).rounded() : geometry.size.width
                ZStack(alignment: .topLeading) {
                    if split {
                        WorkspaceSplitDivider(fraction: $workspace.splitFraction, color: theme.border)
                    }
                    ForEach(Array(workspace.groups.indices), id: \.self) { group in
                        WorkspaceTabStrip(state: state, workspace: workspace, group: group,
                            runningAgents: Set((state.selected?.subagents?.items ?? []).filter { $0.status == "running" }.map(\.name)))
                            .frame(width: group == 0 ? left : geometry.size.width - left - 5)
                            .offset(x: group == 0 ? 0 : left + 5)
                    }
                    // Pane identity is independent of its group. Moving or selecting
                    // tabs changes geometry and visibility without remounting editors.
                    ForEach(workspace.panes) { pane in
                        let group = workspace.groups.firstIndex(where: { $0.contains(pane) }) ?? 0
                        let visible = workspace.isVisible(pane)
                        let stripHeight: CGFloat = workspace.panes.count > 1 ? 39 : 0
                        paneBody(pane)
                            .id(state.serverID + "|" + state.selectedID + "|" + pane.id)
                            .frame(width: group == 0 ? left : geometry.size.width - left - 5,
                                   height: max(0, geometry.size.height - stripHeight))
                            .clipped()
                            .offset(x: group == 0 ? 0 : left + 5, y: stripHeight)
                            .opacity(visible ? 1 : 0)
                            .allowsHitTesting(visible).disabled(!visible).accessibilityHidden(!visible)
                            .simultaneousGesture(TapGesture().onEnded { workspace.focusedGroup = group; workspace.contentFocused = true })
                    }
                }
            }
            SessionComposerRegion(state: state)
                .simultaneousGesture(TapGesture().onEnded { workspace.contentFocused = false })
        }.background(theme.surface)
    }
    @ViewBuilder private func paneBody(_ pane: WorkspacePane) -> some View {
        switch pane {
        case .conversation: SessionView(state: state)
        case .review: ReviewPane(state: state)
        case .scratchpad: ScratchpadPane(state: state)
        case .agent(let name): AgentPane(state: state, name: name)
        case .file(let path): FilePane(state: state, path: path)
        }
    }
}

private struct WorkspaceTabStrip: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    @Bindable var workspace: WorkspaceState
    let group: Int
    let runningAgents: Set<String>
    var body: some View {
        if workspace.panes.count > 1, workspace.groups.indices.contains(group) {
            ScrollView(.horizontal) {
                HStack(spacing: 0) {
                    ForEach(workspace.groups[group]) { pane in
                        WorkspaceTab(state: state, workspace: workspace, pane: pane, group: group,
                            running: { if case .agent(let name) = pane { return runningAgents.contains(name) }; return false }())
                    }
                }
            }.scrollIndicators(.hidden)
            .frame(height: 39)
            .background(theme.surface)
            .overlay(alignment: .bottom) { Rule() }
        }
    }
}

private struct WorkspaceTab: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    @Bindable var workspace: WorkspaceState
    let pane: WorkspacePane
    let group: Int
    let running: Bool
    @State private var hovering = false
    @FocusState private var focused: Bool
    private var selected: Bool { workspace.selections.indices.contains(group) && workspace.selections[group] == pane }
    var body: some View {
        Button { workspace.select(pane) } label: {
            HStack(spacing: 6) {
                if case .agent = pane {
                    Group {
                        if running { KitSpinner().accessibilityLabel("Running") }
                        else { RobotTabIcon() }
                    }.frame(width: 15, height: 15)
                }
                else if let icon { Image(systemName: icon).font(.kit(size: 12)) }
                Text(pane.title).lineLimit(1).truncationMode(.middle)
            }.font(.kit(size: 12))
                .foregroundStyle(selected ? theme.text : theme.muted)
                .padding(.horizontal, 12).frame(height: 38)
                .contentShape(Rectangle())
        }.buttonStyle(.plain).focused($focused)
            .overlay(alignment: .bottom) {
                if selected { Rectangle().fill(workspace.contentFocused && workspace.focusedGroup == group ? theme.accent : theme.border).frame(height: 2) }
            }
            .overlay(alignment: .trailing) {
                if pane.closable && (hovering || focused) {
                    Button { close() } label: {
                        Image(systemName: "xmark").font(.kit(size: 9)).frame(width: 24, height: 28)
                            .background(theme.surface)
                    }.buttonStyle(.plain).padding(.trailing, 2).accessibilityLabel("Close \(pane.title)")
                }
            }
            .onHover { hovering = $0 }
            .onChange(of: focused) { if focused { workspace.select(pane) } }
            .contextMenu { WorkspaceTabActions(state: state, workspace: workspace, pane: pane) }
            .accessibilityAddTraits(selected ? .isSelected : [])
    }
    private var icon: String? {
        switch pane {
        case .file: "doc"
        case .review: "chevron.left.forwardslash.chevron.right"
        case .scratchpad: "note.text"
        default: nil
        }
    }
    private func close() {
        if pane == .scratchpad, let client = state.scratchpadClient {
            Task { await workspace.closeScratchpad(client: client, session: state.selectedID) }
        } else { workspace.close(pane) }
    }
}

private struct WorkspaceTabActions: View {
    let state: SessionStore
    let workspace: WorkspaceState
    let pane: WorkspacePane
    var body: some View {
        Button(workspace.groups.count == 1 ? "Move to split right" : "Move to other group") { workspace.moveToOtherGroup(pane) }
            .disabled(workspace.panes.count < 2)
        if workspace.groups.count > 1 {
            Button("Join all tabs") { workspace.joinGroups(selecting: pane) }
        }
        if pane.closable {
            Divider()
            Button("Close tab") {
                if pane == .scratchpad, let client = state.scratchpadClient {
                    Task { await workspace.closeScratchpad(client: client, session: state.selectedID) }
                } else { workspace.close(pane) }
            }
        }
    }
}

/// Phosphor Robot (regular), rendered from the bundled vector asset.
private struct RobotTabIcon: View {
    var body: some View {
        if let url = Bundle.module.url(forResource: "robot", withExtension: "svg"), let image = NSImage(contentsOf: url) {
            Image(nsImage: image).renderingMode(.template).resizable().scaledToFit()
        }
    }
}
struct ScratchpadPane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    private var workspace: WorkspaceState { state.ui.workspace }
    private var pad: ScratchpadState { workspace.scratchpadState }
    var body: some View {
        VStack(spacing: 0) {
            if pad.status == .conflict {
                HStack {
                    Text("Shared scratchpad changed. Your draft is preserved.")
                    Spacer()
                    Button("Review changes") { pad.review() }
                }.font(.kit(size: 11)).foregroundStyle(theme.warning).padding(12)
                Rule()
            }
            if pad.reviewing {
                ScrollView([.vertical, .horizontal]) {
                    HStack {
                        Text(pad.reviewText).font(.kit(size: 12, design: .monospaced))
                            .textSelection(.enabled).fixedSize(horizontal: true, vertical: false)
                        Spacer(minLength: 0)
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(16)
                }.accessibilityLabel("Scratchpad changes")
                Rule()
                HStack {
                    Button("Keep editing") { pad.keepEditing() }
                    Button("Use shared") { pad.useShared() }
                    Spacer()
                    Button("Replace shared with mine") {
                        guard let client = state.scratchpadClient else { return }
                        Task { await pad.replaceReviewed(client: client, session: state.selectedID) }
                    }
                }.padding(12)
            } else {
                @Bindable var workspace = workspace
                ScratchpadEditor(text: $workspace.scratchpad, documentVersion: pad.documentVersion,
                    editorState: $workspace.scratchpadPosition)
            }
            if pad.status != .saved {
                Rule()
                HStack {
                    Spacer()
                    switch pad.status {
                    case .loading: Text("Loading…")
                    case .saved: EmptyView()
                    case .unsaved: Text("Unsaved")
                    case .saving: Text("Saving…")
                    case .conflict: Text("Conflict")
                    case .failed(let message):
                        Text(message).lineLimit(1)
                        Button("Retry") {
                            guard let client = state.scratchpadClient else { return }
                            Task { _ = await pad.save(client: client, session: state.selectedID) }
                        }
                    }
                }.font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
            }
        }
        .task(id: state.selectedID) {
            guard let client = state.scratchpadClient else { return }
            pad.bind(client: client, session: state.selectedID)
            await pad.load(client: client, session: state.selectedID)
        }
    }
}

struct FilePane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let path: String
    @State private var revision = 0
    @State private var position = SourceEditorState()
    private var entry: FilePreviewCache.Entry? { workspace.filePreviews.entries[path] }
    private var preview: WireWorkspaceFileRead? { entry?.preview }
    private var error: String? { entry?.error }
    private var loading: Bool { entry?.loading == true }
    private var workspace: WorkspaceState { state.ui.workspace }
    private var active: Bool { workspace.isVisible(.file(path)) }
    private var request: String {
        state.serverID + "|" + state.selectedID + "|" + (state.selected?.cwd ?? "") + "|" + String(active) + "|" + String(revision) + "|" + String(workspace.filePreviews.revision)
    }

    private var selectedLines: ClosedRange<Int>? {
        guard let preview, let ranges = position.cursorPositions, ranges.count == 1 else { return nil }
        return AnnotationSelection.lines(in: preview.content, selection: ranges[0].range)
    }
    private func beginAnnotation() {
        guard let preview, let lines = selectedLines else { return }
        state.annotationState.begin(anchor: .init(kind: .value0, workspaceFile: .init(
            workspaceId: preview.workspace.workspaceId, path: preview.path, fileRevision: preview.revision,
            startLine: lines.lowerBound, endLine: lines.upperBound), workingTreeDiff: nil))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(path).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted).lineLimit(1).truncationMode(.middle)
                Spacer()
                if loading { KitSpinner() }
                if state.catalogClient is any AnnotationClient {
                    Button(state.annotationState.replacement == nil && state.annotationState.selectionDraft == nil ? "Annotate selection" : "Use selected range") { beginAnnotation() }
                        .font(.kit(size: 11)).buttonStyle(.plain)
                        .help("Annotate up to 200 selected lines")
                        .disabled(selectedLines == nil || state.annotationState.pending || state.annotationState.editor != nil)
                }
                Button { revision += 1 } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(.plain).accessibilityLabel("Refresh file").help("Refresh file").disabled(loading)
            }.padding(.horizontal, 20).padding(.vertical, 10)
            Rule()
            if let error {
                Text((preview == nil ? "" : "Showing an older preview. ") + error)
                    .font(.kit(size: 12)).foregroundStyle(theme.muted).padding(12)
                Rule()
            }
            ForEach(state.annotationState.records.filter { $0.path == path && $0.stale && !$0.isDiff }) { note in
                Button {
                    state.annotationState.inspected = note
                } label: {
                    HStack {
                        Label("File changed", systemImage: "exclamationmark.triangle")
                        Text(note.body).lineLimit(1)
                        Spacer()
                        Text("View captured source")
                    }.font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
                }.buttonStyle(.plain)
            }
            if let anchor = state.annotationState.editor?.anchor.workspaceFile,
               anchor.path == path, let preview, anchor.fileRevision != preview.revision {
                HStack {
                    Text("File changed. Your unsaved comment is preserved.")
                    Spacer()
                    Button("Select new range") { state.annotationState.reselectEditor() }
                    Button("Cancel") { state.annotationState.cancelEditor() }
                }.font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
            }
            if state.annotationState.replacement?.path == path || state.annotationState.selectionDraft?.path == path {
                HStack {
                    Text("Select a new source range for your comment.")
                    Spacer()
                    Button("Cancel") { state.annotationState.cancelEditor() }
                }.font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
            }
            if let preview {
                if let client = state.catalogClient as? any AnnotationClient {
                    AnnotatedFileEditor(file: preview, position: $position, annotations: state.annotationState,
                        client: client, session: state.selectedID)
                        .id(preview.workspace.workspaceId + preview.revision).clipped()
                } else {
                    ReadOnlyFileEditor(path: path, source: preview.content, position: $position)
                }
            } else if let file = workspace.fixture.files.first(where: { $0.path == path }) {
                ReadOnlyFileEditor(path: path, source: file.content)
            } else {
                Text(loading ? "Loading file…" : error == nil ? "File preview unavailable" : "Refresh to try again")
                    .foregroundStyle(theme.muted).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            Text((entry?.stale == true && preview != nil ? "Read-only · Preview may be out of date" : "Read-only") + (preview?.truncated == true ? " · Preview truncated" : ""))
                .font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
        }.task(id: request) {
            guard active, !state.isDemo, let client = state.catalogClient as? any WorkspaceFileClient else { return }
            guard let cwd = state.selected?.cwd else { return }
            do { try await Task.sleep(for: .milliseconds(200)) } catch { return }
            await workspace.filePreviews.load(path, session: state.selectedID, cwd: cwd,
                client: client, force: revision > 0)
            if let annotationClient = state.catalogClient as? any AnnotationClient {
                let annotations = state.annotationState
                try? await annotations.refresh(client: annotationClient, session: state.selectedID)
                if let id = annotations.revealed, let stale = annotations.records.first(where: { $0.id == id && $0.stale }) {
                    annotations.inspected = stale; annotations.revealed = nil
                }
            }
        }
    }
}
