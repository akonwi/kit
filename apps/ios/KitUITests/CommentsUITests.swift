import XCTest

/// Comments on a session's changes, on the paired server of an isolated Kit
/// home: a tap on a line number opens the comment box, the handle stretches
/// the selection, Comment saves it, its card edits and deletes it, its chip
/// above the composer shows it in Changes, and it goes with a message. Run by script/test_comments.sh,
/// which names a session whose folder is a git repository with uncommitted
/// changes.
final class CommentsUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testComments() throws {
        let environment = ProcessInfo.processInfo.environment
        guard let title = environment["KIT_COMMENTS_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_comments.sh against an isolated server with a session in a git repository.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()
        let menu = app.buttons["session-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 15))
        menu.tap()
        let changes = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Changes")).firstMatch
        XCTAssertTrue(changes.waitForExistence(timeout: 5))
        changes.tap()

        // A tap on the first file's third line number opens the box under it.
        let lines = app.descendants(matching: .any)["diff-lines"].firstMatch
        XCTAssertTrue(lines.waitForExistence(timeout: 15))
        lines.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 14, dy: 50)).tap()
        let field = app.descendants(matching: .any)["diff-comment-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Comment on ")).firstMatch.exists)
        XCTAssertFalse(app.buttons["diff-comment-save"].isEnabled)
        attach(app, "One line")

        // The handle stretches it two lines further.
        let before = commentTitle(app)
        let handle = app.descendants(matching: .any)["diff-selection-handle"]
        XCTAssertTrue(handle.exists)
        handle.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
            .press(forDuration: 0.2, thenDragTo: handle.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
                .withOffset(CGVector(dx: 0, dy: 40)))
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        XCTAssertTrue(commentTitle(app).hasPrefix("Comment on lines") || commentTitle(app).hasPrefix("Comment on removed lines"),
                      "Expected a range, got \(commentTitle(app)) after \(before)")
        attach(app, "Three lines")

        // Write it and save it; the box closes and the composer returns.
        field.tap()
        field.typeText("Does this need a test?")
        XCTAssertTrue(app.buttons["diff-comment-save"].isEnabled)
        attach(app, "Written")
        app.buttons["diff-comment-save"].tap()
        XCTAssertTrue(waitForAbsence(field, timeout: 10))

        // It shows under its lines, waiting for the next message.
        let card = card(app, containing: "Does this need a test?")
        XCTAssertTrue(card.waitForExistence(timeout: 10))
        XCTAssertTrue(card.label.contains("goes with your next message"), card.label)
        attach(app, "Saved")

        // Edit reopens the box with the comment in it.
        app.buttons["diff-comment-menu"].firstMatch.tap()
        let edit = app.buttons["Edit"]
        XCTAssertTrue(edit.waitForExistence(timeout: 5))
        edit.tap()
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        XCTAssertEqual(field.value as? String, "Does this need a test?")
        field.tap()
        field.typeText(" And docs?")
        app.buttons["diff-comment-save"].tap()
        XCTAssertTrue(waitForAbsence(field, timeout: 10))
        XCTAssertTrue(self.card(app, containing: "Does this need a test? And docs?").waitForExistence(timeout: 10))
        attach(app, "Edited")

        // Its chip sits above the composer. Back in the session, the chip
        // opens Changes on the comment.
        let chip = app.buttons.matching(identifier: "comment-chip").firstMatch
        XCTAssertTrue(chip.waitForExistence(timeout: 5))
        XCTAssertTrue(chip.label.hasPrefix("Comment on README.md:") || chip.label.hasPrefix("Comment on "), chip.label)
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(chip.waitForExistence(timeout: 5))
        attach(app, "Chip in session")
        chip.tap()
        XCTAssertTrue(self.card(app, containing: "And docs?").waitForExistence(timeout: 10))
        XCTAssertTrue(self.card(app, containing: "And docs?").isHittable)
        attach(app, "Revealed")

        // Delete removes the card and its chip.
        app.buttons["diff-comment-menu"].firstMatch.tap()
        let delete = app.buttons["Delete"]
        XCTAssertTrue(delete.waitForExistence(timeout: 5))
        delete.tap()
        XCTAssertTrue(waitForAbsence(app.descendants(matching: .any)["diff-comment-card"].firstMatch, timeout: 10))
        XCTAssertTrue(waitForAbsence(chip, timeout: 5))
        attach(app, "Deleted")

        // A comment goes with the next message, and its chip clears.
        lines.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 14, dy: 10)).tap()
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.typeText("Reply in one word.")
        app.buttons["diff-comment-save"].tap()
        XCTAssertTrue(chip.waitForExistence(timeout: 10))
        app.navigationBars.buttons.element(boundBy: 0).tap()
        let composer = app.descendants(matching: .any)["composer"].firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 5))
        composer.tap()
        composer.typeText("What do you make of this comment?")
        app.buttons["Send"].tap()
        XCTAssertTrue(waitForAbsence(chip, timeout: 15))
        attach(app, "Sent")
        let stop = app.buttons["Stop"]
        if stop.waitForExistence(timeout: 5) { stop.tap() }
    }

    /// Run by script/test_changed_comment.sh, which comments on a line and
    /// then changes it.
    @MainActor
    func testChangedComment() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_COMMENTS_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_changed_comment.sh against an isolated server.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch.tap()

        // Its chip warns that the code changed.
        let chip = app.buttons.matching(NSPredicate(format: "identifier == %@ AND label CONTAINS %@",
                                                     "comment-chip", "Diff changed")).firstMatch
        XCTAssertTrue(chip.waitForExistence(timeout: 15))
        attach(app, "Changed chip")

        // The chip opens its card, which offers a new range.
        chip.tap()
        let changed = card(app, containing: "Diff changed")
        XCTAssertTrue(changed.waitForExistence(timeout: 10))
        attach(app, "Changed card")
        app.buttons["diff-comment-select-range"].tap()
        XCTAssertTrue(app.buttons["diff-comment-stop-placing"].waitForExistence(timeout: 5))
        attach(app, "Placing")

        // A tap on the first line above the card opens the box with its text.
        let above = app.descendants(matching: .any).matching(identifier: "diff-lines").allElementsBoundByIndex
            .filter { $0.frame.maxY <= changed.frame.minY + 1 }
            .max { $0.frame.maxY < $1.frame.maxY }
        let lines = try XCTUnwrap(above)
        lines.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 14, dy: 10)).tap()
        let field = app.descendants(matching: .any)["diff-comment-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        XCTAssertEqual(field.value as? String, "Is this line still right?")
        app.buttons["diff-comment-save"].tap()
        XCTAssertTrue(waitForAbsence(field, timeout: 10))

        // It's an ordinary comment again, and so is its chip.
        let moved = card(app, containing: "goes with your next message")
        XCTAssertTrue(moved.waitForExistence(timeout: 10))
        XCTAssertTrue(waitForAbsence(changed, timeout: 5))
        XCTAssertTrue(waitForAbsence(chip, timeout: 5))
        attach(app, "Moved")

        app.buttons["diff-comment-menu"].firstMatch.tap()
        app.buttons["Delete"].tap()
        XCTAssertTrue(waitForAbsence(moved, timeout: 10))
    }

    private func card(_ app: XCUIApplication, containing text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "identifier == %@ AND label CONTAINS %@",
                                                             "diff-comment-card", text)).firstMatch
    }

    private func commentTitle(_ app: XCUIApplication) -> String {
        app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Comment on ")).firstMatch.label
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
