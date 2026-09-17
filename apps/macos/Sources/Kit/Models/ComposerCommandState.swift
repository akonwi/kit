import Foundation
import Observation

@MainActor @Observable final class ComposerCommandState {
    var catalog: [PromptCommand] = []
    private(set) var isOpen = false
    private(set) var query = ""
    private(set) var selection = 0
    var matches: [PromptCommand] {
        catalog.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) || $0.description.localizedCaseInsensitiveContains(query) }
    }
    var selected: PromptCommand? { matches.indices.contains(selection) ? matches[selection] : nil }
    var visibleMatches: [PromptCommand] {
        let first = max(0, selection - 5)
        return Array(matches.dropFirst(first).prefix(6))
    }
    var range: NSRange { NSRange(location: 0, length: ("/" + query).utf16.count) }
    func close() { isOpen = false }
    func observe(text: String, caret: NSRange, pasted: Bool) {
        guard !pasted, text.hasPrefix("/"), !text.dropFirst().contains(where: { $0.isWhitespace }),
              caret.length == 0, caret.location == text.utf16.count else { close(); return }
        let query = String(text.dropFirst())
        if self.query != query { selection = 0 }
        self.query = query
        isOpen = !matches.isEmpty
        selection = min(selection, max(0, matches.count - 1))
    }
    func move(_ offset: Int) {
        guard !matches.isEmpty else { return }
        selection = (selection + offset + matches.count) % matches.count
    }
}
