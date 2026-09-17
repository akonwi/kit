import AppKit
import Foundation
import SwiftUI

/// Kit's partial theme JSON format. Unsupported roles are retained for future views.
struct NativeThemeDefinition: Codable {
    var tokens: [String: String]?
    var syntaxPalette: [String: String]?

    static func parse(_ data: Data) throws -> NativeThemeDefinition {
        let value = try JSONDecoder().decode(Self.self, from: data)
        guard value.tokens != nil || value.syntaxPalette != nil else {
            throw ThemeError.invalid("Expected a tokens or syntaxPalette object.")
        }
        for (key, color) in Array(value.tokens ?? [:]) + Array(value.syntaxPalette ?? [:]) {
            guard hexColor(color) != nil else {
                throw ThemeError.invalid("\(key): use transparent, #RGB, #RGBA, #RRGGBB, or #RRGGBBAA.")
            }
        }
        return value
    }

    static func hexColor(_ source: String) -> Color? {
        if source == "transparent" { return .clear }
        guard source.first == "#" else { return nil }
        var hex = String(source.dropFirst())
        guard [3, 4, 6, 8].contains(hex.count), hex.allSatisfy({ $0.isHexDigit }) else { return nil }
        if hex.count <= 4 { hex = hex.map { "\($0)\($0)" }.joined() }
        guard let number = UInt64(hex, radix: 16) else { return nil }
        let alpha = hex.count == 8 ? Double(number & 255) / 255 : 1
        let rgb = hex.count == 8 ? number >> 8 : number
        return Color(.sRGB, red: Double((rgb >> 16) & 255) / 255,
                     green: Double((rgb >> 8) & 255) / 255, blue: Double(rgb & 255) / 255, opacity: alpha)
    }

    /// Fixed imported backgrounds determine the contrast of native window chrome.
    var preferredColorScheme: ColorScheme? {
        guard let source = tokens?["bg"], let color = Self.hexColor(source),
              let rgb = NSColor(color).usingColorSpace(.sRGB), rgb.alphaComponent >= 0.99 else { return nil }
        func linear(_ channel: CGFloat) -> Double {
            let value = Double(channel)
            return value <= 0.04045 ? value / 12.92 : pow((value + 0.055) / 1.055, 2.4)
        }
        let luminance = 0.2126 * linear(rgb.redComponent) + 0.7152 * linear(rgb.greenComponent) + 0.0722 * linear(rgb.blueComponent)
        return luminance < 0.179 ? .dark : .light
    }

    static func windowScheme(appearance: String, palette: String, customJSON: String) -> ColorScheme? {
        if palette == "custom", let data = customJSON.data(using: .utf8),
           let theme = try? parse(data), let scheme = theme.preferredColorScheme { return scheme }
        return appearance == "system" ? nil : appearance == "dark" ? .dark : .light
    }

    enum ThemeError: LocalizedError {
        case invalid(String)
        var errorDescription: String? { switch self { case .invalid(let message): return message } }
    }
}

extension MicaTheme {
    init(dark: Bool, palette: String, customJSON: String) {
        self.dark = dark
        if palette == "custom", let data = customJSON.data(using: .utf8),
           let custom = try? NativeThemeDefinition.parse(data) {
            self.dark = custom.preferredColorScheme.map { $0 == .dark } ?? dark
            overrides = custom.tokens ?? [:]
            syntaxOverrides = custom.syntaxPalette ?? [:]
        } else if palette == "slate" {
            overrides = dark
                ? ["bg":"#18212c", "bgSurface":"#202c39", "bgMuted":"#2b3a4b", "textPrimary":"#e2eaf2", "textMuted":"#a6b8ca", "borderDefault":"#35485b", "borderFocused":"#8db8eb"]
                : ["bg":"#f8fafc", "bgSurface":"#edf2f7", "bgMuted":"#e1e9f2", "textPrimary":"#24374a", "textMuted":"#586c81", "borderDefault":"#ced9e5", "borderFocused":"#386ba4"]
        } else if palette == "sand" {
            overrides = dark
                ? ["bg":"#24221e", "bgSurface":"#2e2b25", "bgMuted":"#3b372e", "textPrimary":"#ebe5d8", "textMuted":"#b8ac96", "borderDefault":"#484237", "borderFocused":"#d5a96d"]
                : ["bg":"#faf7f0", "bgSurface":"#f0eade", "bgMuted":"#e6ddcd", "textPrimary":"#383126", "textMuted":"#776950", "borderDefault":"#d9cfbd", "borderFocused":"#986326"]
        }
    }

    func token(_ name: String, fallback: Color) -> Color {
        overrides[name].flatMap(NativeThemeDefinition.hexColor) ?? fallback
    }
    func syntax(_ name: String, fallback: Color) -> Color {
        syntaxOverrides[name].flatMap(NativeThemeDefinition.hexColor) ?? fallback
    }
}
