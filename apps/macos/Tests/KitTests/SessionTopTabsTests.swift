import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The top strip's order: tabs are dragged one at a time, and the sidebar's
/// tree follows the order they leave.
@MainActor struct SessionTopTabOrderTests {
    private func id(_ session: String, server: String = SessionWindowModel.defaultServer) -> SessionIdentity {
        SessionIdentity(server: server, session: session)
    }

    /// A window whose catalog knows each session's parent.
    private func model(_ sessions: [String], parents: [String: String] = [:], selected: String) -> SessionWindowModel {
        let model = SessionWindowModel(id: UUID(), sessions: sessions.map { id($0) }, selected: id(selected))
        for session in sessions.reversed() {
            model.servers.local.catalog.recordCreated(SessionExcerpt(id: session, title: session.uppercased(), parentSessionID: parents[session],
                                                                     sourceTitle: "", model: "m", thinking: "high", workspace: "",
                                                                     date: "", messages: []))
        }
        return model
    }

    private func titles(_ tabs: [SessionTab]) -> [String] { tabs.map(\.title) }

    @Test func draggingATopTabMovesItAloneAndTheSidebarRegroups() throws {
        let model = model(["a", "a1", "b"], parents: ["a1": "a"], selected: "b")
        var changes = 0
        model.onChange = { _ in changes += 1 }
        let a = try #require(model.tab(for: id("a")))
        model.move(a, toGap: 3)
        #expect(titles(model.navigableTabs(.top)) == ["A1", "B", "A"])
        #expect(model.sessions == [id("a1"), id("b"), id("a")])
        // A family sits where its first tab is, so A's family still leads.
        #expect(model.rows.map(\.id) == ["a", "a1", "b"])
        #expect(changes == 1)
        let b = try #require(model.tab(for: id("b")))
        model.move(b, toGap: 0)
        #expect(titles(model.navigableTabs(.top)) == ["B", "A1", "A"])
        #expect(model.rows.map(\.id) == ["b", "a", "a1"])
        #expect(changes == 2)
    }

    @Test func aTabMovesOnePlaceAtATimeAndStopsAtTheEnds() throws {
        let model = model(["a", "b", "c"], selected: "a")
        let b = try #require(model.tab(for: id("b")))
        model.move(b, by: 1)
        #expect(titles(model.tabs) == ["A", "C", "B"])
        model.move(b, by: 1)
        #expect(titles(model.tabs) == ["A", "C", "B"])
        model.move(b, by: -1)
        model.move(b, by: -1)
        model.move(b, by: -1)
        #expect(titles(model.tabs) == ["B", "A", "C"])
    }

    @Test func droppingATabBesideItselfLeavesTheOrder() throws {
        let model = model(["a", "b", "c"], selected: "a")
        var changes = 0
        model.onChange = { _ in changes += 1 }
        let b = try #require(model.tab(for: id("b")))
        model.move(b, toGap: 1)
        model.move(b, toGap: 2)
        #expect(titles(model.tabs) == ["A", "B", "C"])
        #expect(changes == 0)
    }

    @Test func closingFromTopKeepsTheOrderAndSelectsTheNeighbor() throws {
        let model = model(["a", "b", "a1", "c"], parents: ["a1": "a"], selected: "a")
        model.close(model.selectedTab, from: .top)
        #expect(titles(model.tabs) == ["B", "A1", "C"])
        #expect(model.selectedSession == id("b"))
        let c = try #require(model.tab(for: id("c")))
        model.select(c)
        model.close(c, from: .top)
        #expect(titles(model.tabs) == ["B", "A1"])
        #expect(model.selectedSession == id("a1"))
    }

    @Test func aCloseTheWindowMakesItselfKeepsTheOrder() throws {
        let model = model(["a", "b", "a1"], parents: ["a1": "a"], selected: "b")
        let a = try #require(model.tab(for: id("a")))
        model.close(a, from: nil)
        #expect(titles(model.tabs) == ["B", "A1"])
        #expect(model.selectedSession == id("b"))
    }

