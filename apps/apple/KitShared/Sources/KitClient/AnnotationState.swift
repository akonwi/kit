import Foundation
import Observation

/// A session's annotations and the one being written. UI drafts survive
/// failed writes. Records only come from server reads or ordered events.
@MainActor @Observable public final class AnnotationState {
    public private(set) var records: [FileAnnotation] = []
    public private(set) var pending = false
    public private(set) var error: String?
    public private(set) var evidenceConflict: AnnotationEvidenceConflict?
    public private(set) var uncertain = false
    private var observed: [FileAnnotation] = []
    private var generation = 0
    private var acknowledgedCreation: FileAnnotation?
    public var draft = ""
    public var revealed: UInt64?
    public var revealToken = UUID()
    public var editor: Editor?
    public var replacement: FileAnnotation?
    public var inspected: FileAnnotation?
    public var selectionDraft: SelectionDraft?

    public init() {}

    public struct SelectionDraft {
        public let body: String
        public let replacing: UInt64?
        public let path: String
    }
    public func reselectEditor() {
        guard !pending else { return }
        stageReselection()
    }
    private func stageReselection() {
        guard let editor, let path = editor.anchor.workspaceFile?.path ?? editor.anchor.workingTreeDiff?.path else { return }
        selectionDraft = SelectionDraft(body: draft, replacing: acknowledgedCreation?.id ?? editor.editing ?? editor.replacing, path: path)
        self.editor = nil
    }
    public struct Editor {
        public let token = UUID()
        public let anchor: WireAnnotationAnchor
        public let editing: UInt64?
        public let replacing: UInt64?
    }

    public func begin(anchor: WireAnnotationAnchor, editing: FileAnnotation? = nil) {
        guard !pending, !uncertain, editor == nil else { return }
        guard editing != nil || records.count < 128 else {
            error = "A session can contain up to 128 annotations. Delete one before adding another."
            return
        }
        acknowledgedCreation = nil
        evidenceConflict = nil; error = nil
        if let editing { draft = editing.body }
        else if let selectionDraft { draft = selectionDraft.body }
        else if let replacement { draft = replacement.body }
        else { draft = "" }
        editor = Editor(anchor: anchor, editing: editing?.id, replacing: selectionDraft?.replacing ?? replacement?.id)
    }

    public func cancelEditor() {
        guard !pending else { return }
        editor = nil; replacement = nil; selectionDraft = nil; draft = ""; acknowledgedCreation = nil; evidenceConflict = nil
    }

    public func reveal(_ note: FileAnnotation) {
        if note.isDiff { inspected = note; return }
        revealed = note.id; revealToken = UUID()
        if note.stale { inspected = note }
    }

    public func observe(_ incoming: [FileAnnotation]) {
        guard incoming != observed else { return }
        observed = incoming
        generation += 1
        records = incoming
    }

    public func refresh(client: any AnnotationClient, session: String) async throws {
        let epoch = generation
        let values = try await client.annotations(session)
        try Task.checkCancellation()
        guard generation == epoch else { return }
        records = values
    }

    /// No retry loop: an uncertain create must be reconciled and reviewed before another write.
    public func save(client: any AnnotationClient, session: String, anchor: WireAnnotationAnchor,
              editing: UInt64? = nil, replacing: UInt64? = nil) async -> Bool {
        guard !pending, !uncertain, FileAnnotation.validBody(draft) else { return false }
        pending = true; error = nil; evidenceConflict = nil
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
            if let conflict = error as? AnnotationEvidenceConflict {
                evidenceConflict = conflict
                stageReselection()
            }
            self.error = "Couldn’t save the annotation. Your comment is preserved. " + error.localizedDescription
            uncertain = acknowledged || Self.ambiguous(error)
            try? await refresh(client: client, session: session)
            return false
        }
    }

    public func remove(client: any AnnotationClient, session: String, id: UInt64) async {
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

    public func report(_ error: Error) { self.error = error.localizedDescription }

    public func acknowledgeUncertainty() { uncertain = false; error = nil }

    private static func ambiguous(_ error: Error) -> Bool {
        if error is MutationNotSent || error is AnnotationEvidenceConflict { return false }
        if case ClientError.http(let status) = error, (400..<500).contains(status) { return false }
        return true
    }
}
