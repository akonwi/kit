import XCTest

/// Sends, queues, and stops in a session on the paired server of an isolated
/// Kit home. Run by script/test_composer.sh, which names the session; the
/// session's model must be signed in, since this starts real turns.
final class ComposerUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testComposerSendsQueuesAndStops() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_COMPOSER_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_composer.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        let composer = app.textFields["composer"].exists ? app.textFields["composer"] : app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        let action = app.buttons["composer-action"]
        XCTAssertTrue(action.waitForExistence(timeout: 15))
        XCTAssertFalse(action.isEnabled, "Send should be grey with an empty draft")
        attach(app, "Idle")

        composer.tap()
        composer.typeText("Use bash to run `sleep 45`, then reply with the single word done.")
        XCTAssertTrue(action.isEnabled)
        attach(app, "Typing")
        action.tap()

        // The turn starts; with an empty draft the button is Stop.
        XCTAssertTrue(waitFor(action, label: "Stop", timeout: 60), "Expected Stop while the turn runs")
        attach(app, "Running")

        composer.tap()
        composer.typeText("Then reply with the word queued.")
        XCTAssertTrue(waitFor(action, label: "Send", timeout: 5), "Expected Send while there's text")
        attach(app, "Running with text")
        action.tap()

        let tray = app.otherElements["queued-tray"]
        XCTAssertTrue(tray.waitForExistence(timeout: 20), "Expected the queued tray")
        XCTAssertTrue(waitFor(action, label: "Stop", timeout: 10))
        attach(app, "Queued")

        // Clear the queue, then stop the run.
        app.buttons["Clear"].tap()
        XCTAssertTrue(waitForAbsence(tray, timeout: 15), "Expected Clear to empty the queue")
        action.tap()
        XCTAssertTrue(waitFor(action, label: "Send", timeout: 30), "Expected the run to stop")
        attach(app, "Stopped")
    }

    private func waitFor(_ element: XCUIElement, label: String, timeout: TimeInterval) -> Bool {
        let predicate = NSPredicate(format: "label == %@", label)
        return XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: predicate, object: element)], timeout: timeout) == .completed
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
