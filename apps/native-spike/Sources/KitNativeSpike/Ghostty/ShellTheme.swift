import AppKit
import GhosttyKit

/// Terminal presentation owned by the spike. The user's Ghostty configuration
/// is never read; the generated config follows the active Kit theme.
enum ShellTheme {
    static func ghosttyConfig(_ theme: Theme) -> String {
        """
        font-family = JetBrains Mono
        font-size = 12
        window-padding-x = 10
        window-padding-y = 6
        window-padding-balance = true
        window-padding-color = extend
        background = #\(theme.terminal.bg)
        foreground = #\(theme.terminal.fg)
        selection-background = #\(theme.terminal.selection)
        selection-foreground = #\(theme.terminal.fg)
        cursor-style-blink = false
        shell-integration = none
        confirm-close-surface = false
        clipboard-paste-protection = false
        clipboard-read = deny
        clipboard-write = allow
        wait-after-command = false
        macos-option-as-alt = left
        keybind = clear
        """
    }

    /// Writes the generated configuration and returns its path.
    static func writeGhosttyConfig(_ theme: Theme) throws -> String {
        let support = try FileManager.default.url(
            for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            .appendingPathComponent("KitNativeSpike", isDirectory: true)
        try FileManager.default.createDirectory(at: support, withIntermediateDirectories: true)
        let path = support.appendingPathComponent("ghostty-\(theme.id).conf")
        try ghosttyConfig(theme).write(to: path, atomically: true, encoding: .utf8)
        return path.path
    }

    /// Applies a theme to the running app and every live surface.
    static func apply(_ theme: Theme) {
        guard let app = GhosttyRuntime.shared.app, let path = try? writeGhosttyConfig(theme),
              let cfg = ghostty_config_new() else { return }
        ghostty_config_load_file(cfg, path)
        ghostty_config_finalize(cfg)
        ghostty_app_update_config(app, cfg)
        // Keep the config alive like Ghostty's app does; release the old one.
        if let previous = current { ghostty_config_free(previous) }
        current = cfg
    }

    private static var current: ghostty_config_t?
}
