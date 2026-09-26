import AppKit
import Observation

/// Per-window UI state, with per-session drafts and workspaces retained locally.
@MainActor @Observable
final class SessionUIState {
    var palette = false {
        didSet { if palette && !oldValue { paletteGeneration = UUID() } }
    }
    @ObservationIgnored private(set) var paletteGeneration = UUID()
    var subagentsPresented = false
    var filePicker = false
    var filePickerIntent = "mention"
    var mentionQuery = ""
    var composerFocus = 0
    var transcript = TranscriptPresentationState()
    var workspace: WorkspaceState
    var notice = ""
    var draft = ""
    var attachments: [String] = []
    var uploads = ComposerAttachments()
    var serverAttachmentIDs: [String] = []
    var attachmentPreviews: [String: NSImage] = [:]
    private struct DraftState {
        let uploads: ComposerAttachments
        let text: String
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
        drafts[session] = DraftState(uploads: saved.uploads, text: [text, saved.text].filter { !$0.isEmpty }.joined(separator: "\n\n"),
            serverAttachmentIDs: ComposerAttachments.uniqueIDs(saved.serverAttachmentIDs + attachmentIDs), attachments: saved.attachments,
            previews: saved.previews, workspace: saved.workspace, transcript: saved.transcript)
    }

    func switchSession(from old: SessionIdentity, to new: SessionIdentity, demo: Bool) {
        drafts[old] = DraftState(uploads: uploads, text: draft, serverAttachmentIDs: serverAttachmentIDs, attachments: attachments, previews: attachmentPreviews, workspace: workspace, transcript: transcript)
        subagentsPresented = false
        let saved = drafts[new]
        uploads = saved?.uploads ?? ComposerAttachments()
        draft = saved?.text ?? ""
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
}

@MainActor @Observable final class ToolDrawerState {
    /// Nil follows the progressive default; explicit choices survive updates.
    var expanded: Bool?
    var selectedTool: String?

    func isExpanded(count: Int, inProgress: Bool) -> Bool {
        expanded ?? (inProgress && (1...5).contains(count))
    }
}
