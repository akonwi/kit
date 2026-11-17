import AppKit
import SwiftUI
import Testing
@testable import Kit

/// A sheet drawn in a window at its own size, read by its pixels and
/// clicked with the mouse. Points are from the sheet's top left.
@MainActor private final class RenderedSheet {
    let window: NSWindow
    let host: NSView
    var size: CGSize { host.bounds.size }

    init<Content: View>(_ view: Content) async throws {
        let host = NSHostingView(rootView: view)
        let size = host.fittingSize
        window = NSWindow(testContentRect: NSRect(origin: .zero, size: size))
        host.frame = NSRect(origin: .zero, size: size)
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        self.host = host
        try await settle()
    }

    func settle() async throws {
        for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
    }

    func close() { window.close() }

    private func bitmap() throws -> NSBitmapImageRep {
        host.layoutSubtreeIfNeeded()
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        return bitmap
    }

    /// Whether the pixel at point is color.
    func paints(_ color: Color, at point: CGPoint) throws -> Bool {
        let bitmap = try bitmap()
        return matches(bitmap, color, x: point.x, y: point.y)
    }

    /// The spans down the column at x that are color.
    func runs(of color: Color, x: CGFloat) throws -> [ClosedRange<CGFloat>] {
        let bitmap = try bitmap()
        let scale = CGFloat(bitmap.pixelsWide) / size.width
        var runs: [ClosedRange<CGFloat>] = [], start: Int?
        for row in 0...bitmap.pixelsHigh {
            if row < bitmap.pixelsHigh, matches(bitmap, color, x: x, y: CGFloat(row) / scale) {
                if start == nil { start = row }
            } else if let first = start {
                runs.append(CGFloat(first) / scale...CGFloat(row) / scale)
                start = nil
            }
        }
        return runs
    }

    private func matches(_ bitmap: NSBitmapImageRep, _ color: Color, x: CGFloat, y: CGFloat) -> Bool {
        let scale = CGFloat(bitmap.pixelsWide) / bitmap.size.width
        guard let wanted = swatch(color),
              let pixel = bitmap.colorAt(x: Int(x * scale), y: Int(y * scale))?.usingColorSpace(.sRGB) else { return false }
        return abs(pixel.redComponent - wanted.redComponent) < 0.01 && abs(pixel.greenComponent - wanted.greenComponent) < 0.01
            && abs(pixel.blueComponent - wanted.blueComponent) < 0.01
    }

    private var swatches: [Color: NSColor] = [:]

    /// The color as this window draws it.
    private func swatch(_ color: Color) -> NSColor? {
        if let known = swatches[color] { return known }
        let view = NSHostingView(rootView: color.frame(width: 8, height: 8))
        view.frame = NSRect(x: 0, y: 0, width: 8, height: 8)
        let swatchWindow = NSWindow(testContentRect: view.frame)
        swatchWindow.colorSpace = window.colorSpace
        swatchWindow.contentView = view
        defer { swatchWindow.close() }
        view.layoutSubtreeIfNeeded()
        guard let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds) else { return nil }
        view.cacheDisplay(in: view.bounds, to: bitmap)
        let drawn = bitmap.colorAt(x: bitmap.pixelsWide / 2, y: bitmap.pixelsHigh / 2)?.usingColorSpace(.sRGB)
        swatches[color] = drawn
        return drawn
    }

    func click(at point: CGPoint) async throws {
        let location = NSPoint(x: point.x, y: size.height - point.y)
        for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
            window.sendEvent(try #require(NSEvent.mouseEvent(with: type, location: location, modifierFlags: [], timestamp: 0,
                                                             windowNumber: window.windowNumber, context: nil, eventNumber: 0,
                                                             clickCount: 1, pressure: 1)))
        }
        try await settle()
    }
}

/// The URLs a sheet's links open.
@MainActor private final class OpenedURLs {
    var urls: [URL] = []
    var action: OpenURLAction { OpenURLAction { [self] url in urls.append(url); return .handled } }
}

