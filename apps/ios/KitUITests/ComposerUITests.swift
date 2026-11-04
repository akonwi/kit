import XCTest

/// Sends, queues, and stops in a session on the paired server of an isolated
/// Kit home; follows a sent message and its reply, and holds still while the
/// reader looks back. Run by script/test_composer.sh, which names the session; the
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

    /// Sending from partway up the transcript shows the message, and the
    /// reply stays in view above the composer as it streams.
    @MainActor
    func testSendingShowsTheMessageAndReply() throws {
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
        app.scrollViews.firstMatch.swipeDown(velocity: .slow)
        attach(app, "Scrolled up")

        let mark = String(Int(Date().timeIntervalSince1970) % 100_000)
        let prompt = "Write a bulleted list of the numbers 1 to 30, then a last line with the word end\(mark). No other text."
        composer.tap()
        composer.typeText(prompt)
        app.buttons["composer-action"].tap()

        let message = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", prompt)).firstMatch
        XCTAssertTrue(message.waitForExistence(timeout: 15), "Expected the sent message")
        XCTAssertLessThanOrEqual(message.frame.maxY, composer.frame.minY, "Expected the message above the composer")
        attach(app, "Sent")

        let reply = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@ AND NOT label CONTAINS %@", "end" + mark, "Write")).firstMatch
        XCTAssertTrue(reply.waitForExistence(timeout: 90), "Expected the reply")
        XCTAssertTrue(waitFor(app.buttons["composer-action"], label: "Send", timeout: 60), "Expected the turn to end")
        XCTAssertLessThanOrEqual(reply.frame.maxY, composer.frame.minY, "Expected the reply's end above the composer")
        attach(app, "Replied")
    }

    /// Scrolling up while a reply streams stops following: the rows the
    /// reader scrolled to stay where they are until the turn ends.
    @MainActor
    func testReadingEarlierStaysPutWhileAReplyStreams() throws {
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
        let mark = String(Int(Date().timeIntervalSince1970) % 100_000)

        // A short reply to read back to.
        composer.tap()
        composer.typeText("Reply with the single word anchor\(mark).")
        action.tap()
        let anchor = app.staticTexts.matching(NSPredicate(format: "label == %@", "anchor" + mark)).firstMatch
        XCTAssertTrue(anchor.waitForExistence(timeout: 60), "Expected the short reply")
        XCTAssertTrue(waitFor(action, label: "Send", timeout: 30), "Expected the first turn to end")

        let prompt = "Use bash to run `sleep 15`, then write a bulleted list of the numbers 1 to 40."
        composer.typeText(prompt)
        action.tap()
        XCTAssertTrue(waitFor(action, label: "Stop", timeout: 30), "Expected Stop while the turn runs")

        // Scroll back up a little, to the short reply, while the command runs.
        let message = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", prompt)).firstMatch
        XCTAssertTrue(message.waitForExistence(timeout: 10))
        let start = message.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        start.press(forDuration: 0.2, thenDragTo: start.withOffset(CGVector(dx: 0, dy: 120)),
                    withVelocity: .slow, thenHoldForDuration: 0.5)
        attach(app, "Reading earlier")
        XCTAssertTrue(anchor.isHittable, "Expected the short reply in view")
        let top = anchor.frame.minY

        XCTAssertTrue(waitFor(action, label: "Send", timeout: 90), "Expected the turn to end")
        XCTAssertTrue(anchor.isHittable, "Expected the short reply still in view")
        XCTAssertEqual(anchor.frame.minY, top, accuracy: 2, "Expected the short reply where the reader left it")
        attach(app, "After the reply")
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
