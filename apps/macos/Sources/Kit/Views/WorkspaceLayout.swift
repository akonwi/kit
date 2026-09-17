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
                        WorkspaceTabStrip(workspace: workspace, group: group,
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
        case .review: ReviewPane(workspace: state.ui.workspace)
        case .scratchpad: ScratchpadPane(workspace: state.ui.workspace)
        case .agent(let name): AgentPane(state: state, name: name)
        case .file(let path): FilePane(state: state, path: path)
        }
    }
}

private struct WorkspaceTabStrip: View {
    @Environment(\.mica) private var theme
    @Bindable var workspace: WorkspaceState
    let group: Int
    let runningAgents: Set<String>
    var body: some View {
        if workspace.panes.count > 1, workspace.groups.indices.contains(group) {
            HStack(spacing: 0) {
                ScrollView(.horizontal) {
                    HStack(spacing: 0) {
                        ForEach(workspace.groups[group]) { pane in
                            WorkspaceTab(workspace: workspace, pane: pane, group: group,
                                running: { if case .agent(let name) = pane { return runningAgents.contains(name) }; return false }())
                        }
                    }
                }.scrollIndicators(.hidden)
                Menu {
                    ForEach(workspace.groups[group]) { pane in
                        Button(pane.title) { workspace.select(pane) }
                    }
                    Divider()
                    WorkspaceTabActions(workspace: workspace, pane: workspace.selections[group])
                } label: { Image(systemName: "ellipsis").frame(width: 30, height: 38) }
                    .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
                    .accessibilityLabel("Workspace tab actions")
            }
            .frame(height: 39)
            .background(theme.surface)
            .overlay(alignment: .bottom) { Rule() }
        }
    }
}

private struct WorkspaceTab: View {
    @Environment(\.mica) private var theme
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
                    Button { workspace.close(pane) } label: {
                        Image(systemName: "xmark").font(.kit(size: 9)).frame(width: 24, height: 28)
                            .background(theme.surface)
                    }.buttonStyle(.plain).padding(.trailing, 2).accessibilityLabel("Close \(pane.title)")
                }
            }
            .onHover { hovering = $0 }
            .onChange(of: focused) { if focused { workspace.select(pane) } }
            .contextMenu { WorkspaceTabActions(workspace: workspace, pane: pane) }
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
}

private struct WorkspaceTabActions: View {
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
            Button("Close tab") { workspace.close(pane) }
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
    @Bindable var workspace: WorkspaceState
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                MetaLabel(text: "SESSION NOTES")
                Spacer()
                Button(workspace.scratchPreview ? "Edit" : "Preview") { workspace.scratchPreview.toggle() }
                    .buttonStyle(MicaButtonStyle(compact: true))
            }.padding(12)
            Rule()
            if workspace.scratchPreview {
                ScrollView { MarkdownView(source: workspace.scratchpad).frame(maxWidth: .infinity, alignment: .leading).padding(20) }
            } else {
                ScratchpadEditor(text: $workspace.scratchpad)
            }
            Spacer(minLength: 0)
            Rule()
            HStack { Text("Edits retained in this preview"); Spacer(); Text("\(workspace.scratchpad.count) characters") }
                .font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
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

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(path).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted).lineLimit(1).truncationMode(.middle)
                Spacer()
                if loading { KitSpinner() }
                Button { revision += 1 } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(.plain).accessibilityLabel("Refresh file").help("Refresh file").disabled(loading)
            }.padding(.horizontal, 20).padding(.vertical, 10)
            Rule()
            if let error {
                Text((preview == nil ? "" : "Showing an older preview. ") + error)
                    .font(.kit(size: 12)).foregroundStyle(theme.muted).padding(12)
                Rule()
            }
            if let preview {
                ReadOnlyFileEditor(path: path, source: preview.content, position: $position)
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
        }
    }
}
