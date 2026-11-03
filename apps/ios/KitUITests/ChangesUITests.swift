import XCTest

/// Opens Changes from the session menu in a session whose folder is a git
/// repository with uncommitted changes, on the paired server of an isolated
/// Kit home. It reads the diff, folds a file, wraps lines, and compares a
/// recent commit. Run by script/test_changes.sh, which names the session, a
/// file the uncommitted changes touch, and a commit to compare.
final class ChangesUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testChanges() throws {
        let environment = ProcessInfo.processInfo.environment
        guard let title = environment["KIT_CHANGES_SESSION"], !title.isEmpty,
              let file = environment["KIT_CHANGES_FILE"], !file.isEmpty,
              let commit = environment["KIT_CHANGES_COMMIT"], !commit.isEmpty else {
            throw XCTSkip("Run script/test_changes.sh against an isolated server with a session in a git repository.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // The menu counts the changes before they open.
        let menu = app.buttons["session-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 15))
        menu.tap()
        let changes = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Changes")).firstMatch
        XCTAssertTrue(changes.waitForExistence(timeout: 5))
        attach(app, "Menu")
        XCTAssertTrue(waitFor(changes, labelContaining: "files", timeout: 10), "Expected a file count, got \(changes.label)")
        changes.tap()

        // Uncommitted changes, every file in one scroll.
        XCTAssertTrue(app.descendants(matching: .any)["changes-summary"].waitForExistence(timeout: 15))
        XCTAssertEqual(app.buttons["changes-target"].label, "Compare: Uncommitted")
        let header = fileHeader(app, file)
        XCTAssertTrue(header.waitForExistence(timeout: 10), "Expected \(file) in the changes")
        XCTAssertTrue(app.descendants(matching: .any)["diff-lines"].firstMatch.waitForExistence(timeout: 10))
        attach(app, "Uncommitted")

        // A tap on a file's header folds it, and another opens it.
        header.tap()
        XCTAssertTrue(waitFor(fileHeader(app, file), labelContaining: "folded", timeout: 5))
        attach(app, "Folded")
        fileHeader(app, file).tap()
        XCTAssertTrue(waitForAbsence(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ AND label CONTAINS %@", file, "folded")).firstMatch, timeout: 5))

        // Wrap lines, then back.
        app.buttons["changes-menu"].tap()
        let wrap = app.switches["Wrap lines"].exists ? app.switches["Wrap lines"] : app.buttons["Wrap lines"]
        XCTAssertTrue(wrap.waitForExistence(timeout: 5))
        wrap.tap()
        attach(app, "Wrapped")
        app.buttons["changes-menu"].tap()
        (app.switches["Wrap lines"].exists ? app.switches["Wrap lines"] : app.buttons["Wrap lines"]).tap()

        // Compare a recent commit.
        app.buttons["changes-target"].tap()
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Recent commits"].waitForExistence(timeout: 10) || app.staticTexts["RECENT COMMITS"].exists)
        attach(app, "Compare")
        let pick = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", commit)).firstMatch
        XCTAssertTrue(pick.waitForExistence(timeout: 5), "Expected \(commit) among the commits")
        pick.tap()
        XCTAssertTrue(waitFor(app.buttons["changes-target"], labelContaining: "Compare: ", timeout: 5))
        XCTAssertTrue(waitForAbsence(search, timeout: 5))
        let committed = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "greet.go")).firstMatch
        XCTAssertTrue(committed.waitForExistence(timeout: 10), "Expected the commit's file")
        XCTAssertNotEqual(app.buttons["changes-target"].label, "Compare: Uncommitted")
        attach(app, "Commit")

        // Back to uncommitted, so the next run starts there.
        app.buttons["changes-target"].tap()
        let uncommitted = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Uncommitted changes")).firstMatch
        XCTAssertTrue(uncommitted.waitForExistence(timeout: 5))
        uncommitted.tap()
        XCTAssertTrue(waitFor(app.buttons["changes-target"], labelContaining: "Uncommitted", timeout: 5))

        // Back to the session, whose composer stays live.
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(menu.waitForExistence(timeout: 5))
        XCTAssertTrue(app.textViews.matching(NSPredicate(format: "placeholderValue == %@", "Ask Kit…")).firstMatch.waitForExistence(timeout: 5)
            || app.textFields.matching(NSPredicate(format: "placeholderValue == %@", "Ask Kit…")).firstMatch.exists)
    }

    private func fileHeader(_ app: XCUIApplication, _ path: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier == %@ AND label BEGINSWITH %@", "diff-file", path)).firstMatch
    }

    private func waitFor(_ element: XCUIElement, labelContaining text: String, timeout: TimeInterval) -> Bool {
        let predicate = NSPredicate(format: "label CONTAINS %@", text)
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
