import Foundation

// Terminal cell widths, as the reference renderer measures them through
// Cooper and Vaxis: each grapheme cluster's width under Ghostty's rules, where
// wide East Asian characters and emoji take two cells, marks and joiners take
// none, and a tab is eight spaces.

/// One user-perceived character and the cells it occupies.
struct Grapheme: Equatable {
    let text: String
    let width: Int
}

enum TextWidth {
    /// value's graphemes in order, each with its cell width.
    static func graphemes(_ value: String) -> [Grapheme] {
        var result: [Grapheme] = []
        for character in value {
            if character == "\t" {
                result.append(contentsOf: repeatElement(Grapheme(text: " ", width: 1), count: 8))
                continue
            }
            result.append(Grapheme(text: String(character), width: width(of: character)))
        }
        return result
    }

    /// The cells value occupies.
    static func of(_ value: String) -> Int {
        var total = 0
        for character in value {
            total += character == "\t" ? 8 : width(of: character)
        }
        return total
    }

    static func width(of character: Character) -> Int {
        let scalars = Array(character.unicodeScalars)
        guard let first = scalars.first else { return 0 }
        if scalars.count == 1 { return standalone(first) }
        var width = zeroInGrapheme(first) ? 0 : standalone(first)
        var previous = first
        var pictographic = isPictographic(first)
        var regional = isRegional(first)
        var index = 1
        while index < scalars.count {
            let scalar = scalars[index]
            switch scalar.value {
            case 0xFE0F:
                if isEmojiBase(previous) { width = 2 }
            case 0xFE0E:
                if isEmojiBase(previous) { width = 1 }
            case 0x200D where pictographic && index + 1 < scalars.count:
                // The pictograph a joiner adds belongs to the emoji before it.
                index += 1
                previous = scalars[index]
                pictographic = isPictographic(previous)
                index += 1
                continue
            case 0x1F3FB...0x1F3FF:
                width = 2
            default:
                if regional && isRegional(scalar) {
                    width = 2
                } else if !zeroInGrapheme(scalar) {
                    width += standalone(scalar)
                }
            }
            if !isExtend(scalar) {
                pictographic = isPictographic(scalar)
                regional = isRegional(scalar) && !regional
            }
            previous = scalar
            index += 1
        }
        return width
    }

    /// A rune's width on its own.
    static func standalone(_ scalar: Unicode.Scalar) -> Int {
        let value = scalar.value
        if value >= 0x20 && value < 0x7F { return 1 }
        if value < 0x20 || (value >= 0x7F && value < 0xA0) { return 0 }
        if zeroWidth(scalar) { return 0 }
        // A regional indicator only fills two cells as half of a flag.
        if isRegional(scalar) { return 1 }
        if isWide(value) || scalar.properties.isEmojiPresentation { return 2 }
        return 1
    }

    private static func zeroWidth(_ scalar: Unicode.Scalar) -> Bool {
        let value = scalar.value
        switch scalar.properties.generalCategory {
        case .nonspacingMark, .enclosingMark: return true
        case .format: return true
        default: break
        }
        return (0x1160...0x11FF).contains(value)
            || (0xD7B0...0xD7FF).contains(value)
            || (0xFE00...0xFE0F).contains(value)
            || (0xE0100...0xE01EF).contains(value)
            || value == 0x200B
    }

    /// Whether a rune adds no width after the first in a grapheme.
    private static func zeroInGrapheme(_ scalar: Unicode.Scalar) -> Bool {
        zeroWidth(scalar) || scalar.properties.generalCategory == .spacingMark
            || (0xE0000...0xE007F).contains(scalar.value)
    }

    private static func isExtend(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.properties.generalCategory {
        case .nonspacingMark, .enclosingMark, .spacingMark: return true
        default: return (0xFE00...0xFE0F).contains(scalar.value) || (0xE0020...0xE007F).contains(scalar.value)
                || (0x1F3FB...0x1F3FF).contains(scalar.value)
        }
    }

    private static func isPictographic(_ scalar: Unicode.Scalar) -> Bool {
        scalar.value > 0xFF && scalar.properties.isEmoji || scalar.value == 0xA9 || scalar.value == 0xAE
    }

