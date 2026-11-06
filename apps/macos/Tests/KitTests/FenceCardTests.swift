import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The copy button in a fenced block's header takes a click anywhere on its
/// icon, its title, the gap between them, and a margin around them that
/// fills the header's height.
@MainActor struct FenceCardTests {
    static let width: CGFloat = 400
    static let source = "let answer = 42\n"
    /// The header is 29 points tall: its 13-point row with 8 points above
    /// and below.
    static let headerHeight: CGFloat = 29

    /// A card in a window of its own, copying to a private pasteboard.
    @MainActor struct Card {
        let window: NSWindow
        let host: NSView
        let pasteboard: NSPasteboard
        let theme: MicaTheme

        init(theme: MicaTheme = MicaTheme(dark: false)) {
            let pasteboard = NSPasteboard(name: .init("kit-fence-card-\(UUID().uuidString)"))
            let host = NSHostingView(rootView: FenceCard(label: "SWIFT", copy: FenceCardTests.source, pasteboard: pasteboard) {
                Text(FenceCardTests.source).padding(14)
            }
            .frame(width: FenceCardTests.width).environment(\.mica, theme).background(theme.surface))
            let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: FenceCardTests.width, height: 100))
            window.contentView = host
            window.makeKeyAndOrderFront(nil)
            self.window = window
            self.host = host
            self.pasteboard = pasteboard
            self.theme = theme
        }

        /// Clicks `point`, in points from the card's top-leading corner, and
        /// reports whether it copied.
        func click(_ point: CGPoint) throws -> Bool {
            let before = pasteboard.changeCount
            let location = host.convert(NSPoint(x: point.x, y: point.y), to: nil)
            for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
                window.sendEvent(try #require(NSEvent.mouseEvent(with: type, location: location, modifierFlags: [],
                    timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber,
                    context: nil, eventNumber: 0, clickCount: 1, pressure: type == .leftMouseDown ? 1 : 0)))
            }
            return pasteboard.changeCount != before
        }

        /// The painted runs in the header, right of center, in points from the
        /// leading edge: the button's icon, then its title. A column is
        /// painted when any pixel of the header's row of content is.
        func buttonInk() throws -> [ClosedRange<CGFloat>] {
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            let scale = CGFloat(bitmap.pixelsWide) / host.bounds.width
            let fill = try #require(NSColor(theme.raised).usingColorSpace(.sRGB))
            // Inside the card's top border and above its rule.
            let rows = Int(2 * scale)..<Int((FenceCardTests.headerHeight - 2) * scale)
            // Stop short of the card's border on the trailing edge.
            let columns = Int(FenceCardTests.width / 2 * scale)..<Int((FenceCardTests.width - 4) * scale)
            func painted(_ x: Int) -> Bool {
                rows.contains { y in
                    guard let color = bitmap.colorAt(x: x, y: y)?.usingColorSpace(.sRGB) else { return false }
                    return abs(color.redComponent - fill.redComponent) + abs(color.greenComponent - fill.greenComponent)
                        + abs(color.blueComponent - fill.blueComponent) > 0.06
                }
            }
            var runs: [ClosedRange<Int>] = []
            for x in columns where painted(x) {
                // Glyphs a few pixels apart belong to one run.
                if let last = runs.last, x - last.upperBound <= Int(3 * scale) {
                    runs[runs.count - 1] = last.lowerBound...x
                } else {
                    runs.append(x...x)
                }
            }
            return runs.map { CGFloat($0.lowerBound) / scale...CGFloat($0.upperBound + 1) / scale }
        }
    }

    /// Each point is clicked on a fresh card, so every click meets the
    /// "Copy" button as it is first drawn.
    @Test(arguments: ["icon", "gap", "title"]) func aClickOnAnyPartOfTheButtonCopies(_ part: String) async throws {
        let card = Card()
        defer { card.window.close() }
        try await eventually { (try? card.buttonInk().count) == 2 }
        let ink = try card.buttonInk()
        try #require(ink.count == 2, "icon and title \(ink)")
        let (icon, title) = (ink[0], ink[1])
        let x = switch part {
        case "icon": (icon.lowerBound + icon.upperBound) / 2
        case "gap": (icon.upperBound + title.lowerBound) / 2
        default: (title.lowerBound + title.upperBound) / 2
        }
        #expect(try card.click(CGPoint(x: x, y: Self.headerHeight / 2)), "\(part) at \(x), ink \(ink)")
        #expect(card.pasteboard.string(forType: .string) == Self.source)
        // The button acknowledges the copy: its title widens to Copied.
        func titleWidth() -> CGFloat? {
            guard let runs = try? card.buttonInk(), runs.count == 2 else { return nil }
            return runs[1].upperBound - runs[1].lowerBound
        }
        try await eventually { (titleWidth() ?? 0) > title.upperBound - title.lowerBound + 4 }
    }

    /// Clicks across the header's middle row and down through the title
    /// copy from 8 points before the icon to 8 points past the title, and
    /// from the header's top to its rule.
    @Test func theButtonsHitAreaIsItsIconAndTitleWithAMarginFillingTheHeader() async throws {
        let card = Card()
        defer { card.window.close() }
        try await eventually { (try? card.buttonInk().count) == 2 }
        // Copy once first: the title then reads Copied and keeps its width.
        let first = try card.buttonInk()
        _ = try card.click(CGPoint(x: (first[1].lowerBound + first[1].upperBound) / 2, y: Self.headerHeight / 2))
        try await eventually { (try? card.buttonInk()) != first }
        let ink = try card.buttonInk()
        try #require(ink.count == 2, "icon and title \(ink)")
        let drawn = ink[0].lowerBound...ink[1].upperBound

        var across: [CGFloat] = []
        for x in stride(from: Self.width / 2, to: Self.width, by: 0.5) where try card.click(CGPoint(x: x, y: Self.headerHeight / 2)) {
            across.append(x)
        }
        let row = try #require(across.first)...(try #require(across.last))
        // Every point in the run copies: the gap included.
        #expect(across.count == Int(((row.upperBound - row.lowerBound) / 0.5).rounded()) + 1, "gaps in \(across)")
        // The title's glyphs sit inside their frame by up to a point.
        #expect(abs(row.lowerBound - (drawn.lowerBound - FenceCard<EmptyView>.copyMargin)) <= 1, "row \(row), drawn \(drawn)")
        #expect(abs(row.upperBound - (drawn.upperBound + FenceCard<EmptyView>.copyMargin)) <= 1.5, "row \(row), drawn \(drawn)")

        let titleX = (ink[1].lowerBound + ink[1].upperBound) / 2
        var down: [CGFloat] = []
        for y in stride(from: 0.5, to: Self.headerHeight + 6, by: 0.5) where try card.click(CGPoint(x: titleX, y: y)) {
            down.append(y)
        }
        let column = try #require(down.first)...(try #require(down.last))
        #expect(down.count == Int(((column.upperBound - column.lowerBound) / 0.5).rounded()) + 1, "gaps in \(down)")
        #expect(column.lowerBound <= 1 && abs(column.upperBound - Self.headerHeight) <= 1, "column \(column)")
    }
}
