import XCTest

/// Runs shell commands from the composer in a session on the paired server
/// of an isolated Kit home: Shell Command in the menu, and ! typed at the
/// start, each run with Send, with Stop while one runs. Run by script/test_shell.sh, which names the
/// session. The commands only echo; no turn starts.
final class ShellUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testShellCommandsRunFromTheComposer() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_SHELL_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_shell.sh against an isolated server with a session.")
        }
        let app = XCUIApplication.withoutDrafts()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()
        let composer = app.descendants(matching: .any)["composer"].firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15))

        // Shell Command in the menu puts the chip in the field.
        app.openComposerMenu()
        let item = app.buttons["Shell Command"]
        XCTAssertTrue(item.waitForExistence(timeout: 5))
        item.tap()
        XCTAssertTrue(app.buttons["shell-chip"].waitForExistence(timeout: 5))
        let hint = app.descendants(matching: .any)["shell-hint"].firstMatch
        XCTAssertTrue(hint.exists)
        XCTAssertTrue(app.staticTexts["The agent sees the output"].exists)
        composer.typeText("echo seen-by-agent")
        attach(app, "Shell chip")
        app.buttons["composer-action"].tap()

        let seen = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "seen-by-agent")).firstMatch
        XCTAssertTrue(seen.waitForExistence(timeout: 15), "Expected the command's row")
        XCTAssertTrue(waitForAbsence(app.buttons["shell-chip"], timeout: 10), "The draft clears once it runs")
        XCTAssertTrue(app.descendants(matching: .any).matching(identifier: "bash-row")
            .matching(NSPredicate(format: "value == %@", "The agent sees the output")).firstMatch.waitForExistence(timeout: 15))

        // ! typed first makes the chip; Hide keeps the output from the agent.
        composer.tap()
        composer.typeText("!echo hidden-from-agent")
        XCTAssertTrue(app.buttons["shell-chip"].waitForExistence(timeout: 5))
        XCTAssertEqual(composer.value as? String, "echo hidden-from-agent")
        app.buttons["shell-output"].tap()
        XCTAssertTrue(app.staticTexts["The agent doesn’t see the output"].waitForExistence(timeout: 5))
        attach(app, "Hidden output")
        app.buttons["composer-action"].tap()
        XCTAssertTrue(app.descendants(matching: .any).matching(identifier: "bash-row")
            .matching(NSPredicate(format: "value == %@", "The agent doesn’t see the output")).firstMatch.waitForExistence(timeout: 15))

        // While a command runs, the button stops it.
        composer.tap()
        composer.typeText("!sleep 30; echo slept")
        app.buttons["composer-action"].tap()
        let stop = app.buttons.matching(NSPredicate(format: "identifier == %@ AND label == %@", "composer-action", "Stop")).firstMatch
        XCTAssertTrue(stop.waitForExistence(timeout: 10), "Expected Stop while the command runs")
        attach(app, "Running")
        stop.tap()
        let stopped = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "Aborted")).firstMatch
        XCTAssertTrue(stopped.waitForExistence(timeout: 15), "Expected the command to stop")

        // A failing command says how it ended.
        composer.tap()
        composer.typeText("!echo broke; exit 3")
        app.buttons["composer-action"].tap()
        let failed = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "Exit 3")).firstMatch
        XCTAssertTrue(failed.waitForExistence(timeout: 15), "Expected Exit 3 on the row")
        attach(app, "Rows")

        // Tapping the chip makes it a message again.
        composer.tap()
        composer.typeText("!ls")
        app.buttons["shell-chip"].tap()
        XCTAssertTrue(waitForAbsence(app.buttons["shell-chip"], timeout: 5))
        XCTAssertEqual(composer.value as? String, "ls")
    }

    @MainActor
    private func waitForAbsence(_ element: XCUIElement, timeout: TimeInterval) -> Bool {
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: element)
        return XCTWaiter().wait(for: [gone], timeout: timeout) == .completed
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
