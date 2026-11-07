import XCTest

/// Switches a session's transcript between Main and its subagents, and
/// dismisses one, on the paired server of an isolated Kit home whose main
/// agent started five subagents: tester failed, reviewer is working, and
/// scout, librarian, and designer are done. Run by script/test_subagents.sh.
final class SubagentsUITests: XCTestCase {
    private var title = ""

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let title = ProcessInfo.processInfo.environment["KIT_SUBAGENTS_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_subagents.sh against an isolated server.")
        }
        self.title = title
    }

    @MainActor
    func test1SwitchBetweenAgents() throws {
        let app = open()

        // Main is pinned first; then the agent that needs a look, the one
        // working, and the ones done, in the order they were started.
        let names = ["Main", "tester", "reviewer", "scout", "librarian", "designer"]
        let chips = names.map { app.buttons["agent-\($0)"] }
        XCTAssertTrue(chips[0].waitForExistence(timeout: 15), "The agent strip shows\n\(app.debugDescription)")
        XCTAssertEqual(chips.map(\.label), ["Main", "tester, failed", "reviewer, working", "scout, done", "librarian, done", "designer, done"])
        XCTAssertEqual(chips.map(\.frame.minX), chips.map(\.frame.minX).sorted())
        XCTAssertEqual(chips.map(\.isSelected), [true, false, false, false, false, false])
        XCTAssertTrue(app.textViews["composer"].exists || app.textFields["composer"].exists)
        attach(app, "Main")

        // An agent's chip shows its transcript, with the agent bar in place
        // of the composer.
        chips[2].tap()
        XCTAssertTrue(waitForSelection(chips[2]))
        let bar = app.descendants(matching: .any)["agent-bar"]
        XCTAssertTrue(bar.waitForExistence(timeout: 5))
        XCTAssertEqual(bar.label, "reviewer · GPT-5.5 · High, Its results go to Main")
        XCTAssertFalse(app.textViews["composer"].exists || app.textFields["composer"].exists)
        XCTAssertTrue(app.staticTexts["Task from the main agent"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Review the SSE resume change")).firstMatch.exists)
        attach(app, "reviewer")

        // Its menu locks the model while it works.
        app.buttons["agent-menu"].tap()
        let model = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Model")).firstMatch
        XCTAssertTrue(model.waitForExistence(timeout: 5))
        attach(app, "reviewer menu")
        XCTAssertFalse(model.isEnabled)
        XCTAssertEqual(model.label, "Model, Can change once reviewer’s work finishes")
        XCTAssertTrue(app.buttons["dismiss-agent"].exists)
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3)).tap()
        XCTAssertTrue(waitForAbsence(app.buttons["dismiss-agent"]))

        // A failed agent's transcript.
        chips[1].tap()
        XCTAssertTrue(waitForSelection(chips[1]))
        XCTAssertTrue(bar.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["tester’s last task failed."].waitForExistence(timeout: 5))
        attach(app, "tester")

        // Main comes back with its composer.
        chips[0].tap()
        XCTAssertTrue(waitForSelection(chips[0]))
        XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 5) || app.textFields["composer"].exists)

        // A Start agent row in Main's transcript opens its agent.
        let link = app.buttons["open-agent-librarian"]
        XCTAssertTrue(link.waitForExistence(timeout: 5), "Main's transcript links librarian")
        link.tap()
        XCTAssertTrue(waitForSelection(chips[4]))
        XCTAssertTrue(bar.waitForExistence(timeout: 5))
        XCTAssertTrue(bar.label.hasPrefix("librarian · "), bar.label)
        attach(app, "librarian")
    }

    /// Runs last: it changes the session.
    @MainActor
    func test2DismissAnAgent() throws {
        let app = open()
        let designer = app.buttons["agent-designer"]
        XCTAssertTrue(designer.waitForExistence(timeout: 15))
        app.buttons["open-agent-designer"].tap()
        XCTAssertTrue(waitForSelection(designer))

        app.buttons["agent-menu"].tap()
        let dismiss = app.buttons["dismiss-agent"]
        XCTAssertTrue(dismiss.waitForExistence(timeout: 5))
        dismiss.tap()
        let alert = app.alerts["Dismiss “designer”?"]
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        attach(app, "Dismiss designer")
        alert.buttons["Dismiss"].tap()

        // Main comes back, and designer leaves the strip.
        XCTAssertTrue(waitForAbsence(designer, timeout: 15))
        XCTAssertTrue(app.buttons["agent-Main"].isSelected)
        // The strip is back at its start.
        XCTAssertTrue(app.buttons["agent-tester"].isHittable)
        XCTAssertTrue(app.textViews["composer"].exists || app.textFields["composer"].exists)
        attach(app, "Dismissed")
    }

    // MARK: Helpers

    @MainActor
    private func open() -> XCUIApplication {
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()
        XCTAssertTrue(app.buttons["session-menu"].waitForExistence(timeout: 15))
        return app
    }

    private func waitForSelection(_ element: XCUIElement, timeout: TimeInterval = 5) -> Bool {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "isSelected == true"), object: element)
        return XCTWaiter().wait(for: [expectation], timeout: timeout) == .completed
    }

    private func waitForAbsence(_ element: XCUIElement, timeout: TimeInterval = 5) -> Bool {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: element)
        return XCTWaiter().wait(for: [expectation], timeout: timeout) == .completed
    }

    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
