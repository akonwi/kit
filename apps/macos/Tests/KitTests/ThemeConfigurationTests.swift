import Foundation
import Testing
@testable import Kit

@MainActor
struct ThemeConfigurationTests {
    @Test func sharedThemesKeepSeparateLightAndDarkAssignments() throws {
        var configuration = ThemeConfiguration()
        let lightDefinition = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"#FFFCF0","textPrimary":"#100F0F"}}"##.utf8))
        let darkDefinition = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"#100F0F","textPrimary":"#CECDC3"}}"##.utf8))
        let light = InstalledTheme(name: "Flexoki light", definition: lightDefinition)
        let dark = InstalledTheme(name: "Flexoki", definition: darkDefinition)
        configuration.assign(light, fallbackDark: true)
        configuration.assign(dark, fallbackDark: false)

        #expect(configuration.light == light.id)
        #expect(configuration.dark == dark.id)
        #expect(!configuration.theme(dark: false, installed: [light, dark]).dark)
        #expect(configuration.theme(dark: true, installed: [light, dark]).dark)
        let restored = ThemeConfiguration.decode(configuration.json)
        #expect(restored.light == configuration.light)
        #expect(restored.dark == configuration.dark)
    }

    @Test func normalizationDropsSelectionsMissingFromSharedDirectory() throws {
        let definition = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"#100F0F"}}"##.utf8))
        let installed = InstalledTheme(name: "Flexoki", definition: definition)
        var configuration = ThemeConfiguration(light: "removed-import-id", dark: "global:temporarily-invalid")

        let normalized = configuration.normalize(installed: [installed])
        #expect(normalized)
        #expect(configuration.light == "mica")
        #expect(configuration.dark == "global:temporarily-invalid")
        let repeated = configuration.normalize(installed: [installed])
        #expect(!repeated)
    }

    @Test func migrationDeletesEmbeddedThemeDefinitionsAndLegacyKeys() throws {
        let suite = "ThemeConfigurationTests.\(UUID())"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set(##"{"light":"legacy-light-id","dark":"mica","imported":[{"id":"legacy-light-id","name":"Flexoki light","definition":{"tokens":{"bg":"#FFFCF0"}}}]}"##,
                     forKey: "themeConfiguration")
        defaults.set("custom", forKey: "themePalette")
        defaults.set("Flexoki light", forKey: "customThemeName")
        defaults.set(##"{"tokens":{"bg":"#FFFCF0"}}"##, forKey: "customThemeJSON")

        ThemeConfiguration.migrate(in: defaults)

        let json = try #require(defaults.string(forKey: "themeConfiguration"))
        let object = try #require(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
        #expect(object["imported"] == nil)
        #expect(object["light"] as? String == "legacy-light-id")
        #expect(object["dark"] as? String == "mica")
        #expect(defaults.object(forKey: "customThemeJSON") == nil)
        #expect(defaults.object(forKey: "customThemeName") == nil)
        #expect(defaults.object(forKey: "themePalette") == nil)
    }
}
