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
        /// Which of the matching words in the scope were selected.
        var locator: TextCommentLocator? = nil
        /// The tab the words were selected in, to show them from the chip.
        var pane: WorkspacePane? = nil

        /// The words' mark.
        var mark: TextCommentMark { TextCommentMark(id: id, quote: annotation.quote, locator: locator) }
    }

    private(set) var items: [Item] = []
    var error: String?
    /// The comment box at a selection or a mark.
    @ObservationIgnored let popover = AnchoredPopover()
    /// The views showing words comments can be on, which bring them into view.
    @ObservationIgnored private let revealers = NSHashTable<AnyObject>.weakObjects()
    /// Counts chip clicks, so a later one wins over one still bringing its
    /// words into view.
    @ObservationIgnored var revealing = 0

    init() { TextCommentDrafts.register(self) }

    var isEmpty: Bool { items.isEmpty }

    /// Adds a comment on quote, selected in source. Returns the new item's
    /// ID, or nil when the message holds all it can or there's nothing to
    /// add.
    @discardableResult
    func add(comment: String, source: String, quote: String, scope: String?, locator: TextCommentLocator? = nil,
             pane: WorkspacePane? = nil) -> UUID? {
        let comment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !comment.isEmpty, !TextAnnotationText.trimmedQuote(quote).text.isEmpty else { return nil }
        guard items.count < TextAnnotationText.limit else {
            error = "A message can contain up to \(TextAnnotationText.limit) comments on selected text."
            return nil
        }
        error = nil
        let item = Item(id: UUID(), annotation: TextAnnotation(comment: comment, source: source, quote: quote), scope: scope,
                        locator: locator, pane: pane)
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
        items.filter { $0.scope == scope }.map(\.mark)
    }

    /// Adds a view that shows words comments can be on, for chips to bring
    /// them into view. It's held weakly.
    func register(_ revealer: TextCommentRevealer) { revealers.add(revealer) }
    func unregister(_ revealer: TextCommentRevealer) { revealers.remove(revealer) }

    /// The view in pane, or any when it's nil, showing scope's words, if
    /// one does.
    func revealer(for scope: String, in pane: WorkspacePane?) -> TextCommentRevealer? {
        revealers.allObjects.lazy.compactMap { $0 as? TextCommentRevealer }
            .first { $0.showsTextComments(in: scope, pane: pane) }
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
    var locator: TextCommentLocator? = nil

    static let scheme = "kit-text-comment"

    /// The link a mark's words carry, which reopens its comment.
    var url: URL { URL(string: "\(Self.scheme):\(id.uuidString)")! }

    /// The comment a mark's link reopens.
    static func id(in url: URL) -> UUID? {
        guard url.scheme == scheme else { return nil }
        return UUID(uuidString: String(url.absoluteString.dropFirst(scheme.count + 1)))
    }

    /// Where the quote is in text: the occurrence the locator names when
    /// text is what it was selected in; without a locator, every
    /// occurrence of each of its pieces. UTF-16 ranges.
    func ranges(in text: String) -> [NSRange] {
        let string = text as NSString
        func all(_ piece: String) -> [NSRange] {
            var found: [NSRange] = []
            var from = 0
            while from < string.length {
                let range = string.range(of: piece, options: [], range: NSRange(location: from, length: string.length - from))
                guard range.location != NSNotFound, range.length > 0 else { break }
                found.append(range)
                from = NSMaxRange(range)
            }
            return found
        }
        guard let locator else { return pieces.flatMap(all) }
        guard locator.context == text else { return [] }
        let matches = all(quote)
        return matches.indices.contains(locator.occurrence) ? [matches[locator.occurrence]] : []
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

/// Which of the places some words appear were selected: the whole text they
/// were selected in, and how many times the same words come before them.
struct TextCommentLocator: Equatable, Hashable {
    let context: String
    let occurrence: Int

    /// The locator for range of text, or nil when the range doesn't hold
    /// quote.
    init?(text: String, range: NSRange, quote: String) {
        let string = text as NSString
        guard NSMaxRange(range) <= string.length, string.substring(with: range) == quote, !quote.isEmpty else { return nil }
        var count = 0, from = 0
        while from < range.location {
            let found = string.range(of: quote, options: [], range: NSRange(location: from, length: string.length - from))
            guard found.location != NSNotFound, found.location < range.location else { break }
            count += 1
            from = NSMaxRange(found)
        }
        context = text
        occurrence = count
    }

    init(context: String, occurrence: Int) {
        self.context = context; self.occurrence = occurrence
    }
}
