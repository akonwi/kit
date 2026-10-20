import Foundation
import Observation

/// How this app presents diffs: a layout and whether lines wrap. They are
/// app preferences, kept apart from any server's settings, and every Diff
/// pane follows them as they change.
@MainActor @Observable
final class DiffPreferences {
    static let shared = DiffPreferences()
    static let layoutKey = "diffLayout"
    static let wrapLinesKey = "diffWrapLines"

    var layout: DiffLayout { didSet { defaults.set(layout == .split ? "split" : "unified", forKey: Self.layoutKey) } }
    var wrapLines: Bool { didSet { defaults.set(wrapLines, forKey: Self.wrapLinesKey) } }
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        layout = defaults.string(forKey: Self.layoutKey) == "split" ? .split : .unified
        wrapLines = defaults.bool(forKey: Self.wrapLinesKey)
    }
}
