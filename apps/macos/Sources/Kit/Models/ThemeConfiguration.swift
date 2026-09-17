import Foundation
import SwiftUI

struct ThemeConfiguration: Codable {
    struct Imported: Codable, Identifiable {
        var id: String
        var name: String
        var definition: NativeThemeDefinition
        var json: String { String(decoding: (try? JSONEncoder().encode(definition)) ?? Data(), as: UTF8.self) }
    }
    var light = "mica"
    var dark = "mica"
    var imported: [Imported] = []

    static func decode(_ json: String) -> Self {
        (try? JSONDecoder().decode(Self.self, from: Data(json.utf8))) ?? Self()
    }
    var json: String { String(decoding: (try? JSONEncoder().encode(self)) ?? Data(), as: UTF8.self) }

    func theme(dark isDark: Bool) -> MicaTheme {
        let id = isDark ? dark : light
        if let item = imported.first(where: { $0.id == id }) {
            return MicaTheme(dark: isDark, palette: "custom", customJSON: item.json)
        }
        return MicaTheme(dark: isDark, palette: id, customJSON: "")
    }

    mutating func add(name: String, definition: NativeThemeDefinition, fallbackDark: Bool) {
        let id = imported.first(where: { $0.name == name })?.id ?? UUID().uuidString
        imported.removeAll { $0.id == id }
        imported.append(Imported(id: id, name: name, definition: definition))
        let isDark = definition.preferredColorScheme.map { $0 == .dark } ?? fallbackDark
        if isDark { dark = id } else { light = id }
        // Reimporting a changed file must not leave it assigned to the wrong mode.
        if isDark && light == id { light = "mica" }
        if !isDark && dark == id { dark = "mica" }
    }

    static func migrate(in defaults: UserDefaults) {
        guard defaults.string(forKey: "themeConfiguration") == nil else { return }
        var value = Self()
        let oldPalette = defaults.string(forKey: "themePalette") ?? "mica"
        if ["mica", "slate", "sand"].contains(oldPalette) { value.light = oldPalette; value.dark = oldPalette }
        if let oldJSON = defaults.string(forKey: "customThemeJSON"),
           let definition = try? NativeThemeDefinition.parse(Data(oldJSON.utf8)) {
            value.add(name: defaults.string(forKey: "customThemeName") ?? "Imported theme",
                      definition: definition, fallbackDark: defaults.string(forKey: "appearance") == "dark")
            if oldPalette != "custom" { value.light = oldPalette; value.dark = oldPalette }
        }
        defaults.set(value.json, forKey: "themeConfiguration")
    }
}
