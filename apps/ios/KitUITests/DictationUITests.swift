import XCTest

/// Dictation in the composer and the comment box, with the app's scripted
/// microphone, on the paired server of an isolated Kit home: a tap sends
/// once ↑ is tapped, a hold sends when let go, sliding left cancels, refused
/// access offers Settings, silence says so, and a comment fills with the words. Run by script/test_dictation.sh, which
/// names a session whose folder is a git repository with uncommitted changes.
final class DictationUITests: XCTestCase {
    static let heard = "Run the reconnect test 20 times in a row"

    override func setUp() {
        continueAfterFailure = false
    }

    private func session() throws -> String {
        guard let title = ProcessInfo.processInfo.environment["KIT_DICTATION_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_dictation.sh against an isolated server with a session in a git repository.")
        }
        return title
    }

    @MainActor
    private func open(_ title: String, transcript: String = heard, access: String? = nil) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment["KIT_DICTATION_TRANSCRIPT"] = transcript
        app.launchEnvironment["KIT_DICTATION_DELAY"] = "1.5"
        if let access { app.launchEnvironment["KIT_DICTATION_ACCESS"] = access }
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()
        return app
    }

    private func composer(_ app: XCUIApplication) -> XCUIElement {
        app.textFields["composer"].exists ? app.textFields["composer"] : app.textViews["composer"]
    }

    @MainActor
    func testATapSendsWhenSendIsTapped() throws {
        let heard = "Run the reconnect test \(Int(Date().timeIntervalSince1970) % 100_000) times"
        let app = open(try session(), transcript: heard)
        let mic = app.buttons["dictate"]
        XCTAssertTrue(mic.waitForExistence(timeout: 15))
        XCTAssertEqual(mic.label, "Dictate")
        XCTAssertFalse(app.buttons["composer-action"].exists, "The mic takes Send's slot in an empty composer")
        attach(app, "Idle")

        mic.tap()
        let strip = app.descendants(matching: .any)["dictation-strip"]
        XCTAssertTrue(strip.waitForExistence(timeout: 5))
        XCTAssertTrue(waitFor(mic, label: "Send dictation", timeout: 5))
        XCTAssertTrue(strip.staticTexts["Tap ↑ to send"].exists)
        XCTAssertFalse(composer(app).exists, "The strip takes the field's place")
        sleep(2)
        attach(app, "Tapped")

        mic.tap()
        XCTAssertTrue(app.staticTexts["dictation-transcribing"].waitForExistence(timeout: 5))
        attach(app, "Transcribing")

        let sent = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", heard)).firstMatch
        XCTAssertTrue(sent.waitForExistence(timeout: 15), "Expected the dictated message in the transcript")
        XCTAssertTrue(composer(app).waitForExistence(timeout: 10))
        XCTAssertTrue(isEmpty(composer(app)), "The draft is clear once it's sent")
        attach(app, "Sent")
    }

    @MainActor
    func testSlidingLeftCancelsAHold() throws {
        let app = open(try session())
        let mic = app.buttons["dictate"]
        XCTAssertTrue(mic.waitForExistence(timeout: 15))
        let start = mic.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        start.press(forDuration: 2, thenDragTo: start.withOffset(CGVector(dx: -150, dy: 0)),
                    withVelocity: .slow, thenHoldForDuration: 2)
        XCTAssertTrue(composer(app).waitForExistence(timeout: 5))
        XCTAssertEqual(mic.label, "Dictate")
        XCTAssertTrue(isEmpty(composer(app)), "Nothing goes in the draft")
        XCTAssertFalse(app.staticTexts["dictation-transcribing"].exists)
    }

    @MainActor
    func testAHoldSendsWhenLetGo() throws {
        let heard = "Check the reconnect path \(Int(Date().timeIntervalSince1970) % 100_000)"
        let app = open(try session(), transcript: heard)
        let mic = app.buttons["dictate"]
        XCTAssertTrue(mic.waitForExistence(timeout: 15))
        mic.press(forDuration: 3)
        XCTAssertTrue(app.staticTexts["dictation-transcribing"].waitForExistence(timeout: 5))
        let sent = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", heard)).firstMatch
        XCTAssertTrue(sent.waitForExistence(timeout: 15), "Expected the dictated message in the transcript")
        attach(app, "Sent")
    }

    @MainActor
    func testRefusedAccessOffersSettings() throws {
        let app = open(try session(), access: "denied")
        let mic = app.buttons["dictate"]
        XCTAssertTrue(mic.waitForExistence(timeout: 15))
        mic.tap()
        let problem = app.descendants(matching: .any)["dictation-problem"]
        XCTAssertTrue(problem.waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Kit can’t use the microphone or speech recognition."].exists)
        XCTAssertTrue(app.buttons["Settings"].exists)
        XCTAssertTrue(mic.exists, "The mic stays, so it can be tried again")
        attach(app, "Not allowed")
    }

    @MainActor
    func testSilenceSaysNothingWasHeard() throws {
        let app = open(try session(), transcript: " ")
        let mic = app.buttons["dictate"]
        XCTAssertTrue(mic.waitForExistence(timeout: 15))
        mic.tap()
        XCTAssertTrue(waitFor(mic, label: "Send dictation", timeout: 5))
        mic.tap()
        XCTAssertTrue(app.staticTexts["Didn’t catch that."].waitForExistence(timeout: 10))
        XCTAssertTrue(isEmpty(composer(app)))
        attach(app, "Nothing heard")
        app.buttons["Dismiss"].tap()
        XCTAssertTrue(waitForAbsence(app.staticTexts["Didn’t catch that."], timeout: 5))
    }

    @MainActor
    func testACommentCanBeDictated() throws {
        let app = open(try session(), transcript: "Does this need a test?")
        let menu = app.buttons["session-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 15))
        menu.tap()
        let changes = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Changes")).firstMatch
        XCTAssertTrue(changes.waitForExistence(timeout: 5))
        changes.tap()
        let lines = app.descendants(matching: .any)["diff-lines"].firstMatch
        XCTAssertTrue(lines.waitForExistence(timeout: 15))
        lines.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 14, dy: 50)).tap()
        let field = app.descendants(matching: .any)["diff-comment-field"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        let mic = app.buttons["dictate-field"]
        XCTAssertTrue(mic.exists)
        attach(app, "Comment")

        mic.tap()
        let done = app.buttons["dictation-done"]
        XCTAssertTrue(done.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["diff-comment-save"].isEnabled, "Comment waits for the words")
        sleep(2)
        attach(app, "Comment listening")
        done.tap()
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        XCTAssertEqual(field.value as? String, "Does this need a test?")
        XCTAssertTrue(app.buttons["diff-comment-save"].isEnabled)
        attach(app, "Comment filled")
        app.buttons["diff-comment-cancel"].tap()
    }

    /// An empty field reads as its placeholder.
    private func isEmpty(_ field: XCUIElement) -> Bool {
        let value = field.value as? String ?? ""
        return value.isEmpty || value == "Ask Kit…"
    }

    private func waitFor(_ element: XCUIElement, label: String, timeout: TimeInterval) -> Bool {
        let predicate = NSPredicate(format: "label == %@", label)
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
