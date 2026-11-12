import XCTest

/// Uses the composer's command button in a session on the paired server of an
/// isolated Kit home whose project has a `review` prompt command. It searches
/// commands, picks the prompt into a chip and removes it, opens the model
/// choices, and reloads the session's context, which then leads the list as a
/// recent command. It also moves the session to a sibling folder, other/, and
/// back. Run by script/test_commands.sh, which names the session. Nothing
/// here starts a turn.
final class CommandsUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testCommandButton() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_COMMANDS_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_commands.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // The button turns the composer into a search over every command.
        let button = app.buttons["command-button"]
        XCTAssertTrue(button.waitForExistence(timeout: 15))
        attach(app, "Composer")
        button.tap()
        XCTAssertTrue(app.buttons["command-builtin:changes"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["command-prompt:review"].exists)
        XCTAssertTrue(app.buttons["command-builtin:reload"].exists)
        expectComposerAboveKeyboard(app)
        attach(app, "Search")

        // Typing narrows the list.
        app.typeText("rev")
        XCTAssertTrue(waitForAbsence(app.buttons["command-builtin:changes"], timeout: 5))
        XCTAssertTrue(app.buttons["command-prompt:review"].exists)
        attach(app, "Filtered")

        // A prompt command becomes a chip, with what it does above the composer.
        app.buttons["command-prompt:review"].tap()
        let chip = app.buttons["command-chip"]
        XCTAssertTrue(chip.waitForExistence(timeout: 5))
        XCTAssertTrue(app.otherElements["command-hint"].exists || app.staticTexts["Review the working tree"].exists)
        app.typeText("focus on SSE")
        attach(app, "Picked")
        chip.tap()
        XCTAssertTrue(waitForAbsence(chip, timeout: 5))
        XCTAssertEqual(app.descendants(matching: .any)["composer"].firstMatch.value as? String, "focus on SSE")
        attach(app, "Chip removed")
        clear(app.descendants(matching: .any)["composer"].firstMatch)

        // Model opens its choices in place, and Commands goes back.
        button.tap()
        let model = app.buttons["command-builtin:model"]
        XCTAssertTrue(model.waitForExistence(timeout: 5))
        model.tap()
        XCTAssertTrue(app.buttons["command-back"].waitForExistence(timeout: 5))
        expectComposerAboveKeyboard(app)
        attach(app, "Models")
        app.buttons["command-back"].tap()
        XCTAssertTrue(app.buttons["command-builtin:reload"].waitForExistence(timeout: 5))

        // Reload context runs at once and says how it went.
        app.buttons["command-builtin:reload"].tap()
        XCTAssertTrue(app.staticTexts["Context reloaded."].waitForExistence(timeout: 15))
        attach(app, "Reloaded")

        // It now leads the list.
        button.tap()
        XCTAssertTrue(app.staticTexts["Recent"].waitForExistence(timeout: 5))
        attach(app, "Recent")
        app.buttons["command-close"].tap()
        XCTAssertTrue(waitForAbsence(app.buttons["command-builtin:reload"], timeout: 5))

        // Change folder moves the session as cd would, then back.
        changeFolder(app, to: "../other")
        XCTAssertTrue(header(app, containing: "/other").waitForExistence(timeout: 15), "Expected the header to show other/")
        attach(app, "Moved")
        changeFolder(app, to: "../project")
        XCTAssertTrue(header(app, containing: "/project").waitForExistence(timeout: 15))

        // A tap outside the list closes it, and the keyboard with it.
        button.tap()
        XCTAssertTrue(app.buttons["command-builtin:reload"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5))
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.25)).tap()
        XCTAssertTrue(waitForAbsence(app.buttons["command-builtin:reload"], timeout: 5))
        XCTAssertTrue(waitForAbsence(app.keyboards.firstMatch, timeout: 5))
        attach(app, "Closed by a tap outside")
    }

    /// The field and its close button stay in view, above the keyboard.
    private func expectComposerAboveKeyboard(_ app: XCUIApplication) {
        let keyboard = app.keyboards.firstMatch
        XCTAssertTrue(keyboard.waitForExistence(timeout: 5))
        let close = app.buttons["command-close"]
        XCTAssertTrue(close.isHittable)
        XCTAssertLessThanOrEqual(close.frame.maxY, keyboard.frame.minY + 1, "The composer is behind the keyboard")
    }

    @MainActor
    private func changeFolder(_ app: XCUIApplication, to path: String) {
        app.buttons["command-button"].tap()
        let row = app.buttons["command-builtin:changeFolder"]
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
        let field = app.textFields["change-folder-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.typeText(path)
        attach(app, "Change folder to \(path)")
        app.buttons["change-folder-submit"].tap()
        XCTAssertTrue(waitForAbsence(field, timeout: 15), "Expected the sheet to close")
    }

    private func header(_ app: XCUIApplication, containing text: String) -> XCUIElement {
        app.navigationBars.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    private func clear(_ field: XCUIElement) {
        guard let text = field.value as? String, !text.isEmpty else { return }
        field.tap()
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: text.count))
    }

    private func waitForAbsence(_ element: XCUIElement, timeout: TimeInterval) -> Bool {
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: element)
        return XCTWaiter.wait(for: [gone], timeout: timeout) == .completed
    }

    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
