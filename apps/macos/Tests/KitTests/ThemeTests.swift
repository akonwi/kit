import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor
struct ThemeTests {
    @Test func partialThemeUsesMicaFallbacks() throws {
        let json = ##"{"tokens":{"bg":"#123456"},"syntaxPalette":{"keyword":"#abc"}}"##
        _ = try NativeThemeDefinition.parse(Data(json.utf8))
        let theme = MicaTheme(dark: false, palette: "custom", customJSON: json)
        let color = try #require(NSColor(theme.surface).usingColorSpace(.sRGB))
        #expect(abs(color.redComponent - 18.0 / 255) < 0.001)
        #expect(theme.dark)
        #expect(theme.text == MicaTheme(dark: true).text)
        #expect(theme.syntax("keyword", fallback: theme.text) == NativeThemeDefinition.hexColor("#aabbcc"))
    }

    @Test func invalidOverridesAreOmittedWithoutDiscardingTheTheme() throws {
        let definition = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"red","textPrimary":"transparent","future":"transparent","borderAccent":"#abc"},"syntaxPalette":{"text":"#0000","keyword":"#123456","comment":12}}"##.utf8))
        #expect(definition.tokens == ["future": "transparent", "borderAccent": "#abc"])
        #expect(definition.syntaxPalette == ["keyword": "#123456"])
        let empty = try NativeThemeDefinition.parse(Data("{}".utf8))
        #expect(empty.tokens == nil && empty.syntaxPalette == nil)
        for json in ["{", "[]"] {
            #expect(throws: (any Error).self) { try NativeThemeDefinition.parse(Data(json.utf8)) }
        }
    }

    @Test func duplicateKeysMatchRendererNeutralParsingRules() throws {
        #expect(throws: (any Error).self) {
            try NativeThemeDefinition.parse(Data(##"{"tokens":{},"tokens":{"bg":"#123456"}}"##.utf8))
        }
        let duplicateRole = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"#123456","bg":"#abcdef"},"syntaxPalette":{"keyword":"#fedcba"}}"##.utf8))
        #expect(duplicateRole.tokens?.isEmpty == true)
        #expect(duplicateRole.syntaxPalette == ["keyword": "#fedcba"])
    }

    @Test func originalFlexokiThemesImportWithoutConversion() throws {
        let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        for name in ["flexoki", "flexoki-light"] {
            let data = try Data(contentsOf: root.appendingPathComponent("Fixtures/Themes/\(name).json"))
            let definition = try NativeThemeDefinition.parse(data)
            #expect(definition.tokens?["bgTransparent"] == "transparent")
            #expect(NativeThemeDefinition.hexColor("transparent") == .clear)
            let theme = MicaTheme(dark: name == "flexoki", palette: "custom", customJSON: String(decoding: data, as: UTF8.self))
            #expect(theme.accent == NativeThemeDefinition.hexColor(try #require(definition.tokens?["borderAccent"])))
            #expect(theme.focusBorder == NativeThemeDefinition.hexColor(try #require(definition.tokens?["borderFocused"])))
            #expect(theme.accent != theme.focusBorder)
            let expected: ColorScheme = name == "flexoki" ? .dark : .light
            #expect(definition.preferredColorScheme == expected)
            #expect(NativeThemeDefinition.windowScheme(appearance: "light", palette: "custom", customJSON: String(decoding: data, as: UTF8.self)) == expected)
            let roundTrip = try NativeThemeDefinition.parse(JSONEncoder().encode(definition))
            #expect(roundTrip.tokens == definition.tokens)
            #expect(roundTrip.syntaxPalette == definition.syntaxPalette)
        }
    }

    @Test func systemAppearanceAndTransparentBackgroundRemainAdaptive() throws {
        #expect(NativeThemeDefinition.windowScheme(appearance: "system", palette: "mica", customJSON: "") == nil)
        let transparent = try NativeThemeDefinition.parse(Data(##"{"tokens":{"bg":"transparent"}}"##.utf8))
        #expect(transparent.preferredColorScheme == nil)
        #expect(NativeThemeDefinition.windowScheme(appearance: "dark", palette: "mica", customJSON: "") == .dark)
    }

    @Test func kitThemeHasDistinctLightAndDarkAppearances() {
        let light = MicaTheme(dark: false)
        let dark = MicaTheme(dark: true)
        #expect(light.surface != dark.surface)
        #expect(light.text != dark.text)
    }
}
