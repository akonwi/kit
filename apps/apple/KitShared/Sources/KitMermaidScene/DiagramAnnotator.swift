import Foundation
import KitMermaid
import Observation

/// A diagram's Annotate mode: the part under the pointer, the part picked,
/// and the comment being written on it. Both apps drive it from their own
/// pointer, tap and key handling.
@MainActor @Observable
public final class DiagramAnnotator {
    public private(set) var annotating = false
    public private(set) var hovered: Part?
    public private(set) var selected: Part?
    public var comment = ""

    public init() {}

    public func start() { annotating = true }

    public func stop() {
        annotating = false
        hovered = nil
        deselect()
    }

    public func toggle() { annotating ? stop() : start() }

    public func hover(_ part: Part?) { hovered = annotating ? part : nil }

    /// Picks the part clicked, or lets go of the picked one when the click
    /// lands on nothing. A new part starts a new comment.
    public func click(_ part: Part?) {
        guard annotating else { return }
        if part != selected { comment = "" }
        selected = part
    }

    public func deselect() {
        selected = nil
        comment = ""
    }

    /// Esc: lets go of the picked part, else leaves the mode.
    public func escape() {
        if selected != nil { deselect() } else { stop() }
    }

    /// Whether the comment has something to add.
    public var canCommit: Bool { selected != nil && !comment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    /// Adds the comment on the picked part of source's diagram to draft and
    /// lets go of the part. Returns false, keeping both, when it isn't added.
    @discardableResult
    public func commit(to draft: DiagramAnnotationDraft, source: String) -> Bool {
        guard canCommit, let selected, draft.add(comment: comment, source: source, part: selected) else { return false }
        deselect()
        return true
    }
}
