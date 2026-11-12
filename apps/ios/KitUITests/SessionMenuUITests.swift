import XCTest

/// Opens the session menu in a session on the paired server of an isolated
/// Kit home and shows Details. From the composer's command search, it renames
/// the session and back, changes its model and thinking level and back, and
/// forks it; the menu deletes the fork. It also renames the session from its
/// row in the list, and deletes a fork there. Run by
/// script/test_session_menu.sh, which names the session. Nothing here starts
/// a turn.
final class SessionMenuUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testSessionMenu() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_MENU_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_session_menu.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // The menu lists everything the session can do.
        openMenu(app)
        for item in ["Changes", "Open File…", "Scratchpad", "Details", "Delete…"] {
            XCTAssertTrue(menuItem(app, item).waitForExistence(timeout: 5), "Expected \(item) in the menu")
        }
        attach(app, "Menu")

        // Details is a read-only sheet.
        menuItem(app, "Details").tap()
        XCTAssertTrue(app.staticTexts["CONTEXT"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["USAGE"].exists)
        attach(app, "Details")
        app.buttons["Close"].tap()
        XCTAssertTrue(waitForAbsence(app.staticTexts["USAGE"], timeout: 5))

        // Rename, then rename back.
        rename(app, from: title, to: "\(title) renamed")
        attach(app, "Renamed")
        rename(app, from: "\(title) renamed", to: title)

        // Change the model, and its thinking level, then change both back.
        // KIT_MENU_MODEL names a model that offers several thinking levels.
        let originalModel = commandValue(app, "model")
        command(app, "model")
        XCTAssertTrue(app.buttons["command-back"].waitForExistence(timeout: 5))
        attach(app, "Models")
        let target = ProcessInfo.processInfo.environment["KIT_MENU_MODEL"] ?? ""
        if target.isEmpty || target == originalModel {
            app.buttons["command-close"].tap()
        } else {
            pick(app, target)
            expectModel(app, target)
            changeThinkingAndBack(app)
            command(app, "model")
            pick(app, originalModel)
            expectModel(app, originalModel)
        }

        // Fork into a new session, which opens; then delete the fork.
        let forkName = "Menu test fork \(Int(Date().timeIntervalSince1970) % 100_000)"
        command(app, "fork")
        let name = app.textFields["fork-name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.typeText(forkName)
        attach(app, "Fork")
        app.buttons["Fork"].tap()
        XCTAssertTrue(header(app, forkName).waitForExistence(timeout: 20), "Expected the fork to open")
        attach(app, "Forked")

        openMenu(app)
        menuItem(app, "Delete…").tap()
        let confirm = app.buttons["Delete session"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 5))
        attach(app, "Delete")
        confirm.tap()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 20), "Expected to return to the list")
        let fork = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", forkName)).firstMatch
        XCTAssertTrue(waitForAbsence(fork, timeout: 20), "Expected the fork to leave the list")
    }

    /// A long press on a row in the list offers Rename and Delete.
    @MainActor
    func testRowMenu() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_MENU_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_session_menu.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))

        // Rename from the row, then rename back.
        openRowMenu(app, title)
        XCTAssertTrue(app.buttons["Delete…"].exists)
        attach(app, "Row menu")
        app.buttons["Rename…"].tap()
        renameInSheet(app, to: "\(title) renamed")
        XCTAssertTrue(row(app, "\(title) renamed").waitForExistence(timeout: 10), "Expected the row to read the new name")
        attach(app, "Renamed from the row")
        openRowMenu(app, "\(title) renamed")
        app.buttons["Rename…"].tap()
        renameInSheet(app, to: title)
        XCTAssertTrue(row(app, title).waitForExistence(timeout: 10))

        // Fork the session to have one to delete, go back, and delete the fork from its row.
        let forkName = "Row test fork \(Int(Date().timeIntervalSince1970) % 100_000)"
        row(app, title).tap()
        command(app, "fork")
        let name = app.textFields["fork-name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.typeText(forkName)
        app.buttons["Fork"].tap()
        XCTAssertTrue(header(app, forkName).waitForExistence(timeout: 20), "Expected the fork to open")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 10))

        openRowMenu(app, forkName)
        app.buttons["Delete…"].tap()
        let confirm = app.buttons["Delete session"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Delete session?"].exists)
        attach(app, "Delete from the row")
        confirm.tap()
        XCTAssertTrue(waitForAbsence(row(app, forkName), timeout: 20), "Expected the fork to leave the list")
        XCTAssertTrue(row(app, title).exists)
        attach(app, "Deleted from the row")
    }

    // MARK: Steps

    private func row(_ app: XCUIApplication, _ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title + ",")).firstMatch
    }

    @MainActor
    private func openRowMenu(_ app: XCUIApplication, _ title: String) {
        let row = row(app, title)
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.press(forDuration: 1.2)
        XCTAssertTrue(app.buttons["Rename…"].waitForExistence(timeout: 5), "Expected the row menu")
    }

    /// Replaces the name in the open rename sheet and saves it.
    @MainActor
    private func renameInSheet(_ app: XCUIApplication, to new: String) {
        let field = app.textFields["rename-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        let old = field.value as? String ?? ""
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: old.count + 2))
        field.typeText(new)
        XCTAssertEqual(field.value as? String, new)
        app.buttons["Save"].tap()
        XCTAssertTrue(waitForAbsence(field, timeout: 10), "Expected the sheet to close")
    }

    /// Opens the composer's command search, types the command's name, and taps it.
    @MainActor
    private func command(_ app: XCUIApplication, _ name: String) {
        let button = app.buttons["command-button"]
        XCTAssertTrue(button.waitForExistence(timeout: 15))
        button.tap()
        XCTAssertTrue(app.buttons["command-close"].waitForExistence(timeout: 5))
        app.typeText(name)
        let row = app.buttons["command-builtin:" + name]
        XCTAssertTrue(row.waitForExistence(timeout: 5), "Expected \(name) in the command search")
        row.tap()
    }

    /// What a built-in command's row shows as its value, such as the model.
    @MainActor
    private func commandValue(_ app: XCUIApplication, _ name: String) -> String {
        let button = app.buttons["command-button"]
        XCTAssertTrue(button.waitForExistence(timeout: 15))
        button.tap()
        XCTAssertTrue(app.buttons["command-close"].waitForExistence(timeout: 5))
        app.typeText(name)
        let row = app.buttons["command-builtin:" + name]
        XCTAssertTrue(row.waitForExistence(timeout: 5), "Expected \(name) in the command search")
        let value = row.value as? String ?? ""
        app.buttons["command-close"].tap()
        XCTAssertTrue(waitForAbsence(row, timeout: 5))
        return value
    }

    /// Finds a model on the command search's Model page by typing its name, and taps it.
    @MainActor
    private func pick(_ app: XCUIApplication, _ model: String) {
        app.typeText(model)
        let item = app.buttons[model].firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5) && item.isHittable, "Expected \(model) on the Model page")
        item.tap()
    }

    /// Expects the Model command to read `model`, looking again until the
    /// server's change reaches it.
    @MainActor
    private func expectModel(_ app: XCUIApplication, _ model: String) {
        var value = ""
        for _ in 0..<10 {
            value = commandValue(app, "model")
            if value == model { return }
            sleep(1)
        }
        XCTFail("Expected \(model), the command search reads \(value)")
    }

    @MainActor
    private func changeThinkingAndBack(_ app: XCUIApplication) {
        let previous = commandValue(app, "thinking")
        XCTAssertFalse(previous.isEmpty, "Expected Thinking for a model with levels")
        command(app, "thinking")
        let levels = app.buttons.matching(NSPredicate(format: "label IN %@", ["Off", "Minimal", "Low", "Medium", "High", "Extra high", "Max"]))
        XCTAssertTrue(levels.firstMatch.waitForExistence(timeout: 5))
        attach(app, "Thinking")
        guard let other = levels.allElementsBoundByIndex.first(where: { $0.label != previous }) else {
            return XCTFail("Expected another level than \(previous)")
        }
        let chosen = other.label
        other.tap()
        XCTAssertTrue(expectValue(app, "thinking", chosen), "Expected \(chosen)")
        command(app, "thinking")
        app.buttons[previous].firstMatch.tap()
        XCTAssertTrue(expectValue(app, "thinking", previous), "Expected \(previous)")
    }

    @MainActor
    private func expectValue(_ app: XCUIApplication, _ name: String, _ value: String) -> Bool {
        for _ in 0..<10 {
            if commandValue(app, name) == value { return true }
            sleep(1)
        }
        return false
    }

    @MainActor
    private func rename(_ app: XCUIApplication, from old: String, to new: String) {
        command(app, "rename")
        renameInSheet(app, to: new)
        XCTAssertTrue(header(app, new).waitForExistence(timeout: 10), "Expected the header to read \(new)")
    }

    @MainActor
    private func openMenu(_ app: XCUIApplication) {
        let menu = app.buttons["session-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 15))
        menu.tap()
    }

    private func menuItem(_ app: XCUIApplication, _ label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label == %@ OR label BEGINSWITH %@", label, label + ",")).firstMatch
    }

    private func header(_ app: XCUIApplication, _ title: String) -> XCUIElement {
        app.navigationBars.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
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
