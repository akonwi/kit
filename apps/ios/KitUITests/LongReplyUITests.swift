import XCTest

/// Sends two messages in a session on the paired server of an isolated Kit
/// home whose model is the stand-in in script/fixtures/long_reply_model.py.
/// The first one's reply is several screens long and shows from its top; the
/// second one's fits, so the transcript follows it to the end. Each message
/// carries a #tag that the model repeats, so the session can be reused. Run by
/// script/test_long_reply.sh, which names the session.
final class LongReplyUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testALongReplyShowsFromItsTop() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_LONG_REPLY_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_long_reply.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // A long reply opens at its first line, just below the header.
        let run = String(Int(Date().timeIntervalSince1970))
        send(app, "Explain the reconnect fix, long form #\(run)")
        let top = text(app, "LONG-REPLY-TOP \(run)")
        XCTAssertTrue(top.waitForExistence(timeout: 60), "Expected the long reply")
        let header = app.navigationBars.firstMatch.frame.maxY
        let placed = waitFor(timeout: 5) { top.frame.minY >= header - 1 && top.frame.minY < app.frame.height * 0.4 }
        attach(app, "Long reply")
        XCTAssertTrue(placed, "Expected the reply's top in view, near the top of the screen; it's at \(top.frame.minY)")
        // It stays there as the rows settle.
        sleep(2)
        XCTAssertGreaterThanOrEqual(top.frame.minY, header - 1, "The reply's top scrolled away")
        XCTAssertLessThan(top.frame.minY, app.frame.height * 0.4)

        // A reply that fits follows on to the end, as before.
        send(app, "Thanks, ship it #\(run)")
        let short = text(app, "SHORT-REPLY \(run)")
        XCTAssertTrue(short.waitForExistence(timeout: 60), "Expected the short reply")
        let composer = app.descendants(matching: .any)["composer"].firstMatch
        let shown = waitFor(timeout: 5) { short.isHittable && short.frame.maxY <= composer.frame.minY }
        attach(app, "Short reply")
        XCTAssertTrue(shown, "Expected the short reply in view above the composer")
    }

    @MainActor
    private func send(_ app: XCUIApplication, _ message: String) {
        let composer = app.descendants(matching: .any)["composer"].firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        composer.tap()
        composer.typeText(message)
        app.buttons["composer-action"].tap()
        // Drop the keyboard so the transcript has its full height.
        if app.keyboards.firstMatch.exists {
            app.navigationBars.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        }
    }

    /// The reply that carries `marker`: the stand-in model repeats the
    /// message's #tag, so earlier runs' replies don't match.
    private func text(_ app: XCUIApplication, _ marker: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", marker)).firstMatch
    }

    private func waitFor(timeout: TimeInterval, _ condition: @escaping () -> Bool) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if condition() { return true }
            RunLoop.current.run(until: Date().addingTimeInterval(0.25))
        }
        return condition()
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
