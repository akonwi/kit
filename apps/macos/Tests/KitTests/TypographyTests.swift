import AppKit
import Testing
@testable import Kit

@MainActor
struct TypographyTests {
    @Test func preferencesPersistAndReset() throws {
        let suite = "TypographyTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let typography = Typography(defaults: defaults)
        #expect(typography.interfaceFamily == "System")
        #expect(typography.monoFamily == "JetBrains Mono")
        typography.interfaceFamily = "Helvetica"
        typography.monoFamily = "Menlo"
        typography.interfaceSize = 15
        typography.monoSize = 16
        let restored = Typography(defaults: defaults)
        #expect(restored.interfaceFamily == "Helvetica")
        #expect(restored.monoFamily == "Menlo")
        #expect(restored.font(size: 12, mono: true).pointSize == 16)
        #expect(restored.font(size: 13).pointSize == 15)
        restored.reset()
        #expect(restored.interfaceFamily == "System")
        #expect(restored.monoFamily == "JetBrains Mono")
        #expect(restored.interfaceSize == 13)
        #expect(restored.monoSize == 12)
    }

    @Test func unavailableFontsFallBackToSystem() throws {
        let suite = "TypographyTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let typography = Typography(defaults: defaults)
        typography.monoFamily = "Unavailable Font Family"
        #expect(typography.font(size: 12, mono: true).fontName == NSFont.monospacedSystemFont(ofSize: 12, weight: .regular).fontName)
    }
}
