import XCTest

/// Reads a plugin confirmation that is longer and wider than the dock, in a
/// session on the paired server of an isolated Kit home: the whole title, a
/// command and a table that scroll sideways, and the message's last line, then
/// folds the dock with a swipe on the title and declines. Run by
/// script/test_dock_overflow.sh, which names the session and asks the
/// confirmation from its fixture plugin.
final class DockOverflowUITests: XCTestCase {
    private let title = "Deploy the release branch to production, run the database migrations, "
        + "and restart every worker in all three regions?"

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testDockShowsAllOfALongRequest() throws {
        guard let session = ProcessInfo.processInfo.environment["KIT_DOCK_SESSION"], !session.isEmpty else {
            throw XCTSkip("Run script/test_dock_overflow.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", session)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(session)")
        row.tap()

        let dock = app.descendants(matching: .any)["interaction-dock"]
        XCTAssertTrue(dock.waitForExistence(timeout: 30), "Expected the plugin's confirmation in the dock")
        attach(app, "Dock")

        // The title wraps in full across the dock, with the plugin below it.
        let heading = dock.staticTexts[title]
        XCTAssertTrue(heading.exists)
        XCTAssertGreaterThan(heading.frame.height, 3 * 20, "Expected the title on more than three lines")
        XCTAssertGreaterThanOrEqual(heading.frame.minX, dock.frame.minX)
        XCTAssertLessThanOrEqual(heading.frame.maxX, dock.frame.maxX)
        let plugin = dock.staticTexts["dock-overflow"]
        XCTAssertTrue(plugin.exists)
        XCTAssertGreaterThanOrEqual(plugin.frame.minY, heading.frame.maxY - 1)

        // The message scrolls in the tallest area of the dock.
        let detail = try XCTUnwrap(dock.scrollViews.allElementsBoundByIndex.max { $0.frame.height < $1.frame.height })

        // The command runs off the side and scrolls to its end.
        let command = dock.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "kubectl")).firstMatch
        XCTAssertTrue(command.exists)
        XCTAssertGreaterThan(command.frame.maxX, detail.frame.maxX, "Expected the command wider than the dock")
        let commandStart = command.frame.minX
        swipeSideways(app, in: detail, at: command.frame.midY)
        XCTAssertLessThan(command.frame.minX, commandStart - 100, "Expected the command to scroll sideways")
        attach(app, "Command scrolled")

        // So does the table, once it's scrolled into view.
        let cell = dock.staticTexts["production-ap-southeast-2-blue"]
        scroll(app, detail, until: { cell.frame.minY >= detail.frame.minY && cell.frame.maxY <= detail.frame.maxY })
        let downtime = dock.staticTexts["about 30 seconds"]
        let downtimeStart = downtime.frame.minX
        swipeSideways(app, in: detail, at: cell.frame.midY)
        XCTAssertLessThan(downtime.frame.minX, downtimeStart - 20, "Expected the table to scroll sideways")
        attach(app, "Table scrolled")

        // The message reads to its last line.
        let last = dock.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Line 1 of a long note")).firstMatch
        scroll(app, detail, until: { last.exists && last.frame.maxY <= detail.frame.maxY + 1 })
        XCTAssertTrue(last.label.hasSuffix("The last line of the message."))
        XCTAssertGreaterThanOrEqual(last.frame.minY, detail.frame.minY)
        attach(app, "End")

        // A swipe down on the title folds the dock; then decline.
        heading.swipeDown()
        let pill = app.descendants(matching: .any)["interaction-dock-collapsed"]
        XCTAssertTrue(pill.waitForExistence(timeout: 5), "Expected a swipe on the title to fold the dock")
        pill.buttons["expand-dock"].tap()
        XCTAssertTrue(dock.waitForExistence(timeout: 5))
        // Decline it, and any left by an earlier run that stopped before answering.
        for _ in 0..<5 where dock.exists {
            dock.buttons["Not now"].tap()
            _ = waitForAbsence(dock, timeout: 5)
        }
        XCTAssertTrue(waitForAbsence(dock, timeout: 30), "Expected the dock to leave")
        let composer = app.textFields["composer"].exists ? app.textFields["composer"] : app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
    }

    /// Drags right to left across `area` at the height `y`.
    @MainActor
    private func swipeSideways(_ app: XCUIApplication, in area: XCUIElement, at y: CGFloat) {
        let origin = app.coordinate(withNormalizedOffset: .zero)
        let start = origin.withOffset(CGVector(dx: area.frame.maxX - 30, dy: y))
        start.press(forDuration: 0.05, thenDragTo: origin.withOffset(CGVector(dx: area.frame.minX + 30, dy: y)))
    }

    /// Drags `area` up a third of its height at a time until `done`.
    @MainActor
    private func scroll(_ app: XCUIApplication, _ area: XCUIElement, until done: () -> Bool) {
        for _ in 0..<8 where !done() {
            let start = area.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7))
            start.press(forDuration: 0.05, thenDragTo: area.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.37)))
        }
        XCTAssertTrue(done(), "Expected to scroll far enough")
    }

    private func waitForAbsence(_ element: XCUIElement, timeout: TimeInterval) -> Bool {
        let predicate = NSPredicate(format: "exists == false")
        return XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: predicate, object: element)], timeout: timeout) == .completed
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