    private static func isEmojiBase(_ scalar: Unicode.Scalar) -> Bool {
        scalar.properties.isEmoji
    }

    private static func isRegional(_ scalar: Unicode.Scalar) -> Bool {
        (0x1F1E6...0x1F1FF).contains(scalar.value)
    }

    /// East Asian Wide and Fullwidth ranges outside emoji presentation.
    private static func isWide(_ value: UInt32) -> Bool {
        for range in wide where range.contains(value) { return true }
        return false
    }

    private static let wide: [ClosedRange<UInt32>] = [
        0x1100...0x115F, 0x231A...0x231B, 0x2329...0x232A, 0x23E9...0x23EC, 0x23F0...0x23F0,
        0x23F3...0x23F3, 0x25FD...0x25FE, 0x2614...0x2615, 0x2630...0x2637, 0x2648...0x2653, 0x267F...0x267F, 0x268A...0x268F,
        0x2693...0x2693, 0x26A1...0x26A1, 0x26AA...0x26AB, 0x26BD...0x26BE, 0x26C4...0x26C5,
        0x26CE...0x26CE, 0x26D4...0x26D4, 0x26EA...0x26EA, 0x26F2...0x26F3, 0x26F5...0x26F5,
        0x26FA...0x26FA, 0x26FD...0x26FD, 0x2705...0x2705, 0x270A...0x270B, 0x2728...0x2728,
        0x274C...0x274C, 0x274E...0x274E, 0x2753...0x2755, 0x2757...0x2757, 0x2795...0x2797,
        0x27B0...0x27B0, 0x27BF...0x27BF, 0x2B1B...0x2B1C, 0x2B50...0x2B50, 0x2B55...0x2B55,
        0x2E80...0x303E, 0x3041...0x3247, 0x3250...0x33FF, 0x3400...0x4DFF, 0x4E00...0x9FFF, 0xA000...0xA4CF,
        0xA960...0xA97F, 0xAC00...0xD7A3, 0xF900...0xFAFF, 0xFE10...0xFE19, 0xFE30...0xFE6F,
        0xFF00...0xFF60, 0xFFE0...0xFFE6, 0x16FE0...0x16FE4, 0x16FF0...0x16FF1, 0x17000...0x18CFF,
        0x18D00...0x18D08, 0x1AFF0...0x1B2FF, 0x1D300...0x1D356, 0x1D360...0x1D376, 0x1F004...0x1F004, 0x1F0CF...0x1F0CF,
        0x1F18E...0x1F18E, 0x1F191...0x1F19A, 0x1F200...0x1F202, 0x1F210...0x1F23B,
        0x1F240...0x1F248, 0x1F250...0x1F251, 0x1F260...0x1F265, 0x1F300...0x1F320,
        0x1F32D...0x1F335, 0x1F337...0x1F37C, 0x1F37E...0x1F393, 0x1F3A0...0x1F3CA,
        0x1F3CF...0x1F3D3, 0x1F3E0...0x1F3F0, 0x1F3F4...0x1F3F4, 0x1F3F8...0x1F43E,
        0x1F440...0x1F440, 0x1F442...0x1F4FC, 0x1F4FF...0x1F53D, 0x1F54B...0x1F54E,
        0x1F550...0x1F567, 0x1F57A...0x1F57A, 0x1F595...0x1F596, 0x1F5A4...0x1F5A4,
        0x1F5FB...0x1F64F, 0x1F680...0x1F6C5, 0x1F6CC...0x1F6CC, 0x1F6D0...0x1F6D2,
        0x1F6D5...0x1F6D7, 0x1F6DC...0x1F6DF, 0x1F6EB...0x1F6EC, 0x1F6F4...0x1F6FC,
        0x1F7E0...0x1F7EB, 0x1F7F0...0x1F7F0, 0x1F90C...0x1F93A, 0x1F93C...0x1F945,
        0x1F947...0x1F9FF, 0x1FA70...0x1FAFF, 0x20000...0x2FFFD, 0x30000...0x3FFFD,
    ]
}
