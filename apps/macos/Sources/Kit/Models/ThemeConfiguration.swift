import Foundation
import Observation
import SwiftUI

/// App-local light and dark selections. Theme definitions only come from the shared themes directory.
struct ThemeConfiguration: Codable {
    var light = "mica"
    var dark = "mica"

    static func decode(_ json: String) -> Self {
        (try? JSONDecoder().decode(Self.self, from: Data(json.utf8))) ?? Self()
    }
    var json: String { String(decoding: (try? JSONEncoder().encode(self)) ?? Data(), as: UTF8.self) }

    /// Kit's own theme in either variant. "mica" is Kit's theme in the
    /// variant of the mode it's assigned to.
    static let kitDark = "mica-dark", kitLight = "mica-light"

    func theme(dark isDark: Bool, installed: [InstalledTheme] = []) -> MicaTheme {
        let id = isDark ? dark : light
        if id == Self.kitDark { return MicaTheme(dark: true, palette: "mica", customJSON: "") }
        if id == Self.kitLight { return MicaTheme(dark: false, palette: "mica", customJSON: "") }
        if let item = installed.first(where: { $0.id == id }) {
            return MicaTheme(dark: isDark, palette: "custom", customJSON: item.definition.json)
        }
        return MicaTheme(dark: isDark, palette: id, customJSON: "")
    }

    /// The theme for one mode, showing a picker's preview in its place.
    @MainActor func resolved(dark isDark: Bool, installed: [InstalledTheme] = []) -> MicaTheme {
        var shown = self
        if let preview = ThemePreview.shared.choice, preview.dark == isDark {
            if isDark { shown.dark = preview.id } else { shown.light = preview.id }
        }
        return shown.theme(dark: isDark, installed: installed)
    }

    /// The themes offered for either mode: Kit's dark and light, then every
    /// installed theme.
    static func choices(installed: [InstalledTheme]) -> [(id: String, name: String)] {
        [(kitDark, "Kit dark"), (kitLight, "Kit light")] + installed.map { ($0.id, $0.name) }
    }

    /// The choice a mode's saved theme appears as: "mica" is the variant of
    /// that mode.
    func choice(dark isDark: Bool) -> String {
        let id = isDark ? dark : light
        return id == "mica" ? (isDark ? Self.kitDark : Self.kitLight) : id
    }

    mutating func assign(_ item: InstalledTheme, fallbackDark: Bool) {
        let isDark = item.definition.preferredColorScheme.map { $0 == .dark } ?? fallbackDark
        if isDark { dark = item.id } else { light = item.id }
        if isDark && light == item.id { light = "mica" }
        if !isDark && dark == item.id { dark = "mica" }
    }

    /// Drops obsolete preference-backed IDs while retaining shared-file selections
    /// that may be temporarily unavailable or malformed.
    @discardableResult
    mutating func normalize(installed: [InstalledTheme]) -> Bool {
        let valid = Set(["mica", Self.kitDark, Self.kitLight] + installed.map(\.id))
        let previous = self
        if !valid.contains(light), !light.hasPrefix("global:") { light = "mica" }
        if !valid.contains(dark), !dark.hasPrefix("global:") { dark = "mica" }
        return light != previous.light || dark != previous.dark
    }

    /// Removes obsolete embedded theme data while retaining built-in palette choices.
    static func migrate(in defaults: UserDefaults) {
        let value = defaults.string(forKey: "themeConfiguration").map(Self.decode) ?? Self()
        // Re-encoding strips the removed `imported` field from older configurations.
        defaults.set(value.json, forKey: "themeConfiguration")
        defaults.removeObject(forKey: "customThemeJSON")
        defaults.removeObject(forKey: "customThemeName")
        defaults.removeObject(forKey: "themePalette")
    }
}

/// A theme shown in place of one mode's saved theme while the palette's
/// theme picker moves through the list. Nothing is saved until a theme is
/// chosen.
@MainActor @Observable
final class ThemePreview {
    static let shared = ThemePreview()
    struct Choice: Equatable {
        let id: String
        let dark: Bool
    }
    var choice: Choice?
}
