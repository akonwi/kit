import Foundation
import Testing
@testable import Kit

/// A `TerminalTheme` on its own defaults, system appearance, typography, and
/// preview, recording what it reports.
@MainActor
final class TerminalThemeHarness {
    let defaults = TestDefaults.make()
    let system = SystemAppearance(scheme: .light)
    let typography: Typography
    let preview = ThemePreview()
    let theme: TerminalTheme
    private(set) var reported: [TerminalAppearance] = []

    init(appearance: String = "system") {
        defaults.set(appearance, forKey: "appearance")
        typography = Typography(defaults: defaults)
        theme = TerminalTheme(defaults: defaults, system: system, typography: typography, preview: preview)
    }

    func follow() { theme.follow { [weak self] in self?.reported.append($0) } }

    /// The appearance Kit's own theme gives terminals in one mode.
    func kit(dark: Bool) -> TerminalAppearance {
        TerminalAppearance(theme: ThemeConfiguration().theme(dark: dark), typography: typography)
    }
}

@MainActor
struct TerminalThemeTests {
    @Test func startsInTheThemeTheWindowsResolve() {
        let harness = TerminalThemeHarness()
        #expect(harness.theme.current == harness.kit(dark: false))
        harness.system.update(.dark)
        #expect(harness.theme.current == harness.kit(dark: true))
        #expect(harness.kit(dark: true).background != harness.kit(dark: false).background)
    }

    @Test func followsTheSystemTurningDarkAndLightWhileSetToSystem() async throws {
        let harness = TerminalThemeHarness(appearance: "system")
        harness.follow()
        harness.system.update(.dark)
        try await eventually { harness.reported.count == 1 }
        harness.system.update(.light)
        try await eventually { harness.reported.count == 2 }
        #expect(harness.reported == [harness.kit(dark: true), harness.kit(dark: false)])
    }

    @Test func followsTheAppearanceChosenInSettings() async throws {
        let harness = TerminalThemeHarness(appearance: "system")
        harness.follow()
        harness.defaults.set("dark", forKey: "appearance")
        try await eventually { harness.reported.count == 1 }
        #expect(harness.reported == [harness.kit(dark: true)])
    }

    @Test func followsTheThemeChosenForTheCurrentMode() async throws {
        let harness = TerminalThemeHarness(appearance: "light")
        harness.follow()
        var configuration = ThemeConfiguration()
        configuration.light = ThemeConfiguration.kitDark
        harness.defaults.set(configuration.json, forKey: "themeConfiguration")
        try await eventually { harness.reported.count == 1 }
        #expect(harness.reported == [harness.kit(dark: true)])
    }

    @Test func followsAThemePreview() async throws {
        let harness = TerminalThemeHarness(appearance: "light")
        harness.follow()
        harness.preview.choice = .init(id: ThemeConfiguration.kitDark, dark: false)
        try await eventually { harness.reported.count == 1 }
        harness.preview.choice = nil
        try await eventually { harness.reported.count == 2 }
        #expect(harness.reported == [harness.kit(dark: true), harness.kit(dark: false)])
    }

    @Test func followsTheCodeFont() async throws {
        let harness = TerminalThemeHarness()
        harness.follow()
        harness.typography.monoFamily = "Menlo"
        try await eventually { harness.reported.count == 1 }
        harness.typography.monoSize = 15
        try await eventually { harness.reported.count == 2 }
        #expect(harness.reported.map(\.fontFamily) == ["Menlo", "Menlo"])
        #expect(harness.reported.map(\.fontSize) == [12, 15])
    }

    @Test func reportsChangesMadeTogetherOnce() async throws {
        let harness = TerminalThemeHarness(appearance: "system")
        harness.follow()
        harness.system.update(.dark)
        harness.typography.monoFamily = "Menlo"
        try await eventually { harness.reported.count == 1 }
        var expected = harness.kit(dark: true)
        expected.fontFamily = "Menlo"
        #expect(harness.reported == [expected])
    }
}
