import AppKit
import Observation

/// Per-window UI state, with per-session drafts and workspaces retained locally.
@MainActor @Observable
final class SessionUIState {
    var palette = false {
        didSet {
            if palette && !oldValue { paletteGeneration = UUID() }
            // The palette's view is gone once it closes, so the screen it was
            // showing is reset here; each opening starts at the command list.
            if !palette { subagentsPresented = false }
        }
    }
    @ObservationIgnored private(set) var paletteGeneration = UUID()
    var subagentsPresented = false
    var filePicker = false
    var filePickerIntent = "mention"
    var mentionQuery = ""
    var composerFocus = 0
    var composerFocusAtEndRequest: Int?
    var transcript = TranscriptPresentationState()
    var workspace: WorkspaceState
    var notice = ""
    var draft = ""
    /// The composer's caret within the draft. Only the selected session tab
    /// is mounted, so this restores the caret when its composer is rebuilt.
    @ObservationIgnored var composerSelection: NSRange?
    var attachments: [String] = []
    var uploads = ComposerAttachments()
    var serverAttachmentIDs: [String] = []
    var attachmentPreviews: [String: NSImage] = [:]
    private struct DraftState {
        let uploads: ComposerAttachments
        let text: String
        let selection: NSRange?
        let serverAttachmentIDs: [String]
        let attachments: [String]
        let previews: [String: NSImage]
        let workspace: WorkspaceState
        let transcript: TranscriptPresentationState
    }
    private var drafts: [SessionIdentity: DraftState] = [:]

    init(demo: Bool) { workspace = WorkspaceState(demo: demo) }

    func restoreQueueDraft(session: SessionIdentity, text: String, attachmentIDs: [String]) {
        guard let saved = drafts[session] else { return }
        drafts[session] = DraftState(uploads: saved.uploads, text: [text, saved.text].filter { !$0.isEmpty }.joined(separator: "\n\n"), selection: nil,
            serverAttachmentIDs: ComposerAttachments.uniqueIDs(saved.serverAttachmentIDs + attachmentIDs), attachments: saved.attachments,
            previews: saved.previews, workspace: saved.workspace, transcript: saved.transcript)
    }

    /// Keeps the drafts of a server's sessions under the server's new name.
    func serverRenamed(from name: String, to newName: String) {
        drafts = Dictionary(uniqueKeysWithValues: drafts.map { identity, draft in
            (identity.server == name ? SessionIdentity(server: newName, session: identity.session) : identity, draft)
        })
    }

    /// Stops every browser page and ends every shell this tab holds for its sessions.
    func closeLiveTabs() {
        for workspace in workspaces { workspace.closeLiveTabs() }
    }

    /// The current workspace and those kept for other sessions.
    var workspaces: [WorkspaceState] { [workspace] + drafts.values.map(\.workspace) }

    func switchSession(from old: SessionIdentity, to new: SessionIdentity, demo: Bool) {
        drafts[old] = DraftState(uploads: uploads, text: draft, selection: composerSelection, serverAttachmentIDs: serverAttachmentIDs, attachments: attachments, previews: attachmentPreviews, workspace: workspace, transcript: transcript)
        subagentsPresented = false
        composerFocusAtEndRequest = nil
        let saved = drafts[new]
        uploads = saved?.uploads ?? ComposerAttachments()
        draft = saved?.text ?? ""
        composerSelection = saved?.selection
        serverAttachmentIDs = saved?.serverAttachmentIDs ?? []
        attachments = saved?.attachments ?? []
        attachmentPreviews = saved?.previews ?? [:]
        workspace = saved?.workspace ?? WorkspaceState(demo: demo)
        transcript = saved?.transcript ?? TranscriptPresentationState()
        notice = ""
    }
}

/// Persistent row interaction state, owned by the session rather than its views.
@MainActor final class TranscriptPresentationState {
    private var drawers: [String: ToolDrawerState] = [:]

    func drawer(for messageID: String) -> ToolDrawerState {
        if let existing = drawers[messageID] { return existing }
        let state = ToolDrawerState()
        drawers[messageID] = state
        return state
    }

    /// Forgets interaction state for rows no longer in the transcript.
    func retainDrawers(for messages: [TranscriptMessage]) {
        let keys = Set(messages.map(\.id) + messages.compactMap { $0.plugin.map { "plugin:" + $0.turnID } })
        drawers = drawers.filter { keys.contains($0.key) }
    }
}

@MainActor @Observable final class ToolDrawerState {
    /// Groups larger than this collapse by default.
    static let collapseThreshold = 5

    /// Nil follows the default; explicit choices survive updates.
    var expanded: Bool?
    var selectedTool: String?

    /// How a batch of this many consecutive tool calls presents. A lone call
    /// is just its row, never collapsed. A group has a disclosure header and
    /// is open through the threshold, live or finished, and collapsed above
    /// it, unless the user chose otherwise.
    func disclosure(count: Int) -> ToolBatchDisclosure {
        guard count > 1 else { return .single }
        return .group(expanded: expanded ?? (count <= Self.collapseThreshold))
    }
}

/// The presentation of a batch of tool calls.
enum ToolBatchDisclosure: Equatable {
    /// A lone call: its row with no header.
    case single
    /// Consecutive calls under a disclosure header, showing their rows when
    /// expanded.
    case group(expanded: Bool)

    /// Whether the batch's rows are visible.
    var showsRows: Bool {
        switch self {
        case .single: true
        case .group(let expanded): expanded
        }
    }
}
