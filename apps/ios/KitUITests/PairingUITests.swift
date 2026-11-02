import XCTest

/// Pairs with a real Kit server. Run through `script/test_pairing.sh`, which
/// issues a fresh link from an isolated Kit home; without one this skips.
final class PairingUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    /// Runs first, on a fresh install, so it starts at first run.
    @MainActor
    func testFirstRunScansOrPastes() throws {
        let app = XCUIApplication()
        app.launch()

        let scan = app.buttons["Scan pairing code"]
        XCTAssertTrue(scan.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Paste pairing link"].exists)
        attach(app, "First run")
        scan.tap()

        let pasteInstead = app.buttons["Paste link instead"]
        XCTAssertTrue(pasteInstead.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Scan the code from kit server pair"].exists)
        // A simulator has no camera, so the scanner points to pasting.
        XCTAssertTrue(app.staticTexts["This device has no camera. Paste the link instead."].exists)
        attach(app, "Scanner")
        pasteInstead.tap()

        // The pair sheet opens once the scanner has gone.
        XCTAssertTrue(app.navigationBars["Pair server"].waitForExistence(timeout: 10))
        let field = app.descendants(matching: .any).matching(identifier: "pairing-link").firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5), "The sheet asks for a link\n\(app.debugDescription)")
        attach(app, "Paste link")
    }

    @MainActor
    func testPairingWithALinkShowsTheServerOnline() throws {
        guard let link = ProcessInfo.processInfo.environment["KIT_PAIRING_LINK"], !link.isEmpty else {
            throw XCTSkip("Run script/test_pairing.sh to pair with an isolated server.")
        }
        let host = try XCTUnwrap(URLComponents(string: link)?.host)

        let app = XCUIApplication()
        app.launchEnvironment["KIT_PAIRING_LINK"] = link
        app.launch()

        let pair = app.buttons["Pair"]
        XCTAssertTrue(pair.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Server name"].exists)
        XCTAssertTrue(app.staticTexts["This iPhone as"].exists)
        attach(app, "Confirm")
        pair.tap()

        let row = app.staticTexts[host]
        XCTAssertTrue(row.waitForExistence(timeout: 15), "The paired server is listed")
        let online = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "http://\(host)")).firstMatch
        XCTAssertTrue(online.waitForExistence(timeout: 15), "The server is shown online with its address and version")
        XCTAssertTrue(online.label.contains(" · Kit "), online.label)
        attach(app, "Servers")

        row.tap()
        XCTAssertTrue(app.staticTexts["Status, Online"].waitForExistence(timeout: 10))
        attach(app, "Server detail")
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
