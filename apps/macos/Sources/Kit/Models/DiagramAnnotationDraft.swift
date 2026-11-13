import Foundation
import KitMermaid

/// The diagram annotations waiting in a session's composer. On send each
/// becomes a text block after the message (ADR 0046's annotations); nothing
/// is stored on the server until the message is sent.
@MainActor @Observable
final class DiagramAnnotationDraft {
    struct Item: Identifiable, Equatable {
        let id: UUID
        let annotation: DiagramAnnotation
        /// The part, as the model's text names it.
        let summary: String
        /// What Kit read from the diagram about the part.
        let facts: [String]
    }
    private(set) var items: [Item] = []
    var error: String?

    /// The most annotations one message holds.
    static let limit = 16

    var isEmpty: Bool { items.isEmpty }

    /// Adds a comment on a part of a diagram's source. Returns false when
    /// the message holds all it can or the part isn't in the diagram.
    @discardableResult
    func add(comment: String, source: String, part: Part,
             nonce: String = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()) -> Bool {
        guard items.count < Self.limit else {
            error = "A message can contain up to \(Self.limit) diagram annotations."
            return false
        }
        let annotation = DiagramAnnotation(comment: comment, source: source, part: part, nonce: nonce)
        guard let facts = try? DiagramAnnotationText.describe(source, part),
              let summary = try? DiagramAnnotationText.summary(source, part) else {
            error = "That part of the diagram can’t be annotated."
            return false
        }
        error = nil
        items.append(Item(id: UUID(), annotation: annotation, summary: summary, facts: facts))
        return true
    }

    func remove(_ id: UUID) {
        items.removeAll { $0.id == id }
        if items.isEmpty { error = nil }
    }

    /// The parts of source's diagram that have a waiting annotation.
    func parts(of source: String) -> [Part] {
        items.filter { $0.annotation.source == source }.map(\.annotation.part)
    }

    /// The blocks the model gets after the message's text, or "" for none.
    func messageText() -> String {
        (try? DiagramAnnotationText.text(items.map(\.annotation))) ?? ""
    }

    /// Removes the annotations a sent message carried.
    func acknowledge(_ ids: [UUID]) {
        for id in ids { remove(id) }
    }
}
