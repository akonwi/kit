import AppKit
import CoreText
import SwiftUI

/// Design roles from the Paper "Kit App" file. Each theme maps Kit theme
/// tokens onto these roles (see the "Notes · theme files" board).
struct Theme: Identifiable, Equatable {
    let id: String
    let name: String
    let dark: Bool
    let surface: Color      // tile surface
    let raised: Color       // composer, headers
    let frame: Color        // window chrome
    let frameHover: Color   // selected sidebar row, toggle
    let chip: Color         // code chips
    let border: Color
    let strongBorder: Color // idle tab underline, gutter numbers
    let focusBorder: Color  // focused tile border
    let text: Color
    let muted: Color
    let accent: Color
    let success: Color
    let warning: Color
    let danger: Color
    let userTint: Color
    let addedBg: Color
    let removedBg: Color
    let addedText: Color
    let removedText: Color
    let addedGutter: Color
    let removedGutter: Color
    /// Terminal colours (hex) for the generated Ghostty config.
    let terminal: (bg: String, fg: String, selection: String)

    static func == (a: Theme, b: Theme) -> Bool { a.id == b.id }

    var colorScheme: ColorScheme { dark ? .dark : .light }
}

extension Color {
    init(hex: String) {
        var value: UInt64 = 0
        Scanner(string: hex.trimmingCharacters(in: CharacterSet(charactersIn: "#"))).scanHexInt64(&value)
        self.init(.sRGB,
                  red: Double((value >> 16) & 0xFF) / 255,
                  green: Double((value >> 8) & 0xFF) / 255,
                  blue: Double(value & 0xFF) / 255)
    }
}

/// Builds a Kit-theme-driven palette (Flexoki, Claude, Nord) using the role
/// mapping in the Paper notes. Gaps in the token set are filled the way the
/// boards do: chrome reuses bgSurface, tints mix userBorder over bg.
private func kitTheme(
    id: String, name: String, dark: Bool,
    bg: String, bgSurface: String, bgMuted: String, border: String, focused: String,
    accent: String, text: String, muted: String, tool: String, warning: String, error: String,
    addedBg: String, removedBg: String, addedGutter: String, removedGutter: String, userTint: String
) -> Theme {
    Theme(
        id: id, name: name, dark: dark,
        surface: Color(hex: bg), raised: Color(hex: bgSurface), frame: Color(hex: bgSurface),
        frameHover: Color(hex: bgMuted), chip: Color(hex: bgMuted),
        border: Color(hex: border), strongBorder: Color(hex: focused), focusBorder: Color(hex: focused),
        text: Color(hex: text), muted: Color(hex: muted), accent: Color(hex: accent),
        success: Color(hex: tool), warning: Color(hex: warning), danger: Color(hex: error),
        userTint: Color(hex: userTint),
        addedBg: Color(hex: addedBg), removedBg: Color(hex: removedBg),
        addedText: Color(hex: text), removedText: Color(hex: text),
        addedGutter: Color(hex: addedGutter), removedGutter: Color(hex: removedGutter),
        terminal: (bg, text, bgMuted))
}

extension Theme {
    static let micaDark = Theme(
        id: "mica-dark", name: "Mica Dark", dark: true,
        surface: Color(hex: "090909"), raised: Color(hex: "0F0F0F"), frame: Color(hex: "141414"),
        frameHover: Color(hex: "1C1C1C"), chip: Color(hex: "1A1A1A"),
        border: Color(hex: "2E2E2E"), strongBorder: Color(hex: "585858"), focusBorder: Color(hex: "3A5B91"),
        text: Color(hex: "F2F2F2"), muted: Color(hex: "A1A1A1"), accent: Color(hex: "5596FF"),
        success: Color(hex: "65DA84"), warning: Color(hex: "FF9A59"), danger: Color(hex: "FF9088"),
        userTint: Color(hex: "16233A"),
        addedBg: Color(hex: "0F2416"), removedBg: Color(hex: "2A1414"),
        addedText: Color(hex: "BDEBC8"), removedText: Color(hex: "F3C4BF"),
        addedGutter: Color(hex: "5FA571"), removedGutter: Color(hex: "B8706A"),
        terminal: ("090909", "F2F2F2", "2E2E2E"))

