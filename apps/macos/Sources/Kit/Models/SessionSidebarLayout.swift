import Foundation
import Observation

/// How wide one window's session sidebar is and whether it is hidden, for
/// the In Sidebar placement. Each window keeps its own, as restoration
/// intent; a new window starts with the last one set in any window.
struct SessionSidebarLayout: Codable, Equatable {
    static let defaultWidth: CGFloat = 248
    static let minimumWidth: CGFloat = 200
    static let maximumWidth: CGFloat = 400
    /// Letting go of a drag that would leave the sidebar narrower than this
    /// hides it instead.
    static let hidingWidth: CGFloat = 120
    /// How far one assistive-technology increment moves the edge.
    static let accessibilityStep: CGFloat = 20

    var width: CGFloat
    var hidden: Bool

    init(width: CGFloat = defaultWidth, hidden: Bool = false) {
        self.width = Self.clamped(width)
        self.hidden = hidden
    }

    /// A width within the sidebar's limits. A width that isn't a number is
    /// the default.
    static func clamped(_ width: CGFloat) -> CGFloat {
        guard width.isFinite else { return defaultWidth }
        return min(max(width, minimumWidth), maximumWidth)
    }

    private enum CodingKeys: String, CodingKey { case width, hidden }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(width: try container.decodeIfPresent(CGFloat.self, forKey: .width) ?? Self.defaultWidth,
                  hidden: try container.decodeIfPresent(Bool.self, forKey: .hidden) ?? false)
    }
}

/// The last sidebar layout set in any window, which a window without saved
/// intent of its own starts with.
struct SessionSidebarDefaults {
    static let widthKey = "sessionSidebarWidth"
    static let hiddenKey = "sessionSidebarHidden"
    let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func load() -> SessionSidebarLayout {
        let width = defaults.object(forKey: Self.widthKey) as? Double
        return SessionSidebarLayout(width: width.map { CGFloat($0) } ?? SessionSidebarLayout.defaultWidth,
                                    hidden: defaults.bool(forKey: Self.hiddenKey))
    }

    func save(_ layout: SessionSidebarLayout) {
        defaults.set(Double(layout.width), forKey: Self.widthKey)
        defaults.set(layout.hidden, forKey: Self.hiddenKey)
    }
}

/// A hidden sidebar shown over the tiles while the pointer rests on its
/// toggle or ⌘ is held. It opens once the pointer has stayed on the toggle
/// for `delay`, or at once when a ⌘ hold begins; stays while the pointer is
/// on the toggle or the sidebar, or the hold lasts; and closes once the
/// pointer has been off both for `delay`, or at once when the hold ends.
@MainActor @Observable
final class SessionSidebarPeek {
    static let delay: Duration = .milliseconds(300)
    private(set) var showing = false
    @ObservationIgnored private var onToggle = false
    @ObservationIgnored private var onSidebar = false
    @ObservationIgnored private var held = false
    @ObservationIgnored private var pending: Task<Void, Never>?
    @ObservationIgnored private let sleep: Sleeper

    init(sleep: @escaping Sleeper = realTimeSleeper) {
        self.sleep = sleep
    }

    /// The pointer entered or left the Show Sidebar toggle.
    func pointer(onToggle inside: Bool) {
        onToggle = inside
        update()
    }

    /// The pointer entered or left the peeking sidebar.
    func pointer(onSidebar inside: Bool) {
        onSidebar = inside
        update()
    }

    /// ⌘ began or stopped being held alone. The hold's own delay has passed,
    /// so the sidebar shows and goes without another.
    func command(held: Bool) {
        guard held != self.held else { return }
        self.held = held
        pending?.cancel()
        pending = nil
        showing = wanted
    }

    /// Closes the sidebar now, as choosing a tab or pressing Esc does. It
    /// opens again only when the pointer next comes to rest on the toggle,
    /// or ⌘ is next held.
    func close() {
        pending?.cancel()
        pending = nil
        onToggle = false
        onSidebar = false
        held = false
        showing = false
    }

    /// Whether a change is waiting out the delay.
    var isPending: Bool { pending != nil }

    /// The sidebar keeps itself open only once it is showing.
    private var wanted: Bool { onToggle || held || (showing && onSidebar) }

    private func update() {
        let wanted = wanted
        pending?.cancel()
        pending = nil
        guard wanted != showing else { return }
        pending = Task { [weak self, sleep] in
            do { try await sleep(Self.delay) } catch { return }
            guard let self, !Task.isCancelled else { return }
            self.pending = nil
            self.showing = wanted
        }
    }
}
