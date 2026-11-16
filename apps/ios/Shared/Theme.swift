import SwiftUI
import UIKit

/// Kit's palette, from the design tokens of the Kit · iOS Paper file. Each
/// color follows the system appearance.
enum Theme {
    static let surface = dynamic(light: 0xFFFFFF, dark: 0x090909)
    static let raised = dynamic(light: 0xF6F6F6, dark: 0x0F0F0F)
    static let hover = dynamic(light: 0xEFEFEF, dark: 0x161616)
    static let border = dynamic(light: 0xDBDBDB, dark: 0x2E2E2E)
    static let strongBorder = dynamic(light: 0xABABAB, dark: 0x585858)
    static let text = dynamic(light: 0x121212, dark: 0xF2F2F2)
    static let muted = dynamic(light: 0x525252, dark: 0xA1A1A1)
    static let accent = dynamic(light: 0x2965E9, dark: 0x5596FF)
    /// The fill behind the user's messages.
    static let userWash = dynamic(light: 0xE5ECFC, dark: 0x172235)
    /// The tint behind a find match that isn't current.
    static let findMatch = dynamic(light: 0xD4E0FA, dark: 0x1C2E4C)
    static let success = dynamic(light: 0x006915, dark: 0x65DA84)
    static let warning = dynamic(light: 0xB54A00, dark: 0xFF9A59)
    static let danger = dynamic(light: 0x98181F, dark: 0xFF9088)
    static let diffAdded = dynamic(light: 0xE6F0E8, dark: 0x162519)
    static let diffRemoved = dynamic(light: 0xF5E8E9, dark: 0x271C1B)
    /// The fill of a secondary button on glass: the hover grey in light, and
    /// a white wash in dark so the pill still shows against dark glass.
    static let glassButton = Color(uiColor: UIColor { traits in
        traits.userInterfaceStyle == .dark ? UIColor(white: 1, alpha: 0.1) : UIColor(rgb: 0xEFEFEF)
    })

    /// Corner radii: controls and chips, then cards.
    static let controlRadius: CGFloat = 4
    static let cardRadius: CGFloat = 10

    private static func dynamic(light: UInt32, dark: UInt32) -> Color {
        Color(uiColor: UIColor { traits in
            UIColor(rgb: traits.userInterfaceStyle == .dark ? dark : light)
        })
    }
}

private extension UIColor {
    convenience init(rgb: UInt32) {
        self.init(red: CGFloat((rgb >> 16) & 0xFF) / 255, green: CGFloat((rgb >> 8) & 0xFF) / 255,
                  blue: CGFloat(rgb & 0xFF) / 255, alpha: 1)
    }
}
