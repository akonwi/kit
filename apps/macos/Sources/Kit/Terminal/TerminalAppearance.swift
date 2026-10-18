import AppKit
import SwiftUI

/// How terminals look: the Kit theme's colors and the mono font from
/// Settings › Typography. It becomes libghostty's whole configuration; the
/// user's own Ghostty configuration is never read.
struct TerminalAppearance: Equatable {
    var background: String
    var foreground: String
    var cursor: String
    var selection: String
    var dark: Bool
    var fontFamily: String
    var fontSize: Double

    init(background: String, foreground: String, cursor: String, selection: String, dark: Bool,
         fontFamily: String, fontSize: Double) {
        self.background = background
        self.foreground = foreground
        self.cursor = cursor
        self.selection = selection
        self.dark = dark
        self.fontFamily = fontFamily
        self.fontSize = fontSize
    }

    @MainActor
    init(theme: MicaTheme, typography: Typography = .shared) {
        self.init(background: Self.hex(theme.surface), foreground: Self.hex(theme.text), cursor: Self.hex(theme.text),
                  selection: Self.hex(theme.surface.mix(with: theme.accent, by: 0.35)), dark: theme.dark,
                  fontFamily: typography.monoFamily, fontSize: typography.monoSize)
    }

    /// libghostty's configuration file. Keys are cleared and only those
    /// listed are bound, so Kit's menus keep their shortcuts and Ghostty's
    /// window, tab, and split bindings never reach a terminal. Programs may
    /// write the clipboard but not read it.
    var configuration: String {
        var lines = [
            "font-family = " + Self.quoted(fontFamily),
            "font-size = " + String(format: "%g", fontSize),
            "window-padding-x = 10",
            "window-padding-y = 6",
            "window-padding-balance = true",
            "window-padding-color = extend",
            "background = " + background,
            "foreground = " + foreground,
            "cursor-color = " + cursor,
            "cursor-text = " + background,
            "selection-background = " + selection,
            "selection-foreground = " + foreground,
            // Ghostty's 16 colors suit dark backgrounds; on light ones its
            // pale colors are darkened until they read.
            "minimum-contrast = " + (dark ? "1" : "3"),
            "clipboard-read = deny",
            "clipboard-write = allow",
            "clipboard-paste-protection = false",
            "confirm-close-surface = true",
            "wait-after-command = false",
            "keybind = clear",
        ]
        lines += Self.bindings.map { "keybind = " + $0 }
        return lines.joined(separator: "\n") + "\n"
    }

    /// Shortcuts a terminal keeps from Ghostty's macOS defaults: scrolling,
    /// prompt jumps, clearing, and line editing.
    static let bindings = [
        "super+k=clear_screen",
        "super+home=scroll_to_top",
        "super+end=scroll_to_bottom",
        "shift+page_up=scroll_page_up",
        "shift+page_down=scroll_page_down",
        "super+page_up=scroll_page_up",
        "super+page_down=scroll_page_down",
        "super+shift+arrow_up=jump_to_prompt:-1",
        "super+shift+arrow_down=jump_to_prompt:1",
        "super+arrow_left=text:\\x01",
        "super+arrow_right=text:\\x05",
        "super+backspace=text:\\x15",
        "alt+arrow_left=esc:b",
        "alt+arrow_right=esc:f",
    ]

    /// A configuration value in double quotes, for names with spaces.
    private static func quoted(_ value: String) -> String {
        "\"" + value.replacingOccurrences(of: "\\", with: "").replacingOccurrences(of: "\"", with: "")
            .replacingOccurrences(of: "\n", with: " ") + "\""
    }

    /// `#RRGGBB` in sRGB, ignoring alpha.
    static func hex(_ color: Color) -> String {
        let resolved = NSColor(color).usingColorSpace(.sRGB) ?? .black
        func channel(_ value: CGFloat) -> Int { Int((min(max(value, 0), 1) * 255).rounded()) }
        return String(format: "#%02X%02X%02X", channel(resolved.redComponent),
                      channel(resolved.greenComponent), channel(resolved.blueComponent))
    }
}
