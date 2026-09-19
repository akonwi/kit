import AppKit
import SwiftUI

/// Observe the inherited app appearance, never a window's explicit theme override.
@MainActor @Observable
final class SystemAppearance {
    static let shared = SystemAppearance(application: .shared)
    private(set) var scheme: ColorScheme
    @ObservationIgnored private var observation: NSKeyValueObservation?

    init(scheme: ColorScheme) { self.scheme = scheme }

    convenience init(application: NSApplication) {
        self.init(scheme: Self.colorScheme(application.effectiveAppearance))
        observation = application.observe(\.effectiveAppearance, options: [.new]) { [weak self] application, _ in
            Task { @MainActor [weak self] in
                self?.update(Self.colorScheme(application.effectiveAppearance))
            }
        }
    }

    func update(_ scheme: ColorScheme) { self.scheme = scheme }

    func resolve(_ preference: String) -> ColorScheme {
        switch preference {
        case "light": .light
        case "dark": .dark
        default: scheme
        }
    }

    static func colorScheme(_ appearance: NSAppearance) -> ColorScheme {
        appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua ? .dark : .light
    }
}
