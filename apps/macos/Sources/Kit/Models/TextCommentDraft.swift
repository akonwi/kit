import Foundation
import KitTranscript
import Observation

/// The comments on selected text waiting in a session's composer
/// (ADR 0070). Each holds the comment, the words, where they came from,
/// and the scope they were selected in, so the words stay marked there
/// until the message is sent.
@MainActor @Observable final class TextCommentDraft {
    struct Item: Identifiable, Equatable {
        let id: UUID
        var annotation: TextAnnotation
        /// What the words were selected in, such as a transcript message's
        /// ID, for marking them; nil where they aren't marked.
        let scope: String?
    }

    private(set) var items: [Item] = []
    var error: String?
    /// The comment box at a selection or a mark.
    @ObservationIgnored let popover = AnchoredPopover()

    init() { TextCommentDrafts.register(self) }

    var isEmpty: Bool { items.isEmpty }

    /// Adds a comment on quote, selected in source. Returns the new item's
    /// ID, or nil when the message holds all it can or there's nothing to
    /// add.
    @discardableResult
    func add(comment: String, source: String, quote: String, scope: String?) -> UUID? {
        let comment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !comment.isEmpty, !TextAnnotationText.trimmedQuote(quote).text.isEmpty else { return nil }
        guard items.count < TextAnnotationText.limit else {
            error = "A message can contain up to \(TextAnnotationText.limit) comments on selected text."
            return nil
        }
        error = nil
        let item = Item(id: UUID(), annotation: TextAnnotation(comment: comment, source: source, quote: quote), scope: scope)
        items.append(item)
        return item.id
    }

    func item(_ id: UUID) -> Item? { items.first { $0.id == id } }

    /// Replaces a waiting comment's text; an empty one removes it.
    func update(_ id: UUID, comment: String) {
        let comment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        if comment.isEmpty { remove(id); return }
        items[index].annotation.comment = comment
    }

    func remove(_ id: UUID) {
        items.removeAll { $0.id == id }
        if items.isEmpty { error = nil }
    }

    /// The waiting comments selected in scope, to mark their words.
    func marks(in scope: String) -> [TextCommentMark] {
        items.filter { $0.scope == scope }.map { TextCommentMark(id: $0.id, quote: $0.annotation.quote) }
    }

    /// The blocks the model gets after the message's text, or "" for none.
    func messageText() -> String { TextAnnotationText.text(items.map(\.annotation)) }

    /// Removes the comments a sent message carried.
    func acknowledge(_ ids: [UUID]) {
        for id in ids { remove(id) }
    }
}

/// Words a waiting comment is on, as the text they were selected in marks
/// them.
struct TextCommentMark: Equatable, Hashable {
    let id: UUID
    let quote: String

    static let scheme = "kit-text-comment"

    /// The link a mark's words carry, which reopens its comment.
    var url: URL { URL(string: "\(Self.scheme):\(id.uuidString)")! }

    /// The comment a mark's link reopens.
    static func id(in url: URL) -> UUID? {
        guard url.scheme == scheme else { return nil }
        return UUID(uuidString: String(url.absoluteString.dropFirst(scheme.count + 1)))
    }

    /// The pieces of the quote to look for in a run of text: each of its
    /// lines, trimmed, since a selection across blocks is split across
    /// views.
    var pieces: [String] {
        quote.split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { $0.count >= 2 }
    }
}
