import AppKit
import CoreText
import SwiftUI

@MainActor @Observable
final class Typography {
    static let shared = Typography()
    var interfaceFamily: String { didSet { defaults.set(interfaceFamily, forKey: "interfaceFont") } }
    var monoFamily: String { didSet { defaults.set(monoFamily, forKey: "monoFont") } }
    var interfaceSize: Double { didSet { defaults.set(interfaceSize, forKey: "interfaceFontSize") } }
    var monoSize: Double { didSet { defaults.set(monoSize, forKey: "monoFontSize") } }
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        interfaceFamily = defaults.string(forKey: "interfaceFont") ?? "System"
        monoFamily = defaults.string(forKey: "monoFont") ?? "JetBrains Mono"
        interfaceSize = defaults.object(forKey: "interfaceFontSize") as? Double ?? 13
        monoSize = defaults.object(forKey: "monoFontSize") as? Double ?? 12
    }

    static func registerFonts() {
        guard let folder = Bundle.main.resourceURL?.appendingPathComponent("Fonts"),
              let urls = try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil) else { return }
        for url in urls where url.pathExtension == "ttf" {
            CTFontManagerRegisterFontsForURL(url as CFURL, .process, nil)
        }
    }

    func font(size: CGFloat, weight: NSFont.Weight = .regular, mono: Bool = false) -> NSFont {
        let family = mono ? monoFamily : interfaceFamily
        let pointSize = max(8, size + (mono ? monoSize - 12 : interfaceSize - 13))
        let fallback = mono ? NSFont.monospacedSystemFont(ofSize: pointSize, weight: weight)
            : NSFont.systemFont(ofSize: pointSize, weight: weight)
        guard family != "System" else { return fallback }
        let managerWeight = weight >= .bold ? 9 : weight >= .medium ? 6 : 5
        return NSFontManager.shared.font(withFamily: family, traits: [], weight: managerWeight, size: pointSize) ?? fallback
    }

    func reset() {
        interfaceFamily = "System"
        monoFamily = "JetBrains Mono"
        interfaceSize = 13
        monoSize = 12
    }
}

extension Font {
    @MainActor static func kit(size: CGFloat, weight: Font.Weight = .regular, design: Font.Design = .default) -> Font {
        let nativeWeight: NSFont.Weight = switch weight {
        case .bold, .heavy, .black: .bold
        case .semibold: .semibold
        case .medium: .medium
        case .light, .thin, .ultraLight: .light
        default: .regular
        }
        return Font(Typography.shared.font(size: size, weight: nativeWeight, mono: design == .monospaced))
    }
}
