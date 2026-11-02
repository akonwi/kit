import XCTest

/// Folds and reopens the dock, then answers a choice, a confirmation, and
/// guided questions from it in a session on the paired server of an isolated
/// Kit home. Run by script/test_dock.sh, which names the session. This starts
/// a real turn that asks the agent to call the interaction tools, so the
/// session's model must be signed in.
final class DockUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testDockAnswersRequests() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_DOCK_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_dock.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        let composer = app.textFields["composer"].exists ? app.textFields["composer"] : app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        composer.tap()
        composer.typeText("""
            Call select_from_user with title "Pick a color" and options Red and Green. \
            Then call confirm_from_user with title "Ship it?". \
            Then call guided_questions with title "Plan" and two questions: a select question "Size" with options Small \
            and Large, and an optional text question "Notes". Don't run anything else. Then reply with the word done.
            """)
        app.buttons["composer-action"].tap()

        // A choice: pick one, then submit.
        let dock = app.descendants(matching: .any)["interaction-dock"]
        XCTAssertTrue(dock.waitForExistence(timeout: 120), "Expected the choice in the dock")
        XCTAssertTrue(dock.staticTexts["Pick a color"].waitForExistence(timeout: 5))
        attach(app, "Choice")

        // Fold it away to read the transcript, then open it again.
        dock.buttons["collapse-dock"].tap()
        let pill = app.descendants(matching: .any)["interaction-dock-collapsed"]
        XCTAssertTrue(pill.waitForExistence(timeout: 5), "Expected the folded request")
        XCTAssertTrue(pill.staticTexts["Waiting for you"].exists)
        attach(app, "Folded")
        pill.buttons["expand-dock"].tap()
        XCTAssertTrue(dock.waitForExistence(timeout: 5))

        dock.buttons["Green"].tap()
        dock.buttons["Submit"].tap()

        // A confirmation: decline it.
        XCTAssertTrue(dock.staticTexts["Ship it?"].waitForExistence(timeout: 120), "Expected the confirmation")
        attach(app, "Confirm")
        dock.buttons["No"].tap()

        // Guided questions: a choice, then skip the optional note.
        XCTAssertTrue(dock.staticTexts["Size"].waitForExistence(timeout: 120), "Expected the guided questions")
        attach(app, "Guided 1")
        dock.buttons["Large"].tap()
        dock.buttons["Next"].tap()
        XCTAssertTrue(dock.staticTexts["Notes"].waitForExistence(timeout: 5))
        attach(app, "Guided 2")
        dock.buttons["Skip"].tap()

        // The composer returns.
        XCTAssertTrue(waitForAbsence(dock, timeout: 120), "Expected the dock to leave")
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        attach(app, "Answered")
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
