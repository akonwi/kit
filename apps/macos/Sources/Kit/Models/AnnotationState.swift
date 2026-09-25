import Foundation
import Observation

/// UI drafts survive failed writes. Records only come from server reads or ordered events.
@MainActor @Observable final class AnnotationState {
    private(set) var records: [FileAnnotation] = []
    private(set) var pending = false
    private(set) var error: String?
    private(set) var uncertain = false
    private var observed: [FileAnnotation] = []
    private var generation = 0
    private var acknowledgedCreation: FileAnnotation?
    var draft = ""
    var revealed: UInt64?
    var revealToken = UUID()
    var editor: Editor?
    var replacement: FileAnnotation?
    var inspected: FileAnnotation?
    var selectionDraft: SelectionDraft?
    struct SelectionDraft {
        let body: String
        let replacing: UInt64?
        let path: String
    }
    func reselectEditor() {
        guard !pending, let editor, let path = editor.anchor.workspaceFile?.path ?? editor.anchor.workingTreeDiff?.path else { return }
        selectionDraft = SelectionDraft(body: draft, replacing: acknowledgedCreation?.id ?? editor.editing ?? editor.replacing, path: path)
        self.editor = nil
    }
    struct Editor {
        let token = UUID()
        let anchor: WireAnnotationAnchor
        let editing: UInt64?
        let replacing: UInt64?
    }

    func begin(anchor: WireAnnotationAnchor, editing: FileAnnotation? = nil) {
        guard !pending, !uncertain, editor == nil else { return }
        guard editing != nil || records.count < 128 else {
            error = "A session can contain up to 128 annotations. Delete one before adding another."
            return
        }
        acknowledgedCreation = nil
        if let editing { draft = editing.body }
        else if let selectionDraft { draft = selectionDraft.body }
        else if let replacement { draft = replacement.body }
        else { draft = "" }
        editor = Editor(anchor: anchor, editing: editing?.id, replacing: selectionDraft?.replacing ?? replacement?.id)
    }

    func cancelEditor() {
        guard !pending else { return }
        editor = nil; replacement = nil; selectionDraft = nil; draft = ""; acknowledgedCreation = nil
    }

    func reveal(_ note: FileAnnotation) {
        if note.isDiff { inspected = note; return }
        revealed = note.id; revealToken = UUID()
        if note.stale { inspected = note }
    }

    func observe(_ incoming: [FileAnnotation]) {
        guard incoming != observed else { return }
        observed = incoming
        generation += 1
        records = incoming
    }

    func refresh(client: any AnnotationClient, session: String) async throws {
        let epoch = generation
        let values = try await client.annotations(session)
        try Task.checkCancellation()
        guard generation == epoch else { return }
        records = values
    }

    /// No retry loop: an uncertain create must be reconciled and reviewed before another write.
    func save(client: any AnnotationClient, session: String, anchor: WireAnnotationAnchor,
              editing: UInt64? = nil, replacing: UInt64? = nil) async -> Bool {
        guard !pending, !uncertain, FileAnnotation.validBody(draft) else { return false }
        pending = true; error = nil
        defer { pending = false }
        var acknowledged = false
        do {
            if let editing {
                _ = try await client.updateAnnotation(session, input: .init(annotationId: editing, body: draft))
            } else if let created = acknowledgedCreation {
                if created.body != draft {
                    acknowledgedCreation = try await client.updateAnnotation(session, input: .init(annotationId: created.id, body: draft))
                }
            } else {
                acknowledgedCreation = try await client.createAnnotation(session, input: .init(anchor: anchor, body: draft))
            }
            acknowledged = true
            // Remove the stale record only after a definite creation acknowledgement.
            if let replacing { try await client.deleteAnnotation(session, id: replacing) }
            try await refresh(client: client, session: session)
            draft = ""
            editor = nil; replacement = nil; selectionDraft = nil; acknowledgedCreation = nil
            return true
        } catch {
            self.error = "Couldn’t save the annotation. Your comment is preserved. " + error.localizedDescription
            uncertain = acknowledged || Self.ambiguous(error)
            try? await refresh(client: client, session: session)
            return false
        }
    }

    func remove(client: any AnnotationClient, session: String, id: UInt64) async {
        guard !pending else { return }
        pending = true; error = nil
        defer { pending = false }
        do {
            try await client.deleteAnnotation(session, id: id)
            try await refresh(client: client, session: session)
        } catch {
            self.error = "Couldn’t delete the annotation. " + error.localizedDescription
            try? await refresh(client: client, session: session)
        }
    }

    func report(_ error: Error) { self.error = error.localizedDescription }

    func acknowledgeUncertainty() { uncertain = false; error = nil }

    private static func ambiguous(_ error: Error) -> Bool {
        if error is MutationNotSent { return false }
        if case ClientError.http(let status) = error, (400..<500).contains(status) { return false }
        return true
    }
}