    @Test func aTopTabNamesItsFolderWhenItDiffersFromTheSelectedTabsOrIsOnAnotherServer() throws {
        func excerpt(_ id: String, cwd: String) -> SessionExcerpt {
            SessionExcerpt(id: id, title: id, sourceTitle: "", model: "m", thinking: "high", workspace: cwd, cwd: cwd,
                           date: "", messages: [])
        }
        let servers = ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"),
                                      saved: { [SavedServer(name: "vps", kind: .home, address: "/tmp/vps")] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector)) { throw ClientError.noDaemon }
        }
        let vps = try #require(servers.connection(named: "vps"))
        let model = SessionWindowModel(id: UUID(), servers: servers,
                                       sessions: [id("a"), id("b"), id("c"), id("r", server: "vps")], selected: id("a"))
        for (session, cwd) in [("a", "/src/kit"), ("b", "/src/kit"), ("c", "/src/ard")] {
            servers.local.catalog.recordCreated(excerpt(session, cwd: cwd))
        }
        vps.catalog.recordCreated(excerpt("r", cwd: "/src/kit"))
        let selected = model.selectedTab
        #expect(model.tabs.map { SessionTabStrip.project(of: $0, selected: selected) } == [nil, nil, "ard", "kit"])
    }

    /// Five tabs at the strip's pitch: 220 pt with 9 pt between.
    private let spans: [ClosedRange<CGFloat>] = (0..<5).map { index in
        let minX = CGFloat(index) * 229
        return minX...(minX + 220)
    }

    @Test func dividersHideBesideTheSelectedAndHoveredTabsAndTheHeldTabsSlot() {
        let ids = (0..<5).map { _ in UUID() }
        func dividers(selected: Int, hovered: Int? = nil, held: Int? = nil) -> [Bool] {
            (0..<4).map { SessionTabStrip.showsDivider(after: $0, of: ids, selected: ids[selected],
                                                       hovered: hovered.map { ids[$0] }, held: held.map { ids[$0] }) }
        }
        #expect(dividers(selected: 1) == [false, false, true, true])
        #expect(dividers(selected: 1, hovered: 3) == [false, false, false, false])
        #expect(dividers(selected: 0, held: 3) == [false, true, false, false])
        #expect(dividers(selected: 4) == [true, true, true, false])
    }

    @Test func theTabsAHeldTabPassesMakeWayForIt() {
        let tabs = ["a", "b", "c", "d", "e"]
        #expect(SessionTabStrip.arranged(tabs, moving: 1, to: 3) == ["a", "c", "d", "b", "e"])
        #expect(SessionTabStrip.arranged(tabs, moving: 3, to: 0) == ["d", "a", "b", "c", "e"])
        #expect(SessionTabStrip.arranged(tabs, moving: 2, to: 2) == tabs)
        // Dropping there moves the tab into the gap that gives the same order.
        #expect(SessionTabStrip.gap(moving: 1, to: 3) == 4)
        #expect(SessionTabStrip.gap(moving: 3, to: 0) == 0)
        #expect(SessionTabStrip.gap(moving: 2, to: 2) == nil)
    }

    @Test func aNeighborMakesWayOnceTheLiftedTabIsNearerItsPlace() {
        // Slots start every 229 pt; tab 1 starts at 229.
        #expect(SessionTabStrip.target(of: 229, among: spans) == 1)
        // 114 pt right, it is nearer its own slot; 115 pt right, the next one's.
        #expect(SessionTabStrip.target(of: 343, among: spans) == 1)
        #expect(SessionTabStrip.target(of: 344, among: spans) == 2)
        #expect(SessionTabStrip.target(of: 115, among: spans) == 1)
        #expect(SessionTabStrip.target(of: 114, among: spans) == 0)
        #expect(SessionTabStrip.target(of: 2000, among: spans) == 4)
    }

    @Test func theLiftedCopyFollowsThePointerWithinTheTabsAndClearOfTheFades() {
        #expect(SessionTabStrip.liftedLeading(329, width: 220, among: spans, visible: 0...1200) == 329)
        // It stops at the first tab's leading edge and the last tab's trailing edge.
        #expect(SessionTabStrip.liftedLeading(-171, width: 220, among: spans, visible: 0...1200) == 0)
        #expect(SessionTabStrip.liftedLeading(2229, width: 220, among: spans, visible: 0...1200) == 916)
        // When tabs overflow, it stops short of the edge fades, where the strip scrolls.
        #expect(SessionTabStrip.liftedLeading(2229, width: 220, among: spans, visible: 0...404) == 184)
        #expect(SessionTabStrip.liftedLeading(-171, width: 220, among: spans, visible: 332...704) == 332)
        // In a clear span narrower than the tab, it stops once it no longer covers the span.
        #expect(SessionTabStrip.liftedLeading(250, width: 220, among: spans, visible: 300...400) == 250)
        #expect(SessionTabStrip.liftedLeading(329, width: 220, among: spans, visible: 300...400) == 300)
        #expect(SessionTabStrip.liftedLeading(100, width: 220, among: spans, visible: 300...400) == 180)
        #expect(SessionTabStrip.clearSpan(offset: 300, viewport: 500, fadingBefore: 32, fadingAfter: 96) == 332...704)
        #expect(SessionTabStrip.clearSpan(offset: 0, viewport: 500, fadingBefore: 0, fadingAfter: 96) == 0...404)
        #expect(SessionTabStrip.clearSpan(offset: 0, viewport: 100, fadingBefore: 32, fadingAfter: 96) == nil)
    }

    @Test func theStripScrollsFasterTheFartherPastTheEdgeThePointerIs() {
        #expect(SessionTabStrip.scrollSpeed(past: 0) == 0)
        #expect(SessionTabStrip.scrollSpeed(past: 20) == 200)
        #expect(SessionTabStrip.scrollSpeed(past: -60) == -600)
        #expect(SessionTabStrip.scrollSpeed(past: 500) == 1200)
        #expect(SessionTabStrip.scrollSpeed(past: -500) == -1200)
    }

    @Test func aDroppedTabSettlesIntoTheSlotItWasHeldOver() {
        let drag = SessionTabStripDrag()
        let tab = UUID()
        #expect(drag.begin(tab, at: 1, slots: spans, grab: 40))
        #expect(drag.dragging && drag.origin == 1 && drag.target == 1)
        drag.follow(leading: 600, overshoot: 0, edge: 0)
        drag.retarget(3)
        #expect(drag.drop(tab) == (true, 4))
        // The tab order now has it in its new place; the copy glides there.
        #expect(!drag.dragging && drag.settling && drag.origin == 3 && drag.target == 3)
        drag.settle()
        #expect(drag.leading == 687)
        // Its gesture's end, also reported, doesn't drop it again.
        #expect(drag.drop(tab) == (false, nil))
        drag.finish(tab)
        #expect(drag.id == nil && drag.leading == nil && !drag.settling)
    }

    @Test func lettingGoOfADraggedTabDoesntClickIt() {
        let drag = SessionTabStripDrag()
        let tab = UUID(), other = UUID()
        #expect(!drag.claimsRelease(of: tab))
        #expect(drag.begin(tab, at: 1, slots: spans, grab: 40))
        // The button may see the release before the drag ends, or after.
        #expect(drag.claimsRelease(of: tab) && !drag.claimsRelease(of: other))
        _ = drag.drop(tab)
        drag.finish(tab)
        #expect(drag.claimsRelease(of: tab))
        drag.released(tab)
        #expect(!drag.claimsRelease(of: tab))
    }

    @Test func escapeReturnsTheHeldTabUntilTheDragEnds() {
        let drag = SessionTabStripDrag()
        let tab = UUID()
        #expect(drag.begin(tab, at: 2, slots: spans, grab: 40))
        drag.follow(leading: 10, overshoot: -30, edge: -1)
        drag.retarget(0)
        #expect(drag.cancel())
        #expect(!drag.dragging && drag.target == 2 && drag.edge == 0)
        drag.settle()
        #expect(drag.leading == 458)
        drag.finish(tab)
        // The gesture keeps reporting until the button is released, or the
        // gesture is cancelled.
        #expect(!drag.begin(tab, at: 2, slots: spans, grab: 40))
        #expect(drag.drop(tab) == (false, nil))
        #expect(drag.begin(tab, at: 2, slots: spans, grab: 40))
        #expect(drag.cancel())
        #expect(!drag.cancel())
    }

    @Test func aDragEndsWhenTheTabsChange() {
        let ids = (0..<5).map { _ in UUID() }
        let drag = SessionTabStripDrag()
        #expect(drag.begin(ids[1], at: 1, slots: spans, grab: 40))
        #expect(drag.holds(ids))
        // Moving other tabs past it keeps it.
        let swapped = [ids[0], ids[1], ids[3], ids[2], ids[4]]
        drag.forget(keeping: swapped)
        #expect(drag.holds(swapped))
        // Moving it ends it, as does another tab opening, and its gesture's
        // later reports are ignored.
        let moved = [ids[1], ids[0], ids[2], ids[3], ids[4]]
        #expect(!drag.holds(moved))
        drag.forget(keeping: moved)
        #expect(drag.id == nil)
        #expect(!drag.begin(ids[1], at: 0, slots: spans, grab: 40))
        #expect(drag.drop(ids[1]) == (false, nil))
        #expect(drag.begin(ids[1], at: 1, slots: spans, grab: 40))
        #expect(!drag.holds(ids + [UUID()]))
        drag.forget(keeping: ids + [UUID()])
        #expect(drag.id == nil)
        #expect(drag.drop(ids[1]) == (false, nil))
        // A held tab that closes ends its drag; a cancelled one doesn't swallow a later drag.
        #expect(drag.begin(ids[2], at: 2, slots: spans, grab: 40))
        drag.forget(keeping: ids.filter { $0 != ids[2] })
        #expect(drag.id == nil)
        #expect(drag.begin(ids[0], at: 0, slots: spans, grab: 40))
        #expect(drag.cancel())
        drag.forget(keeping: [])
        #expect(drag.begin(ids[0], at: 0, slots: spans, grab: 40))
    }

    @Test func anotherServersTabNamesItInItsTooltipAndSpokenName() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("r", server: "vps"), id("l")], selected: id("l"))
        let remote = model.tabs[0], local = model.tabs[1]
        #expect(SessionTabHelp.text(title: remote.title, status: "Awaiting response", server: remote.serverLabel, parent: "Tiling")
            == "r — Awaiting response\nOn vps\nFrom Tiling")
        #expect(SessionTabHelp.accessibilityLabel(title: remote.title, server: remote.serverLabel) == "r, on vps")
        #expect(SessionTabHelp.text(title: local.title, status: nil, server: local.serverLabel) == "l")
        #expect(SessionTabHelp.accessibilityLabel(title: local.title, server: local.serverLabel) == "l")
        #expect(SessionTabHelp.text(title: "Design review", status: "Closed", server: "vps") == "Design review — Closed\nOn vps")
    }
}

