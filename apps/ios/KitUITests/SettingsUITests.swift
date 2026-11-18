import XCTest

/// The Settings tab chooses the composer's send button style. Needs the app
/// paired with a server, as after script/test_pairing.sh.
final class SettingsUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testTheSendButtonStyleIsChosenInSettings() throws {
        let app = XCUIApplication.withoutDrafts()
        app.launch()
        let tab = app.tabBars.buttons["Settings"]
        guard tab.waitForExistence(timeout: 15) else {
            throw XCTSkip("Pair the app with a server first, as script/test_pairing.sh does.")
        }
        tab.tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 5))
        let hold = app.buttons["Press and Hold"]
        let split = app.buttons["Split Button"]
        XCTAssertTrue(hold.waitForExistence(timeout: 5))
        XCTAssertTrue(split.exists)
        XCTAssertTrue(hold.isSelected, "Press and Hold is the default")
        XCTAssertTrue(app.staticTexts["Press and hold the send button for attachments and commands."].exists)
        attach(app, "Press and Hold")

        split.tap()
        XCTAssertTrue(split.isSelected)
        XCTAssertTrue(app.staticTexts["Tap the arrow beside the send button for attachments and commands."].exists)
        attach(app, "Split Button")

        hold.tap()
        XCTAssertTrue(hold.isSelected)
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
