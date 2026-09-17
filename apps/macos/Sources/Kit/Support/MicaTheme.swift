import SwiftUI

/// Mica's default OKLCH roles, resolved into sRGB for the native client.
/// Spacing follows the 4 / 8 / 12 / 16 / 24 / 40 scale; corners soften at interactive surface boundaries.
struct MicaTheme: Equatable {
    var dark: Bool
    var overrides: [String: String] = [:]
    var syntaxOverrides: [String: String] = [:]
    var surface: Color { token("bg", fallback: neutral(1, 0.14)) }
    var raised: Color { token("bgSurface", fallback: neutral(0.973, 0.17)) }
    var hover: Color { token("bgMuted", fallback: neutral(0.952, 0.20)) }
    var border: Color { token("borderDefault", fallback: neutral(0.89, 0.30)) }
    var strongBorder: Color { token("borderDefault", fallback: neutral(0.74, 0.46)) }
    var text: Color { token("textPrimary", fallback: neutral(0.18, 0.96)) }
    var muted: Color { token("textMuted", fallback: neutral(0.44, 0.71)) }
    var accent: Color { token("borderAccent", fallback: token("borderFocused", fallback: Self.oklch(dark ? 0.70 : 0.55, 0.21, 263))) }
    var focusBorder: Color { token("borderFocused", fallback: accent) }
    var success: Color { token("progressNormal", fallback: Self.oklch(dark ? 0.80 : 0.44, 0.162, 150)) }
    var warning: Color { token("warningText", fallback: Self.oklch(dark ? 0.80 : 0.42, 0.162, 48)) }
    var danger: Color { token("errorText", fallback: Self.oklch(dark ? 0.80 : 0.44, 0.162, 25)) }

    private func neutral(_ light: Double, _ dark: Double) -> Color {
        Self.oklch(self.dark ? dark : light, 0, 263)
    }

    static func oklch(_ l: Double, _ c: Double, _ h: Double) -> Color {
        let a = c * cos(h * .pi / 180), b = c * sin(h * .pi / 180)
        let ll = pow(l + 0.3963377774 * a + 0.2158037573 * b, 3)
        let mm = pow(l - 0.1055613458 * a - 0.0638541728 * b, 3)
        let ss = pow(l - 0.0894841775 * a - 1.2914855480 * b, 3)
        func channel(_ v: Double) -> Double {
            min(1, max(0, v <= 0.0031308 ? 12.92 * v : 1.055 * pow(v, 1 / 2.4) - 0.055))
        }
        return Color(.sRGB,
                     red: channel(4.0767416621 * ll - 3.3077115913 * mm + 0.2309699292 * ss),
                     green: channel(-1.2684380046 * ll + 2.6097574011 * mm - 0.3413193965 * ss),
                     blue: channel(-0.0041960863 * ll - 0.7034186147 * mm + 1.7076147010 * ss))
    }
}

private struct MicaThemeKey: EnvironmentKey {
    static let defaultValue = MicaTheme(dark: false)
}

extension EnvironmentValues {
    var mica: MicaTheme {
        get { self[MicaThemeKey.self] }
        set { self[MicaThemeKey.self] = newValue }
    }
}

struct MicaButtonStyle: ButtonStyle {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    var primary = false
    var compact = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.kit(size: 12, weight: .medium))
            .padding(.horizontal, compact ? 10 : 14)
            .frame(height: compact ? 28 : 36)
            .foregroundStyle(primary ? theme.surface : theme.text)
            .background(primary ? theme.text.opacity(configuration.isPressed ? 0.8 : 1) : (configuration.isPressed ? theme.hover : theme.raised))
            .clipShape(RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(primary ? theme.text : theme.border, lineWidth: 1))
            .contentShape(Rectangle())
            .opacity(enabled ? 1 : 0.45)
    }
}

struct Rule: View {
    @Environment(\.mica) private var theme
    var body: some View { Rectangle().fill(theme.border).frame(height: 1) }
}

struct MetaLabel: View {
    @Environment(\.mica) private var theme
    let text: String
    var body: some View {
        Text(text).font(.kit(size: 10, weight: .medium, design: .monospaced))
            .foregroundStyle(theme.muted).tracking(1)
    }
}
