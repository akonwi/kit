import Foundation
import KitClient
import Observation

/// A place in the text a find bar searches: an offset, in UTF-16 units, into
/// one of its pieces. A code file is one piece; a rendered Markdown file has
/// a piece for each text its rows show (see `MarkdownFind`).
struct FindPosition: Comparable, Hashable, Sendable {
    var piece: Int
    var offset: Int

    static let start = FindPosition(piece: 0, offset: 0)

    static func < (a: Self, b: Self) -> Bool { (a.piece, a.offset) < (b.piece, b.offset) }
}

/// A match a find bar found: a UTF-16 range in one piece.
struct FindMatch: Equatable, Sendable {
    var piece: Int
    var range: Range<Int>
    var start: FindPosition { FindPosition(piece: piece, offset: range.lowerBound) }
    var nsRange: NSRange { NSRange(location: range.lowerBound, length: range.count) }
}

/// One file tab's find bar: what it searches, its query and options, what
/// it found, and which match is current. Views supply the text and carry
/// out the requests it counts (focus the field, show the current match,
/// give the keyboard back to the file).
@MainActor @Observable
final class FileFind {
    /// What a file tab shows, which decides what its pieces are.
    enum Corpus: Equatable, Sendable { case none, source, rendered }

    private(set) var isOpen = false
    var query = "" { didSet { if query != oldValue { searchChanged() } } }
    var matchCase = false { didSet { if matchCase != oldValue { searchChanged() } } }
    var wholeWord = false { didSet { if wholeWord != oldValue { searchChanged() } } }
    /// The lines a cut-short file arrived with, or 0 when it arrived whole.
    var searchedLines = 0

    /// The lines of `source`, a file that arrived cut short, or 0 for one
    /// that arrived whole.
    nonisolated static func searchedLines(_ source: String?, truncated: Bool) -> Int {
        guard truncated, let source, !source.isEmpty else { return 0 }
        var lines = 0
        for unit in source.utf8 where unit == 0x0A { lines += 1 }
        return source.utf8.last == 0x0A ? lines : lines + 1
    }

    private(set) var corpus = Corpus.none
    private(set) var pieces: [String] = []
    /// Counts changes to `pieces`, so a search knows when to run again.
    private(set) var piecesVersion = 0

    private(set) var matches: [FindMatch] = []
    private(set) var capped = false
    private(set) var current: Int?
    /// Counts searches finished, so views can tell a fresh result apart.
    private(set) var searches = 0

    /// Asks the bar's field to take the keyboard and select its text.
    private(set) var focusRequest = 0
    /// Asks the file's view to show the current match.
    private(set) var revealRequest = 0
    /// Asks the file's view to take the keyboard back, the bar having closed.
    private(set) var dismissRequest = 0

    /// Where a search starts looking when nothing else says: the top of
    /// what the file's view shows. Set by the view.
    @ObservationIgnored var visibleAnchor: @MainActor () -> FindPosition = { .start }
    /// The text selected in the file's view, for Use Selection for Find.
    @ObservationIgnored var selectedText: @MainActor () -> (text: String, start: FindPosition)? = { nil }

    /// Where the next search starts instead of the current match or the
    /// view: a used selection, or where Find Next resumes after closing.
    @ObservationIgnored private var pendingAnchor: FindPosition?
    /// Where the current match was when the bar closed.
    @ObservationIgnored private var resumeAnchor: FindPosition?
    /// Steps to take once the next search finishes, from Find Next or
    /// Previous while the bar was closed.
    @ObservationIgnored private var pendingStep = 0

    var currentMatch: FindMatch? { current.map { matches[$0] } }

    /// What the bar's count reads; empty with nothing typed.
    var label: String {
        guard !query.isEmpty else { return "" }
        return TextFind.label(current: current.map { $0 + 1 } ?? 0, total: matches.count, capped: capped,
                              searchedLines: searchedLines)
    }

    /// What a search runs on: changes to any of it search again.
    struct Request: Equatable, Sendable {
        var query: String
        var options: TextFind.Options
        var piecesVersion: Int
    }
    var request: Request {
        Request(query: query, options: .init(matchCase: matchCase, wholeWord: wholeWord), piecesVersion: piecesVersion)
    }

    /// Opens the bar, or with it open, selects its field's text again. The
    /// last query comes back, searched from the top of what's on screen.
    func open() {
        if !isOpen { isOpen = true }
        focusRequest += 1
    }