    static let micaLight = Theme(
        id: "mica-light", name: "Mica Light", dark: false,
        surface: Color(hex: "FFFFFF"), raised: Color(hex: "F6F6F6"), frame: Color(hex: "EBEBEB"),
        frameHover: Color(hex: "E0E0E0"), chip: Color(hex: "EFEFEF"),
        border: Color(hex: "DBDBDB"), strongBorder: Color(hex: "ABABAB"), focusBorder: Color(hex: "8EA9E8"),
        text: Color(hex: "121212"), muted: Color(hex: "525252"), accent: Color(hex: "2965E9"),
        success: Color(hex: "006915"), warning: Color(hex: "B54A00"), danger: Color(hex: "98181F"),
        userTint: Color(hex: "E4ECFC"),
        addedBg: Color(hex: "E3F5E5"), removedBg: Color(hex: "FBE6E6"),
        addedText: Color(hex: "121212"), removedText: Color(hex: "121212"),
        addedGutter: Color(hex: "3C8A4E"), removedGutter: Color(hex: "B0484A"),
        terminal: ("FFFFFF", "121212", "DBDBDB"))

    static let flexoki = kitTheme(
        id: "flexoki", name: "Flexoki", dark: true,
        bg: "100F0F", bgSurface: "1C1B1A", bgMuted: "282726", border: "343331", focused: "575653",
        accent: "4385BE", text: "CECDC3", muted: "878580", tool: "879A39", warning: "DA702C", error: "D14D41",
        addedBg: "252D09", removedBg: "3E1715", addedGutter: "879A39", removedGutter: "D14D41", userTint: "19242E")

    static let flexokiLight = kitTheme(
        id: "flexoki-light", name: "Flexoki Light", dark: false,
        bg: "FFFCF0", bgSurface: "F2F0E5", bgMuted: "E6E4D9", border: "DAD8CE", focused: "9F9D96",
        accent: "205EA6", text: "100F0F", muted: "878580", tool: "66800B", warning: "BC5215", error: "AF3029",
        addedBg: "F2F0E5", removedBg: "F2F0E5", addedGutter: "66800B", removedGutter: "AF3029", userTint: "E4E9E7")

    static let claude = kitTheme(
        id: "claude", name: "Claude", dark: false,
        bg: "FAF9F5", bgSurface: "F2F1ED", bgMuted: "EAE9E5", border: "DEDEDA", focused: "B5B4B1",
        accent: "D07254", text: "141413", muted: "71706E", tool: "788C5D", warning: "B16803", error: "C15F3C",
        addedBg: "E8EAE0", removedBg: "F3E7DF", addedGutter: "788C5D", removedGutter: "C15F3C", userTint: "F5E9E2")

    static let claudeDark = kitTheme(
        id: "claude-dark", name: "Claude Dark", dark: true,
        bg: "262624", bgSurface: "2F2F2D", bgMuted: "373735", border: "434240", focused: "636360",
        accent: "D97757", text: "E5E4E1", muted: "959492", tool: "9ACA86", warning: "E8C96B", error: "D47563",
        addedBg: "394034", removedBg: "42332E", addedGutter: "9ACA86", removedGutter: "D47563", userTint: "46352D")

    static let nord = kitTheme(
        id: "nord", name: "Nord", dark: true,
        bg: "2E3440", bgSurface: "363C48", bgMuted: "3D434F", border: "484E59", focused: "646A76",
        accent: "88C0D0", text: "D8DEE9", muted: "979DA8", tool: "A3BE8C", warning: "EBCB8B", error: "BF616A",
        addedBg: "414A4C", removedBg: "453B47", addedGutter: "A3BE8C", removedGutter: "BF616A", userTint: "3E4D5A")

