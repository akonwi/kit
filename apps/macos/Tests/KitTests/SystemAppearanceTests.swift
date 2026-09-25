import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor
struct SystemAppearanceTests {
    @Test func explicitOverrideReturnsDirectlyToSystem() {
        let appearance = SystemAppearance(scheme: .light)
        #expect(["dark", "system", "light", "dark", "system"].map(appearance.resolve) == [.dark, .light, .light, .dark, .light])
        appearance.update(.dark)
        #expect(["light", "system", "dark", "light", "system"].map(appearance.resolve) == [.light, .dark, .dark, .light, .dark])
    }

    @Test func systemChangesRespectExplicitChoices() {
        let appearance = SystemAppearance(scheme: .light)
        appearance.update(.dark)
        #expect(appearance.resolve("system") == .dark)
        #expect(appearance.resolve("light") == .light)
        appearance.update(.light)
        #expect(appearance.resolve("system") == .light)
        #expect(appearance.resolve("dark") == .dark)
    }

    @Test func appKitAppearanceResolvesBothSchemes() throws {
        #expect(SystemAppearance.colorScheme(try #require(NSAppearance(named: .aqua))) == .light)
        #expect(SystemAppearance.colorScheme(try #require(NSAppearance(named: .darkAqua))) == .dark)
        #expect(SystemAppearance.colorScheme(try #require(NSAppearance(named: .accessibilityHighContrastDarkAqua))) == .dark)
    }
}
