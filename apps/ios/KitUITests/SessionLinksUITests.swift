import XCTest

/// Opens a session from a mention in a transcript, on the paired server of an
/// isolated Kit home whose model is the stand-in in
/// script/fixtures/long_reply_model.py. It sends a message that mentions the
/// second session, taps the mention, which reads as that session's title, and
/// comes back. Run by script/test_session_links.sh, which names the sessions.
final class SessionLinksUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testAMentionOpensItsSession() throws {
        let environment = ProcessInfo.processInfo.environment
        guard let title = environment["KIT_LINKS_SESSION"], let target = environment["KIT_LINKS_TARGET"],
              let targetID = environment["KIT_LINKS_TARGET_ID"] else {
            throw XCTSkip("Run script/test_session_links.sh against an isolated server with two sessions.")
        }
        let app = XCUIApplication.withoutDrafts()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // The mention reads as # and the session's title.
        let composer = app.descendants(matching: .any)["composer"].firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        composer.tap()
        composer.typeText("Ask #[session:\(targetID)] about the cursor")
        app.buttons["composer-action"].tap()
        let mention = app.links["#\(target)"].firstMatch
        XCTAssertTrue(mention.waitForExistence(timeout: 30), "Expected the mention as #\(target)")
        attach(app, "Mention")

        // Tapping it opens that session over this one.
        mention.tap()
        XCTAssertTrue(header(app, target).waitForExistence(timeout: 15), "Expected \(target) to open")
        attach(app, "Opened")

        // Back returns to the session that mentioned it.
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(header(app, title).waitForExistence(timeout: 10), "Expected to come back to \(title)")
        attach(app, "Back")
    }

    private func header(_ app: XCUIApplication, _ text: String) -> XCUIElement {
        app.navigationBars.staticTexts.matching(NSPredicate(format: "label == %@", text)).firstMatch
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