/// The update sheet and Kit › Release Notes as they appear.
@MainActor struct AppUpdateSheetTests {
    private static func release(_ tag: String, body: String, published: String) -> AppUpdateFixture.Listed {
        .init(tag: tag, body: body, published: published)
    }

    /// An app on 0.17.0 offered 0.19.1, with 0.19.0 and 0.18.0 between.
    private func threeReleasesBehind() async -> AppUpdater {
        let pages = ReleasePages([1: [
            Self.release("macos-v0.19.1", body: ReleaseNotesFixture.v0191, published: "2026-11-16T20:16:08Z"),
            Self.release("macos-v0.19.0", body: ReleaseNotesFixture.v0190, published: "2026-11-15T16:59:29Z"),
            Self.release("macos-v0.18.0", body: ReleaseNotesFixture.v0180, published: "2026-11-13T15:11:48Z"),
            .init(tag: "macos-v0.17.0"),
        ]])
        var effects = AppUpdater.Effects.fixture(list: Data())
        effects.fetchList = { try pages.fetch($0) }
        let updater = AppUpdater(current: ReleaseVersion("0.17.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: effects, defaults: TestDefaults.make())
        await updater.check()
        return updater
    }

    /// The sheet is 720 pt wide. Inside its 24 pt padding, the releases sit
    /// in a 400 pt box: a 160 pt list on the raised fill, a 1 pt rule, then
    /// the notes on the sheet's surface. The newest release is selected,
    /// filled in hover from 6 pt inside the list's edges; choosing another
    /// selects it, and View on GitHub opens the selected release.
    @Test(arguments: [true, false])
    func theSheetListsEveryReleaseBesideTheSelectedOnesNotes(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let opened = OpenedURLs()
        let updater = await threeReleasesBehind()
        let sheet = try await RenderedSheet(AppUpdateSheet(updater: updater, servers: nil) {}
            .environment(\.mica, theme).environment(\.openURL, opened.action))
        defer { sheet.close() }
        #expect(sheet.size.width == 720)
        // The box, by its leading border; down the list inside a row's
        // trailing padding: the raised fill with the selected row's hover
        // fill in it.
        let edge = try #require(try sheet.runs(of: theme.border, x: 24.5).max { $0.upperBound - $0.lowerBound < $1.upperBound - $1.lowerBound })
        func inBox(_ runs: [ClosedRange<CGFloat>]) -> [ClosedRange<CGFloat>] {
            runs.filter { $0.lowerBound >= edge.lowerBound - 6 && $0.upperBound <= edge.upperBound + 6 }
        }
        let raised = inBox(try sheet.runs(of: theme.raised, x: 176))
        let rows = inBox(try sheet.runs(of: theme.hover, x: 176))
        let top = try #require(raised.first).lowerBound
        let bottom = try #require(raised.last).upperBound
        // 400 pt less the border above and below.
        #expect(abs(bottom - top - 398) <= 1, "list \(raised)")
        let first = try #require(rows.first)
        #expect(rows.count == 1)
        // 6 pt inside the box, which starts 1 pt above the fill.
        #expect(abs(first.lowerBound - (top - 1 + 6)) <= 1, "selected \(first), list from \(top)")
        // Across the selected row: raised, hover, raised, the rule, the notes.
        let y = (first.lowerBound + first.upperBound) / 2
        #expect(try sheet.paints(theme.raised, at: CGPoint(x: 27, y: y)))
        #expect(try sheet.paints(theme.hover, at: CGPoint(x: 31, y: y)))
        #expect(try sheet.paints(theme.hover, at: CGPoint(x: 177, y: y)))
        #expect(try sheet.paints(theme.raised, at: CGPoint(x: 181, y: y)))
        #expect(try sheet.paints(theme.border, at: CGPoint(x: 184.5, y: y)))
        #expect(try sheet.paints(theme.surface, at: CGPoint(x: 190, y: y)))

        // The second row, 2 pt below the first.
        let height = first.upperBound - first.lowerBound
        try await sheet.click(at: CGPoint(x: 100, y: first.upperBound + 2 + height / 2))
        let selected = try #require(inBox(try sheet.runs(of: theme.hover, x: 176)).first)
        #expect(abs(selected.lowerBound - (first.upperBound + 2)) <= 1, "selected \(selected) after \(first)")
        #expect(abs(selected.upperBound - selected.lowerBound - height) <= 1)

        // View on GitHub, on the footer's leading edge.
        try await sheet.click(at: CGPoint(x: 50, y: sheet.size.height - 24 - 14))
        #expect(opened.urls == [URL(string: "https://github.com/akonwi/kit/releases/tag/macos-v0.19.0")!])
    }

    /// One release shows its notes across the whole box, without a list.
    @Test func oneReleaseShowsWithoutTheList() async throws {
        let theme = MicaTheme(dark: true)
        let pages = ReleasePages([1: [Self.release("macos-v0.19.1", body: ReleaseNotesFixture.v0191, published: "2026-11-16T20:16:08Z"),
                                      .init(tag: "macos-v0.19.0")]])
        var effects = AppUpdater.Effects.fixture(list: Data())
        effects.fetchList = { try pages.fetch($0) }
        let updater = AppUpdater(current: ReleaseVersion("0.19.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: effects, defaults: TestDefaults.make())
        await updater.check()
        let sheet = try await RenderedSheet(AppUpdateSheet(updater: updater, servers: nil) {}.environment(\.mica, theme))
        defer { sheet.close() }

        #expect(sheet.size.width == 720)
        // Down the box's leading edge, inside its border: the sheet's surface.
        let border = try sheet.runs(of: theme.border, x: 24.5)
        let box = try #require(border.max { $0.upperBound - $0.lowerBound < $1.upperBound - $1.lowerBound })
        let middle = (box.lowerBound + box.upperBound) / 2
        #expect(try sheet.paints(theme.surface, at: CGPoint(x: 27, y: middle)))
        #expect(try sheet.paints(theme.surface, at: CGPoint(x: 176, y: middle)))
    }

    /// Kit › Release Notes: this version's notes, View on GitHub, and Done.
    @Test func releaseNotesShowThisVersionWithDone() async throws {
        let theme = MicaTheme(dark: false)
        let opened = OpenedURLs()
        let page = try KitReleasePage(AppUpdateFixture.list([
            Self.release("macos-v0.19.1", body: ReleaseNotesFixture.v0191, published: "2026-11-16T20:16:08Z"),
        ]))
        let release = try #require(page.releases(.app).first)
        var closed = 0
        let sheet = try await RenderedSheet(ReleaseNotesSheet(release: release) { closed += 1 }
            .environment(\.mica, theme).environment(\.openURL, opened.action))
        defer { sheet.close() }

        #expect(sheet.size.width == 720)
        let border = try sheet.runs(of: theme.border, x: 24.5)
        let box = try #require(border.max { $0.upperBound - $0.lowerBound < $1.upperBound - $1.lowerBound })
        #expect(try sheet.paints(theme.surface, at: CGPoint(x: 27, y: (box.lowerBound + box.upperBound) / 2)))
        try await sheet.click(at: CGPoint(x: 50, y: sheet.size.height - 24 - 14))
        #expect(opened.urls == [URL(string: "https://github.com/akonwi/kit/releases/tag/macos-v0.19.1")!])
        // Done, the primary button on the trailing edge, is filled in text.
        #expect(try sheet.paints(theme.text, at: CGPoint(x: 720 - 24 - 4, y: sheet.size.height - 24 - 14)))
        try await sheet.click(at: CGPoint(x: 720 - 24 - 6, y: sheet.size.height - 24 - 14))
        #expect(closed == 1)
    }
}
