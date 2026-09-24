import AppKit
import Foundation
import SwiftUI

/// Kit's partial theme JSON format. Unsupported roles are retained for future views.
struct NativeThemeDefinition: Codable, Equatable, Sendable {
    var tokens: [String: String]?
    var syntaxPalette: [String: String]?
    var json: String { String(decoding: (try? JSONEncoder().encode(self)) ?? Data(), as: UTF8.self) }

    static func parse(_ data: Data) throws -> NativeThemeDefinition {
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw ThemeError.invalid("Expected a JSON object.")
        }
        var scanner = JSONDuplicateScanner(data: data)
        let duplicates = try scanner.scan()
        guard !duplicates.root else { throw ThemeError.invalid("Duplicate top-level field.") }
        return NativeThemeDefinition(tokens: colors(in: object["tokens"], section: "tokens",
                                                     duplicateKeys: duplicates.sections.contains("tokens")),
                                     syntaxPalette: colors(in: object["syntaxPalette"], section: "syntaxPalette",
                                                           duplicateKeys: duplicates.sections.contains("syntaxPalette")))
    }

    /// Invalid individual overrides are omitted, matching the renderer-neutral Go parser.
    private static func colors(in raw: Any?, section: String, duplicateKeys: Bool) -> [String: String]? {
        guard let raw else { return nil }
        if raw is NSNull { return [:] }
        guard !duplicateKeys, let values = raw as? [String: Any] else { return [:] }
        var result: [String: String] = [:]
        for (name, value) in values {
            guard let spelling = value as? String, hexColor(spelling) != nil else { continue }
            let knownOpaque = section == "tokens"
                ? knownTokens.contains(name) && name != "bgTransparent"
                : knownSyntaxRoles.contains(name)
            if knownOpaque && isFullyTransparent(spelling) { continue }
            result[name] = spelling
        }
        return result
    }

    private static func isFullyTransparent(_ spelling: String) -> Bool {
        if spelling == "transparent" { return true }
        let hex = spelling.dropFirst()
        return (hex.count == 4 && hex.last == "0") || (hex.count == 8 && hex.suffix(2) == "00")
    }

    private static let knownTokens: Set<String> = [
        "bg", "bgSurface", "bgMuted", "bgAccent", "bgTransparent", "borderDefault", "borderFocused",
        "borderAccent", "borderDebug", "borderStatus", "composerBashBorder", "composerBashExcludedBorder",
        "textPrimary", "textSecondary", "textMuted", "textPlaceholder", "textDebug", "userText",
        "userTextFocused", "userBorder", "assistantText", "toolText", "reviewText", "errorText",
        "warningText", "subagentText", "debugLabel", "metaText", "attachmentText", "cursor", "pickerBg",
        "pickerBorder", "pickerFocusedBg", "pickerFocusedText", "pickerItemText", "pickerScrollThumb",
        "pickerScrollTrack", "scrollbarFg", "scrollbarBg", "panelText", "progressNormal", "progressWarning",
        "progressCritical", "toggleOn", "diffAddedBg", "diffRemovedBg", "diffAddedContentBg",
        "diffRemovedContentBg", "diffAddedLineNumberBg", "diffRemovedLineNumberBg", "diffCursorBg",
        "diffCursorGutterBg", "diffCursorAddedBg", "diffCursorRemovedBg"
    ]
    private static let knownSyntaxRoles: Set<String> = [
        "text", "heading", "bold", "italic", "link", "list", "quote", "codeInline", "codeBlock",
        "strikethrough", "conceal", "comment", "string", "escape", "number", "keyword", "keywordType",
        "function", "operator", "variable", "member", "builtin", "type", "punctuation", "tag",
        "tagAttribute", "tagDelimiter", "attribute", "label"
    ]

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

/// Detects duplicate object keys that Foundation's JSON decoders otherwise collapse.
private struct JSONDuplicateScanner {
    struct Result {
        var root = false
        var sections: Set<String> = []
    }

    private let bytes: [UInt8]
    private var index = 0
    private var result = Result()

    init(data: Data) { bytes = Array(data) }

    mutating func scan() throws -> Result {
        skipWhitespace()
        guard current == UInt8(ascii: "{") else { throw NativeThemeDefinition.ThemeError.invalid("Expected a JSON object.") }
        try scanObject(depth: 0, section: nil)
        return result
    }

    private mutating func scanValue(depth: Int, section: String?) throws {
        skipWhitespace()
        switch current {
        case UInt8(ascii: "{"): try scanObject(depth: depth, section: section)
        case UInt8(ascii: "["): try scanArray(depth: depth)
        case UInt8(ascii: "\""): _ = try scanString()
        default:
            while let byte = current, ![UInt8(ascii: ","), UInt8(ascii: "]"), UInt8(ascii: "}")].contains(byte),
                  !Self.whitespace.contains(byte) { index += 1 }
        }
    }

    private mutating func scanObject(depth: Int, section: String?) throws {
        index += 1
        var keys: Set<String> = []
        var duplicate = false
        skipWhitespace()
        while current != UInt8(ascii: "}") {
            let key = try scanString()
            if !keys.insert(key).inserted { duplicate = true }
            skipWhitespace(); index += 1 // colon; JSONSerialization already validated the document.
            try scanValue(depth: depth + 1, section: depth == 0 ? key : nil)
            skipWhitespace()
            if current == UInt8(ascii: ",") { index += 1; skipWhitespace() } else { break }
        }
        index += 1
        if depth == 0 && duplicate { result.root = true }
        if depth == 1, duplicate, let section, section == "tokens" || section == "syntaxPalette" {
            result.sections.insert(section)
        }
    }

    private mutating func scanArray(depth: Int) throws {
        index += 1
        skipWhitespace()
        while current != UInt8(ascii: "]") {
            try scanValue(depth: depth + 1, section: nil)
            skipWhitespace()
            if current == UInt8(ascii: ",") { index += 1; skipWhitespace() } else { break }
        }
        index += 1
    }

    private mutating func scanString() throws -> String {
        let start = index
        index += 1
        while let byte = current {
            index += 1
            if byte == UInt8(ascii: "\\") { index += 1; continue }
            if byte == UInt8(ascii: "\"") { break }
        }
        return try JSONDecoder().decode(String.self, from: Data(bytes[start..<index]))
    }

    private mutating func skipWhitespace() {
        while let byte = current, Self.whitespace.contains(byte) { index += 1 }
    }

    private var current: UInt8? { index < bytes.count ? bytes[index] : nil }
    private static let whitespace: Set<UInt8> = [0x20, 0x09, 0x0a, 0x0d]
}

extension MicaTheme {
    init(dark: Bool, palette: String, customJSON: String) {
        self.dark = dark
        if palette == "custom", let data = customJSON.data(using: .utf8),
           let custom = try? NativeThemeDefinition.parse(data) {
            self.dark = custom.preferredColorScheme.map { $0 == .dark } ?? dark
            overrides = custom.tokens ?? [:]
            syntaxOverrides = custom.syntaxPalette ?? [:]
        }
    }

    func token(_ name: String, fallback: Color) -> Color {
        overrides[name].flatMap(NativeThemeDefinition.hexColor) ?? fallback
    }
    func syntax(_ name: String, fallback: Color) -> Color {
        syntaxOverrides[name].flatMap(NativeThemeDefinition.hexColor) ?? fallback
    }
}
