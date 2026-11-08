import XCTest

/// Starts a session from the Sessions list on the paired server of an
/// isolated Kit home: the folder field offers the server's recent folders, a
/// folder the server doesn't have is refused under it, then a typed folder
/// that exists creates a named session, which opens ready for
/// its first message and is deleted again. Run by script/test_new_session.sh,
/// which names the folder. Nothing here starts a turn.
final class NewSessionUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testNewSession() throws {
        guard let folder = ProcessInfo.processInfo.environment["KIT_NEW_SESSION_FOLDER"], !folder.isEmpty else {
            throw XCTSkip("Run script/test_new_session.sh against an isolated server with a folder it can start in.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))

        // The form opens on the latest session's folder and the server's default model.
        let plus = app.buttons["new-session"]
        XCTAssertTrue(plus.waitForExistence(timeout: 15))
        XCTAssertTrue(waitFor(plus, enabled: true, timeout: 15))
        plus.tap()
        XCTAssertTrue(app.navigationBars["New session"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Model"].waitForExistence(timeout: 15), "Expected the models to load")
        let folderField = app.textFields["new-session-folder"]
        XCTAssertTrue(folderField.exists, "Expected one folder field")
        attach(app, "Form")

        // Choosing a folder offers the server's recent folders in place of the rest of the form.
        folderField.tap()
        let footer = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "is your home folder on")).firstMatch
        XCTAssertTrue(footer.waitForExistence(timeout: 5), "Expected the field to say where ~ points")
        XCTAssertFalse(app.buttons["Model"].exists, "Expected the folder to take the sheet while it is chosen")
        attach(app, "Choosing a folder")

        // A folder the server doesn't have is refused under the field, and the form stays open.
        let missing = "~/kit-ios-no-such-folder"
        type(app, folder: missing)
        XCTAssertTrue(app.buttons["Model"].waitForExistence(timeout: 5), "Expected the form back after Done")
        app.buttons["new-session-create"].tap()
        let refusal = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "No folder at \(missing) on ")).firstMatch
        XCTAssertTrue(refusal.waitForExistence(timeout: 15), "Expected the server to refuse \(missing)")
        attach(app, "No such folder")

        // An empty field offers every recent folder, and typing narrows them
        // to those containing it.
        folderField.tap()
        let clear = app.buttons["Clear"]
        if clear.waitForExistence(timeout: 2) { clear.tap() }
        let suggestion = app.buttons["new-session-suggestion"].firstMatch
        if suggestion.waitForExistence(timeout: 5) {
            let label = suggestion.label
            folderField.typeText(String(label.dropLast(2)))
            let narrowed = app.buttons.matching(identifier: "new-session-suggestion")
            XCTAssertTrue(narrowed.matching(NSPredicate(format: "label == %@", label)).firstMatch.waitForExistence(timeout: 5),
                          "Expected \(label) to stay offered")
            attach(app, "Narrowed")
        }

        // A folder it has creates the session, named, which opens with the field focused.
        type(app, folder: folder)
        XCTAssertFalse(refusal.exists, "Expected a new folder to clear the refusal")
        let name = "New session test \(Int(Date().timeIntervalSince1970) % 100_000)"
        let field = app.textFields["new-session-name"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(name)
        app.buttons["new-session-create"].tap()
        XCTAssertTrue(header(app, name).waitForExistence(timeout: 20), "Expected the new session to open")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "Expected the composer to be focused")
        let composer = app.textFields["composer"].exists ? app.textFields["composer"] : app.textViews["composer"]
        XCTAssertEqual(composer.placeholderValue, "Ask Kit…", "Expected the Mac's composer placeholder")
        attach(app, "Created")

        // Delete it again.
        app.buttons["session-menu"].tap()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Delete…")).firstMatch.tap()
        let confirm = app.alerts["Delete session?"].buttons["Delete session"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 5), "Expected a centered alert to confirm the delete")
        attach(app, "Delete")
        confirm.tap()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 20), "Expected to return to the list")
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch
        XCTAssertTrue(waitFor(row, exists: false, timeout: 20), "Expected the session to leave the list")
    }

    // MARK: Steps

    /// Replaces the focused folder field's text with `folder`, then Done.
    @MainActor
    private func type(_ app: XCUIApplication, folder: String) {
        let field = app.textFields["new-session-folder"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        let clear = app.buttons["Clear"]
        if clear.waitForExistence(timeout: 2) { clear.tap() }
        field.typeText(folder)
        field.typeText("\n")
    }

    private func header(_ app: XCUIApplication, _ title: String) -> XCUIElement {
        app.navigationBars.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
    }

    private func waitFor(_ element: XCUIElement, enabled: Bool, timeout: TimeInterval) -> Bool {
        let predicate = NSPredicate(format: "isEnabled == %@", NSNumber(value: enabled))
        return XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: predicate, object: element)], timeout: timeout) == .completed
    }

    private func waitFor(_ element: XCUIElement, exists: Bool, timeout: TimeInterval) -> Bool {
        let predicate = NSPredicate(format: "exists == %@", NSNumber(value: exists))
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