    static let nordLight = kitTheme(
        id: "nord-light", name: "Nord Light", dark: false,
        bg: "E5E9F0", bgSurface: "DFE3EB", bgMuted: "DADEE5", border: "D1D6DE", focused: "B4B9C2",
        accent: "6C87A2", text: "414858", muted: "626977", tool: "758965", warning: "968259", error: "BF616A",
        addedBg: "DCE3E2", removedBg: "E0D9E0", addedGutter: "758965", removedGutter: "BF616A", userTint: "D6DDE7")

    static let all: [Theme] = [.micaDark, .micaLight, .flexoki, .flexokiLight, .claude, .claudeDark, .nord, .nordLight]
}

// MARK: Typography

enum Fonts {
    /// Registers the bundled JetBrains Mono and Phosphor fonts for this process.
    static func register() {
        let candidates = [
            Bundle.main.resourceURL?.appendingPathComponent("Fonts"),
            URL(fileURLWithPath: CommandLine.arguments[0]).deletingLastPathComponent()
                .appendingPathComponent("../../Resources/Fonts").standardized,
        ].compactMap { $0 }
        guard let dir = candidates.first(where: { FileManager.default.fileExists(atPath: $0.path) }),
              let files = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil)
        else {
            NSLog("spike: fonts not found")
            return
        }
        for file in files where file.pathExtension == "ttf" {
            CTFontManagerRegisterFontsForURL(file as CFURL, .process, nil)
        }
    }

    static func mono(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        let name = switch weight {
        case .bold, .semibold: "JetBrainsMono-Bold"
        case .medium: "JetBrainsMono-Medium"
        default: "JetBrainsMono-Regular"
        }
        return .custom(name, fixedSize: size)
    }

    static func ui(_ size: CGFloat = 13, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight)
    }
}

// MARK: Icons

/// Phosphor Regular glyphs drawn from the bundled icon font.
enum Ph: UInt32 {
    case arrowUp = 0xe08e, arrowsIn = 0xe09a, arrowsOut = 0xe0a2, browser = 0xe0f4
    case caretDown = 0xe136, caretRight = 0xe13a, caretUp = 0xe13c, caretLeft = 0xe138
    case chatCircle = 0xe168, chatCenteredText = 0xe166, check = 0xe182, columns = 0xe546
    case file = 0xe230, folder = 0xe24a, gitBranch = 0xe278, gitDiff = 0xe27c, gitPullRequest = 0xe282
    case globe = 0xe288, info = 0xe2ce, magnifyingGlass = 0xe30c, note = 0xe348, pencilSimple = 0xe3b4
    case plus = 0xe3d4, question = 0xe3e8, questionMark = 0xe3e9, robot = 0xe762, rows = 0xe5a2, sidebar = 0xeab6
    case squareSplitHorizontal = 0xe870, squareSplitVertical = 0xe874, stop = 0xe46c
    case terminalWindow = 0xeae8, warning = 0xe4e0, warningCircle = 0xe4e2, x = 0xe4f6
    case layout = 0xe6d6, prohibit = 0xe3de, stack = 0xe466, arrowClockwise = 0xe036, notePencil = 0xe34c
}

struct Icon: View {
    let glyph: Ph
    var size: CGFloat = 14
    var color: Color? = nil
    var filled = false

    var body: some View {
        Text(String(UnicodeScalar(glyph.rawValue)!))
            .font(.custom(filled ? "Phosphor-Fill" : "Phosphor", fixedSize: size))
            .foregroundStyle(color.map(AnyShapeStyle.init) ?? AnyShapeStyle(.foreground))
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

/// Theme environment so every view reads the active palette.
private struct ThemeKey: EnvironmentKey { static let defaultValue = Theme.micaDark }
extension EnvironmentValues {
    var theme: Theme {
        get { self[ThemeKey.self] }
        set { self[ThemeKey.self] = newValue }
    }
}