    /// Closes the bar. The current match stays selected in the file, and
    /// Find Next or Previous resumes from it.
    func close() {
        guard isOpen else { return }
        resumeAnchor = currentMatch?.start
        isOpen = false
        current = nil
        dismissRequest += 1
    }

    /// Moves to the next match, wrapping. With the bar closed, reopens it
    /// on the last query and steps from where it left off.
    func next() { step(1) }

    /// Moves to the previous match, wrapping.
    func previous() { step(-1) }

    /// Searches for the text selected in the file, from where it starts.
    func useSelection() {
        guard let selection = selectedText() else { return }
        let line = selection.text.split(whereSeparator: \.isNewline).first.map(String.init) ?? ""
        guard !line.isEmpty else { return }
        pendingAnchor = selection.start
        current = nil
        query = line
        focusRequest += isOpen ? 1 : 0
    }

    /// Whether the selection is something Use Selection for Find can use.
    var canUseSelection: Bool { selectedText()?.text.isEmpty == false }

    /// Sets the text searched. A change of what's shown, as from rendered
    /// Markdown to its source, forgets the current match, whose place
    /// meant something only in the old pieces.
    func setPieces(_ pieces: [String], corpus: Corpus) {
        guard pieces != self.pieces || corpus != self.corpus else { return }
        if corpus != self.corpus {
            current = nil
            pendingAnchor = nil
            resumeAnchor = nil
        }
        self.corpus = corpus
        self.pieces = pieces
        piecesVersion += 1
    }

    /// Runs the search `request` describes, off the main actor, and picks
    /// the current match: the first at or after where the last one was, a
    /// used selection, or the top of the view.
    func refresh() async {
        let request = request
        let pieces = pieces
        let anchor = pendingAnchor ?? currentMatch?.start ?? visibleAnchor()
        let found = await Task.detached(priority: .userInitiated) {
            Self.search(pieces, query: request.query, options: request.options)
        }.value
        guard !Task.isCancelled, request == self.request else { return }
        pendingAnchor = nil
        matches = found.matches
        capped = found.capped
        current = Self.following(found.matches, anchor: anchor)
        if let index = current, pendingStep != 0 {
            current = (index + pendingStep % matches.count + matches.count) % matches.count
        }
        pendingStep = 0
        searches += 1
        if current != nil { revealRequest += 1 }
    }

    private func step(_ by: Int) {
        guard !query.isEmpty else { open(); return }
        if !isOpen {
            isOpen = true
            pendingAnchor = pendingAnchor ?? resumeAnchor
            pendingStep = by
            return
        }
        guard let index = current, !matches.isEmpty else { return }
        current = (index + by + matches.count) % matches.count
        revealRequest += 1
    }

    /// Clears what was found once the query is gone. Otherwise the current
    /// match stays until the next search, which starts from it, so typing
    /// more of a word keeps its place.
    private func searchChanged() {
        if query.isEmpty {
            matches = []
            capped = false
            current = nil
        }
    }

    /// The matches of `query` in `pieces`, in order, counting at most the
    /// limit across them all. No match spans two pieces.
    nonisolated static func search(_ pieces: [String], query: String, options: TextFind.Options)
        -> (matches: [FindMatch], capped: Bool) {
        var matches: [FindMatch] = []
        let limit = options.limit > 0 ? options.limit : TextFind.defaultLimit
        for (index, piece) in pieces.enumerated() {
            let remaining = limit - matches.count
            if remaining == 0 {
                // Past the limit, one more match anywhere caps the count.
                var probe = options
                probe.limit = 1
                if !TextFind.find(query, in: piece, options: probe).matches.isEmpty { return (matches, true) }
                continue
            }
            var options = options
            options.limit = remaining
            let result = TextFind.find(query, in: piece, options: options)
            matches += result.matches.map { FindMatch(piece: index, range: $0) }
            if result.capped { return (matches, true) }
        }
        return (matches, false)
    }

    /// The first match starting at or after `anchor`, the first when none
    /// does, or nil when there are none.
    nonisolated static func following(_ matches: [FindMatch], anchor: FindPosition) -> Int? {
        guard !matches.isEmpty else { return nil }
        var low = 0, high = matches.count
        while low < high {
            let middle = (low + high) / 2
            if matches[middle].start < anchor { low = middle + 1 } else { high = middle }
        }
        return low == matches.count ? 0 : low
    }
}