/// How the top strip draws its tabs.
@MainActor struct SessionTopTabPresentationTests {
    private func render(_ view: some View, size: NSSize, theme: MicaTheme) async throws -> (NSBitmapImageRep, CGFloat) {
        _ = NSApplication.shared
        let host = NSHostingView(rootView: view.frame(width: size.width, height: size.height, alignment: .topLeading)
            .environment(\.mica, theme).environment(\.colorScheme, theme.dark ? .dark : .light)
            .foregroundStyle(theme.text).background(theme.frame))
        host.sizingOptions = []
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        // Render in sRGB, whatever the display, so theme colors compare exactly.
        window.colorSpace = .sRGB
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
        try #require(host.bounds.size == size)
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        return (bitmap, CGFloat(bitmap.pixelsWide) / size.width)
    }

    private func color(_ bitmap: NSBitmapImageRep, scale: CGFloat, at point: CGPoint) throws -> [CGFloat] {
        try #require(bitmap.bitsPerSample == 8 && bitmap.samplesPerPixel >= 3)
        var pixel = [Int](repeating: 0, count: bitmap.samplesPerPixel)
        bitmap.getPixel(&pixel, atX: Int(point.x * scale), y: Int(point.y * scale))
        return pixel.prefix(3).map { CGFloat($0) / 255 }
    }

