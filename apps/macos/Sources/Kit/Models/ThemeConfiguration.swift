import Foundation
import SwiftUI

/// App-local light and dark selections. Theme definitions only come from the shared themes directory.
struct ThemeConfiguration: Codable {
    var light = "mica"
    var dark = "mica"

    static func decode(_ json: String) -> Self {
        (try? JSONDecoder().decode(Self.self, from: Data(json.utf8))) ?? Self()
    }
    var json: String { String(decoding: (try? JSONEncoder().encode(self)) ?? Data(), as: UTF8.self) }

    func theme(dark isDark: Bool, installed: [InstalledTheme] = []) -> MicaTheme {
        let id = isDark ? dark : light
        if let item = installed.first(where: { $0.id == id }) {
            return MicaTheme(dark: isDark, palette: "custom", customJSON: item.definition.json)
        }
        return MicaTheme(dark: isDark, palette: id, customJSON: "")
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
        let valid = Set(["mica"] + installed.map(\.id))
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
