import Foundation
import Testing
@testable import Kit

@MainActor
struct ThemeConfigurationTests {
    @Test func importsKeepSeparateLightAndDarkAssignments() throws {
        var configuration = ThemeConfiguration()
        let light = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"#FFFCF0","textPrimary":"#100F0F"}}"##.utf8))
        let dark = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"#100F0F","textPrimary":"#CECDC3"}}"##.utf8))
        configuration.add(name: "Flexoki light", definition: light, fallbackDark: true)
        let lightID = configuration.light
        configuration.add(name: "Flexoki", definition: dark, fallbackDark: false)
        #expect(configuration.light == lightID)
        #expect(configuration.dark != lightID)
        #expect(configuration.imported.count == 2)
        #expect(!configuration.theme(dark: false).dark)
        #expect(configuration.theme(dark: true).dark)
        let restored = ThemeConfiguration.decode(configuration.json)
        #expect(restored.light == configuration.light)
        #expect(restored.dark == configuration.dark)
        #expect(restored.imported.count == 2)
    }

    @Test func migrationPreservesImportedThemeAndRunsOnlyOnce() throws {
        let suite = "ThemeConfigurationTests.\(UUID())"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set("custom", forKey: "themePalette")
        defaults.set("Flexoki", forKey: "customThemeName")
        defaults.set(##"{"tokens":{"bg":"#100F0F"}}"##, forKey: "customThemeJSON")
        ThemeConfiguration.migrate(in: defaults)
        let json = try #require(defaults.string(forKey: "themeConfiguration"))
        let migrated = ThemeConfiguration.decode(json)
        #expect(migrated.light == "mica")
        #expect(migrated.imported.first?.name == "Flexoki")
        #expect(migrated.dark == migrated.imported.first?.id)
        defaults.set("sand", forKey: "themePalette")
        ThemeConfiguration.migrate(in: defaults)
        #expect(defaults.string(forKey: "themeConfiguration") == json)
    }
}
