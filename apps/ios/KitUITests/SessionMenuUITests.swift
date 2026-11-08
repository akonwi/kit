import XCTest

/// Opens the session menu in a session on the paired server of an isolated
/// Kit home, then shows Details, renames the session and back, changes its
/// model and thinking level and back, and forks it and deletes the fork. It
/// also renames the session from its row in the list, and deletes a fork
/// there. Run by script/test_session_menu.sh, which names the session.
/// Nothing here starts a turn.
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
        for item in ["Model", "Compact context", "Rename…", "Fork…", "Details", "Delete…"] {
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
        openMenu(app)
        let originalModel = menuItem(app, "Model").label.replacingOccurrences(of: "Model, ", with: "")
        menuItem(app, "Model").tap()
        attach(app, "Models")
        let target = ProcessInfo.processInfo.environment["KIT_MENU_MODEL"] ?? ""
        if target.isEmpty || target == originalModel {
            dismissMenu(app)
        } else {
            pick(app, target)
            expectModel(app, target)
            changeThinkingAndBack(app)
            openMenu(app)
            menuItem(app, "Model").tap()
            pick(app, originalModel)
            expectModel(app, originalModel)
        }

        // Fork into a new session, which opens; then delete the fork.
        let forkName = "Menu test fork \(Int(Date().timeIntervalSince1970) % 100_000)"
        openMenu(app)
        menuItem(app, "Fork…").tap()
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
        openMenu(app)
        menuItem(app, "Fork…").tap()
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
        field.press(forDuration: 1)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        field.typeText(XCUIKeyboardKey.delete.rawValue)
        field.typeText(new)
        app.buttons["Save"].tap()
        XCTAssertTrue(waitForAbsence(field, timeout: 10), "Expected the sheet to close")
    }

    /// Taps a model in the open Model submenu, scrolling down to it.
    @MainActor
    private func pick(_ app: XCUIApplication, _ model: String) {
        let item = app.buttons[model].firstMatch
        for _ in 0..<15 where !(item.exists && item.isHittable) {
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.7, dy: 0.6))
            start.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.7, dy: 0.35)))
        }
        XCTAssertTrue(item.isHittable, "Expected \(model) in the Model menu")
        item.tap()
    }

    /// Expects the menu's Model item to read `model`, reopening the menu
    /// until the server's change reaches it.
    @MainActor
    private func expectModel(_ app: XCUIApplication, _ model: String) {
        var label = ""
        for _ in 0..<10 {
            openMenu(app)
            label = menuItem(app, "Model").label
            dismissMenu(app)
            if label.contains(model) { return }
            sleep(1)
        }
        XCTFail("Expected \(model), the menu reads \(label)")
    }

    @MainActor
    private func changeThinkingAndBack(_ app: XCUIApplication) {
        openMenu(app)
        let thinking = menuItem(app, "Thinking")
        XCTAssertTrue(thinking.waitForExistence(timeout: 5), "Expected Thinking for a model with levels")
        let original = thinking.label
        thinking.tap()
        let levels = app.buttons.matching(NSPredicate(format: "label IN %@", ["Off", "Minimal", "Low", "Medium", "High", "Extra high", "Max"]))
        XCTAssertTrue(levels.firstMatch.waitForExistence(timeout: 5))
        attach(app, "Thinking")
        let all = levels.allElementsBoundByIndex
        guard let other = all.first(where: { !original.hasSuffix($0.label) }),
              let previous = all.first(where: { original.hasSuffix($0.label) })?.label else {
            return XCTFail("Expected another level than \(original)")
        }
        let chosen = other.label
        other.tap()
        openMenu(app)
        XCTAssertTrue(waitFor(menuItem(app, "Thinking"), labelContaining: chosen, timeout: 10), "Expected \(chosen)")
        menuItem(app, "Thinking").tap()
        app.buttons[previous].firstMatch.tap()
        openMenu(app)
        XCTAssertTrue(waitFor(menuItem(app, "Thinking"), labelContaining: previous, timeout: 10))
        dismissMenu(app)
    }

    @MainActor
    private func rename(_ app: XCUIApplication, from old: String, to new: String) {
        openMenu(app)
        menuItem(app, "Rename…").tap()
        renameInSheet(app, to: new)
        XCTAssertTrue(header(app, new).waitForExistence(timeout: 10), "Expected the header to read \(new)")
    }

    @MainActor
    private func openMenu(_ app: XCUIApplication) {
        let menu = app.buttons["session-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 15))
        menu.tap()
    }

    @MainActor
    private func dismissMenu(_ app: XCUIApplication) {
        if menuItem(app, "Details").exists {
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.1, dy: 0.5)).tap()
        }
    }

    private func menuItem(_ app: XCUIApplication, _ label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label == %@ OR label BEGINSWITH %@", label, label + ",")).firstMatch
    }

    private func header(_ app: XCUIApplication, _ title: String) -> XCUIElement {
        app.navigationBars.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
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
