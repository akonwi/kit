import Foundation
import Observation
@preconcurrency import CodeEditSourceEditor

struct WorkspaceFixture: Decodable {
    let files: [PreviewFile]
    let scratchpad: String
}

struct PreviewFile: Decodable, Identifiable {
    var id: String { path }
    let path: String
    let content: String
    let patch: String
    var beforeContent: String? = nil
}

enum WorkspacePane: Hashable, Identifiable {
    case conversation, review, scratchpad, agent(String), file(String)
    var id: String {
        switch self {
        case .conversation: "conversation"
        case .review: "review"
        case .scratchpad: "scratchpad"
        case .agent(let name): "agent:" + name
        case .file(let path): "file:" + path
        }
    }
    var title: String {
        switch self {
        case .conversation: "Agent"
        case .review: "Diff"
        case .scratchpad: "Scratchpad"
        case .agent(let name): name
        case .file(let path): URL(fileURLWithPath: path).lastPathComponent
        }
    }
    var closable: Bool { self != .conversation }
}

@MainActor @Observable
final class WorkspaceState {
    let fixture: WorkspaceFixture
    var fileIndex = FileIndexCache()
    let filePreviews = FilePreviewCache()
    let diff = DiffState()
    var panes: [WorkspacePane] = [.conversation]
    var groups: [[WorkspacePane]] = [[.conversation]]
    var selections: [WorkspacePane] = [.conversation]
    var focusedGroup = 0
    var contentFocused = true
    var splitFraction = 0.5
    var selected: WorkspacePane { selections[min(focusedGroup, selections.count - 1)] }
    var visible: Bool { panes.count > 1 }

    func isVisible(_ pane: WorkspacePane) -> Bool { selections.contains(pane) }
    func select(_ pane: WorkspacePane) {
        guard let group = groups.firstIndex(where: { $0.contains(pane) }) else { return }
        focusedGroup = group
        contentFocused = true
        selections[group] = pane
    }
    var scratchpadState: ScratchpadState
    var scratchpadPosition = SourceEditorState()
    var scratchpad: String {
        get { scratchpadState.draft }
        set { scratchpadState.edit(newValue) }
    }
    var selectedFile = ""
    var changesVisible = true
    var notes: [String: String] = [:]
    var editingLine: Int?
    var noteDraft = ""

    init(demo: Bool = true) { fixture = WorkspaceFixture(files: [], scratchpad: demo ? "# Scratchpad\n\n" : ""); scratchpadState = ScratchpadState(demo: demo); selectedFile = fixture.files.first(where: { !$0.patch.isEmpty })?.path ?? "" }

    func closeScratchpad(client: any ScratchpadClient, session: String) async {
        if scratchpadState.dirty {
            guard await scratchpadState.save(client: client, session: session) else { select(.scratchpad); return }
        }
        close(.scratchpad)
    }

    func invalidateDirectory() {
        fileIndex = FileIndexCache()
        filePreviews.clear()
        diff.invalidate()
        for pane in panes { if case .file = pane { close(pane) } }
        selectedFile = ""
        editingLine = nil
    }

    func open(_ pane: WorkspacePane) {
        if !panes.contains(pane) {
            panes.append(pane)
            groups[focusedGroup].append(pane)
        }
        select(pane)
    }

    func close(_ pane: WorkspacePane) {
        guard pane.closable, let group = groups.firstIndex(where: { $0.contains(pane) }),
              let index = groups[group].firstIndex(of: pane) else { return }
        if case .file(let path) = pane { filePreviews.remove(path) }
        panes.removeAll { $0 == pane }
        groups[group].remove(at: index)
        if groups[group].isEmpty {
            groups.remove(at: group)
            selections.remove(at: group)
            focusedGroup = min(focusedGroup, groups.count - 1)
        } else if selections[group] == pane {
            selections[group] = groups[group][min(index, groups[group].count - 1)]
        }
    }

    func moveToOtherGroup(_ pane: WorkspacePane) {
        guard let source = groups.firstIndex(where: { $0.contains(pane) }) else { return }
        if groups.count == 1 {
            guard groups[0].count > 1 else { return }
            groups.append([])
            selections.append(pane)
        }
        let destination = 1 - source
        groups[source].removeAll { $0 == pane }
        groups[destination].append(pane)
        if pane == .conversation { groups[destination].removeAll { $0 == pane }; groups[destination].insert(pane, at: 0) }
        selections[destination] = pane
        focusedGroup = destination
        if groups[source].isEmpty {
            groups.remove(at: source)
            selections.remove(at: source)
            focusedGroup = 0
        } else if selections[source] == pane { selections[source] = groups[source][0] }
    }

    func joinGroups(selecting pane: WorkspacePane) {
        let ordered = groups.flatMap { $0 }.filter { $0 != .conversation }
        groups = [[.conversation] + ordered]
        selections = [pane]
        focusedGroup = 0
    }

    func noteKey(_ line: Int) -> String { "\(selectedFile):\(line)" }

    func saveNote() {
        guard let line = editingLine else { return }
        let value = noteDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.isEmpty { notes.removeValue(forKey: noteKey(line)) }
        else { notes[noteKey(line)] = value }
        editingLine = nil
        noteDraft = ""
    }

    func editNote(line: Int) {
        editingLine = line
        noteDraft = notes[noteKey(line)] ?? ""
    }
}
