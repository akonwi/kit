import Foundation
import Observation

/// The one source of the terminals' appearance: the Kit theme the window
/// shows and the mono font from Settings › Typography. Once followed, it
/// reports every change to them, whether or not any terminal is on screen:
/// the system turning light or dark, the appearance and theme choices, a
/// theme preview, a reloaded themes folder, and the font.
@MainActor
final class TerminalTheme {
    static let shared = TerminalTheme()

    /// The shared themes folder, for installed themes.
    var library: NativeThemeLibrary?
    private let defaults: UserDefaults
    private let system: SystemAppearance
    private let typography: Typography
    private let preview: ThemePreview
    private var follower: (@MainActor (TerminalAppearance) -> Void)?
    private var reported: TerminalAppearance?
    private var preferences: PreferenceObserver?
    private var scheduled = false

    init(defaults: UserDefaults = .standard, system: SystemAppearance = .shared, typography: Typography = .shared,
         preview: ThemePreview = .shared, library: NativeThemeLibrary? = nil) {
        self.defaults = defaults
        self.system = system
        self.typography = typography
        self.preview = preview
        self.library = library
    }

    /// The appearance the settings call for now, as the windows resolve it.
    var current: TerminalAppearance {
        let scheme = system.resolve(defaults.string(forKey: "appearance") ?? "system")
        let theme = ThemeConfiguration.decode(defaults.string(forKey: "themeConfiguration") ?? "")
            .resolved(dark: scheme == .dark, installed: library?.themes ?? [], preview: preview)
        return TerminalAppearance(theme: theme, typography: typography)
    }

    /// Calls `change` with each new appearance from now on. There is one
    /// follower: the terminal runtime.
    func follow(_ change: @escaping @MainActor (TerminalAppearance) -> Void) {
        follower = change
        reported = current
        if preferences == nil {
            preferences = PreferenceObserver(defaults, keys: ["appearance", "themeConfiguration"]) { [weak self] in
                self?.schedule()
            }
        }
        track()
    }

    /// Reads the appearance while observing what it depends on, and reports
    /// it if it changed.
    private func track() {
        let appearance = withObservationTracking { current } onChange: { [weak self] in
            Task { @MainActor in self?.schedule() }
        }
        guard appearance != reported else { return }
        reported = appearance
        follower?(appearance)
    }

    /// Tracks again once the change in progress has finished, so changes
    /// made together are reported once.
    private func schedule() {
        guard !scheduled else { return }
        scheduled = true
        DispatchQueue.main.async { [weak self] in
            MainActor.assumeIsolated {
                guard let self else { return }
                self.scheduled = false
                self.track()
            }
        }
    }
}

/// Calls back when any of some user defaults keys changes in this process.
private final class PreferenceObserver: NSObject {
    private let defaults: UserDefaults
    private let keys: [String]
    private let changed: @MainActor @Sendable () -> Void

    init(_ defaults: UserDefaults, keys: [String], changed: @escaping @MainActor @Sendable () -> Void) {
        self.defaults = defaults
        self.keys = keys
        self.changed = changed
        super.init()
        for key in keys { defaults.addObserver(self, forKeyPath: key, options: [], context: nil) }
    }

    deinit {
        for key in keys { defaults.removeObserver(self, forKeyPath: key) }
    }

    override func observeValue(forKeyPath keyPath: String?, of object: Any?, change: [NSKeyValueChangeKey: Any]?,
                               context: UnsafeMutableRawPointer?) {
        let changed = changed
        if Thread.isMainThread {
            MainActor.assumeIsolated { changed() }
        } else {
            DispatchQueue.main.async { MainActor.assumeIsolated { changed() } }
        }
    }
}
