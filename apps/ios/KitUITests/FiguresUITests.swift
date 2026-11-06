import XCTest

/// Opens a Mermaid diagram from a session's transcript on its own, pinches
/// it, and closes it, on the paired server of an isolated Kit home. Run by
/// script/test_figures.sh, which names a session holding the message
/// script/fixtures/post_figures.py posts.
final class FiguresUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testDiagramOpensFullScreen() throws {
        guard let title = ProcessInfo.processInfo.environment["KIT_FIGURES_SESSION"], !title.isEmpty else {
            throw XCTSkip("Run script/test_figures.sh against an isolated server with a session.")
        }
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // The first diagram, brought into view from the end of the transcript.
        let open = app.buttons.matching(identifier: "mermaid-open").element(boundBy: 0)
        XCTAssertTrue(app.buttons["composer-action"].waitForExistence(timeout: 15))
        attach(app, "End of the transcript")
        for _ in 0..<20 where !(open.exists && open.isHittable) {
            app.swipeDown(velocity: .slow)
        }
        XCTAssertTrue(open.exists, "No Mermaid diagram in \(title)")
        XCTAssertTrue(open.isHittable, "The diagram's open button stayed out of view")
        attach(app, "In the transcript")

        open.tap()
        let done = app.buttons["mermaid-done"]
        XCTAssertTrue(done.waitForExistence(timeout: 5), "Expected the diagram on its own")
        XCTAssertTrue(app.navigationBars["Diagram"].exists)
        let diagram = app.otherElements["mermaid-screen"].descendants(matching: .any)["mermaid-diagram"]
        XCTAssertTrue(diagram.waitForExistence(timeout: 5))
        attach(app, "Full screen")

        diagram.pinch(withScale: 2.5, velocity: 2)
        attach(app, "Pinched")

        done.tap()
        XCTAssertTrue(waitForAbsence(done, timeout: 5), "Expected Done to close the diagram")
        XCTAssertTrue(open.waitForExistence(timeout: 5))
        attach(app, "Closed")
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