    private func expectColor(_ actual: [CGFloat], _ expected: Color, _ comment: Comment) throws {
        let color = try #require(NSColor(expected).usingColorSpace(.sRGB))
        let expected = [color.redComponent, color.greenComponent, color.blueComponent]
        let close = zip(actual, expected).allSatisfy { abs($0 - $1) < 0.02 }
        #expect(close, "\(comment): \(actual) vs \(expected)")
    }

    @Test func aHairlineDividesInactiveTabsButNotTheSelectedOne() async throws {
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let model = SessionWindowModel(id: UUID(), sessions: ["a", "b", "c", "d"].map {
                SessionIdentity(server: SessionWindowModel.defaultServer, session: $0)
            }, selected: SessionIdentity(server: SessionWindowModel.defaultServer, session: "b"))
            let size = NSSize(width: 1200, height: SessionChrome.rowHeight)
            let (bitmap, scale) = try await render(SessionTopTabs(model: model, placement: .constant(.top)), size: size, theme: theme)
            // Tabs start at 90 pt (16 pt padding, the 68 pt traffic-light
            // space, and 6 pt) and repeat every 229 pt: 220 pt of tab and a
            // 9 pt slot whose 1 pt hairline is at its middle, 16 pt tall and
            // centred in the 44 pt row.
            let mid = SessionChrome.rowHeight / 2
            func divider(after index: Int) -> CGPoint { CGPoint(x: 90 + 220 + 4 + CGFloat(index) * 229 + 0.5, y: mid) }
            try expectColor(color(bitmap, scale: scale, at: divider(after: 0)), theme.frame, "beside the selected tab, before it")
            try expectColor(color(bitmap, scale: scale, at: divider(after: 1)), theme.frame, "beside the selected tab, after it")
            try expectColor(color(bitmap, scale: scale, at: divider(after: 2)), theme.border, "between inactive tabs")
            // The hairline is 16 pt tall: border 7 pt above the middle, frame 9 pt above it.
            try expectColor(color(bitmap, scale: scale, at: CGPoint(x: divider(after: 2).x, y: mid - 7)), theme.border, "hairline top")
            try expectColor(color(bitmap, scale: scale, at: CGPoint(x: divider(after: 2).x, y: mid - 9)), theme.frame, "above the hairline")
            // The selected tab is raised in the tile surface; the others sit on the frame.
            try expectColor(color(bitmap, scale: scale, at: CGPoint(x: 90 + 229 + 170, y: mid)), theme.surface, "selected tab fill")
            try expectColor(color(bitmap, scale: scale, at: CGPoint(x: 90 + 2 * 229 + 170, y: mid)), theme.frame, "inactive tab")
        }
    }
}
