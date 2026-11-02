import XCTest

/// Opens sessions on the paired server of an isolated Kit home, and selects
/// text from a message. Run by script/test_sessions.sh, which names the
/// sessions to open.
final class SessionsUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testSessionsListOpensATranscript() throws {
        guard let titles = ProcessInfo.processInfo.environment["KIT_SESSION_TITLES"], !titles.isEmpty else {
            throw XCTSkip("Run script/test_sessions.sh against an isolated server with sessions.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        attach(app, "Sessions")

        for title in titles.split(separator: "|").map(String.init) {
            let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
            XCTAssertTrue(row.waitForExistence(timeout: 15), "\(title) is listed")
            row.tap()
            XCTAssertTrue(app.staticTexts[title].waitForExistence(timeout: 15), "\(title) opens")
            sleep(3)
            attach(app, title)
            app.navigationBars.buttons.element(boundBy: 0).tap()
            XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 10))
        }
        attach(app, "Sessions after reading")
    }

    /// A long press on a message offers Copy and Select Text, which opens the
    /// message to select part of it.
    @MainActor
    func testAMessageOpensToSelectText() throws {
        guard let titles = ProcessInfo.processInfo.environment["KIT_SESSION_TITLES"], !titles.isEmpty,
              let title = titles.split(separator: "|").first.map(String.init) else {
            throw XCTSkip("Run script/test_sessions.sh against an isolated server with sessions.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch.tap()
        XCTAssertTrue(app.buttons["session-menu"].waitForExistence(timeout: 15))
        sleep(2)

        let message = app.staticTexts.matching(identifier: "prose").allElementsBoundByIndex.last { $0.isHittable }
        try XCTUnwrap(message, "Expected a message on screen").press(forDuration: 1.2)
        XCTAssertTrue(app.buttons["Copy"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Select Text"].exists)
        attach(app, "Message menu")
        app.buttons["Select Text"].tap()

        let body = app.textViews["select-text-body"]
        XCTAssertTrue(body.waitForExistence(timeout: 5))
        XCTAssertFalse((body.value as? String ?? "").isEmpty)
        body.press(forDuration: 1.2)
        attach(app, "Select Text")
        app.buttons["Done"].tap()
        XCTAssertTrue(waitForAbsence(body, timeout: 5))
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
