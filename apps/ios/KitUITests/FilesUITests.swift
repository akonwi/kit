import XCTest

/// Opens files from the session menu's Open File in a session on the paired
/// server of an isolated Kit home: finds a code file by a fuzzy search and
/// reads it, wraps its lines, reads a Markdown file rendered and as source,
/// and sees why a binary file can't be shown. Run by script/test_files.sh,
/// whose session's folder is made by fixtures/make_files_demo.py.
final class FilesUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testOpenFiles() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_FILES_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_files.sh against an isolated server.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // A fuzzy search finds the file by part of its name.
        openFile(app, search: "httpcl")
        let results = app.buttons.matching(identifier: "open-file-row")
        XCTAssertEqual(results.firstMatch.label, "Sources/Client/HTTPClient.swift")
        attach(app, "Open File")
        results.firstMatch.tap()

        // The file opens at its first line, numbered, under its name.
        XCTAssertTrue(app.staticTexts["HTTPClient.swift"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Sources/Client"].exists)
        let first = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Line 1")).firstMatch
        XCTAssertTrue(first.waitForExistence(timeout: 10))
        XCTAssertEqual(first.value as? String, "import Foundation")
        attach(app, "Code")

        // Lines wrap from the menu.
        app.buttons["file-menu"].tap()
        let wrap = app.switches["Wrap lines"].exists ? app.switches["Wrap lines"] : app.buttons["Wrap lines"]
        XCTAssertTrue(wrap.waitForExistence(timeout: 5))
        wrap.tap()
        XCTAssertTrue(first.waitForExistence(timeout: 5))
        attach(app, "Wrapped")
        app.buttons["file-menu"].tap()
        (app.switches["Wrap lines"].exists ? app.switches["Wrap lines"] : app.buttons["Wrap lines"]).tap()
        app.navigationBars.buttons.firstMatch.tap()

        // A Markdown file shows rendered, and its source is one tap away.
        openFile(app, search: "pairing")
        results.firstMatch.tap()
        let heading = app.staticTexts["Pairing a device"]
        XCTAssertTrue(heading.waitForExistence(timeout: 10))
        attach(app, "Preview")
        app.buttons["Source"].tap()
        let source = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Line 1")).firstMatch
        XCTAssertTrue(source.waitForExistence(timeout: 5))
        XCTAssertEqual(source.value as? String, "# Pairing a device")
        attach(app, "Source")
        app.navigationBars.buttons.firstMatch.tap()

        // A binary file says why it can't be shown.
        openFile(app, search: "logo.png")
        results.firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Binary files cannot be previewed."].waitForExistence(timeout: 10))
        attach(app, "Binary")
    }

    @MainActor
    private func openFile(_ app: XCUIApplication, search: String) {
        let menu = app.buttons["session-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 15))
        menu.tap()
        let open = app.buttons["open-file"]
        XCTAssertTrue(open.waitForExistence(timeout: 5))
        open.tap()
        let field = app.textFields["open-file-search"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons.matching(identifier: "open-file-row").firstMatch.waitForExistence(timeout: 10))
        field.typeText(search)
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
