import XCTest

/// Reads the detail of each kind of model question in the dock, in a session
/// on the paired server of an isolated Kit home whose model is the stand-in
/// in script/fixtures/questions_model.py: a confirmation, a typed answer, a
/// choice with an option's detail, and guided questions with the set's title
/// and detail above each question. Run by script/test_dock_detail.sh, which
/// names the session and starts the turn that asks them.
final class DockDetailUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testTheDockShowsEachQuestionsDetail() throws {
        guard let session = ProcessInfo.processInfo.environment["KIT_DOCK_DETAIL_SESSION"], !session.isEmpty else {
            throw XCTSkip("Run script/test_dock_detail.sh against an isolated server with a session.")
        }
        let app = XCUIApplication.withoutDrafts()
        app.launch()
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", session)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(session)")
        row.tap()
        let dock = app.descendants(matching: .any)["interaction-dock"]

        // Each single question shows its title and, below it, its detail; a
        // choice also shows an option's detail. Each is declined to move on.
        for (title, detail, name) in [
            ("Push the branch?", "It is 3 commits ahead of origin/main.", "Confirmation"),
            ("Name the release", "Use the version, like v0.67.0.", "Typed answer"),
            ("Which build?", "Pick where the build goes.", "Choice"),
        ] {
            let heading = dock.staticTexts[title]
            XCTAssertTrue(heading.waitForExistence(timeout: 30), "Expected the \(name.lowercased())")
            let body = dock.staticTexts[detail]
            XCTAssertTrue(body.exists, "Expected the \(name.lowercased())'s detail")
            XCTAssertGreaterThan(body.frame.minY, heading.frame.maxY - 1)
            if name == "Choice" {
                XCTAssertTrue(dock.staticTexts["Internal testers get it."].exists, "Expected the option's detail")
            }
            attach(app, name)
            dock.buttons["Cancel request"].tap()
        }

        // Guided questions show the set's title and detail, then the
        // question and its own detail, on each step.
        let set = dock.staticTexts["Release setup"]
        XCTAssertTrue(set.waitForExistence(timeout: 30), "Expected the guided questions")
        let setDetail = dock.staticTexts["Two questions before the release."]
        XCTAssertTrue(setDetail.exists, "Expected the set's detail")
        let first = dock.staticTexts["Release notes?"]
        XCTAssertTrue(first.exists)
        XCTAssertTrue(dock.staticTexts["One line is enough."].exists, "Expected the question's detail")
        XCTAssertGreaterThan(setDetail.frame.minY, set.frame.maxY - 1)
        XCTAssertGreaterThan(first.frame.minY, setDetail.frame.maxY - 1)
        attach(app, "Guided, first question")

        let field = dock.textFields["interaction-text"].exists ? dock.textFields["interaction-text"] : dock.textViews["interaction-text"]
        field.tap()
        field.typeText("Fixes the dock")
        dock.buttons["Next"].tap()
        XCTAssertTrue(dock.staticTexts["Ship it?"].waitForExistence(timeout: 5))
        XCTAssertTrue(dock.staticTexts["Release setup"].exists)
        XCTAssertTrue(dock.staticTexts["Two questions before the release."].exists, "Expected the set's detail on every step")
        attach(app, "Guided, second question")

        dock.buttons["Skip questions"].tap()
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: dock)
        XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: 30), .completed, "Expected the dock to leave")
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
