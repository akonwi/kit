import XCTest

/// Edits a session's shared scratchpad from the session menu on the paired
/// server of an isolated Kit home: it saves after typing stops and when the
/// screen closes, and when another client changes the shared copy under an
/// unsaved draft, the draft stays and Review changes lets either win. The
/// test plays the other client itself, through the server's API. Run by
/// script/test_scratchpad.sh.
final class ScratchpadUITests: XCTestCase {
    private var server: ScratchpadServer!

    override func setUpWithError() throws {
        continueAfterFailure = false
        let environment = ProcessInfo.processInfo.environment
        guard let home = environment["KIT_SCRATCHPAD_HOME"], let id = environment["KIT_SCRATCHPAD_SESSION_ID"],
              !home.isEmpty, !id.isEmpty else {
            throw XCTSkip("Run script/test_scratchpad.sh against an isolated server.")
        }
        server = try ScratchpadServer(home: home, session: id)
    }

    @MainActor
    func testEditTheScratchpad() throws {
        let title = try XCTUnwrap(ProcessInfo.processInfo.environment["KIT_SCRATCHPAD_SESSION"])
        let seed = "# Plan\n\n## Done\n- [x] Keep the cursor"
        try server.set(seed)

        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.navigationBars["Sessions"].waitForExistence(timeout: 15))
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15), "No session named \(title)")
        row.tap()

        // The session menu opens the pad, showing the shared copy.
        let menu = app.buttons["session-menu"]
        XCTAssertTrue(menu.waitForExistence(timeout: 15))
        menu.tap()
        let item = app.buttons["scratchpad"]
        XCTAssertTrue(item.waitForExistence(timeout: 5))
        item.tap()
        let editor = app.textViews["scratchpad-editor"]
        XCTAssertTrue(editor.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: editor, value: seed))
        let status = app.staticTexts.matching(identifier: "scratchpad-status").firstMatch
        XCTAssertTrue(wait(for: status, label: "Shared with forks of \(title)"))
        attach(app, "Scratchpad")

        // Typing saves once it stops.
        type(editor, "\n- [ ] Try it on the phone")
        let typed = seed + "\n- [ ] Try it on the phone"
        XCTAssertTrue(eventually(timeout: 15) { (try? self.server.content()) == typed }, "The edit wasn't saved")
        XCTAssertTrue(wait(for: status, label: "Shared with forks of \(title)"))

        // The shared copy changing under a draft keeps the draft.
        type(editor, "\n- [ ] Mine")
        let draft = typed + "\n- [ ] Mine"
        let shared = typed + "\n\n## Questions"
        try server.set(shared)
        XCTAssertTrue(wait(for: status, label: "Conflict"))
        XCTAssertEqual(editor.value as? String, draft)
        app.buttons["scratchpad-review-changes"].tap()
        XCTAssertTrue(app.buttons["scratchpad-keep-editing"].waitForExistence(timeout: 5))
        XCTAssertTrue(element(app, "Only in your draft: - [ ] Mine").exists)
        XCTAssertTrue(element(app, "Only in the shared copy: ## Questions").exists)
        attach(app, "Review")

        // Use shared drops the draft.
        app.buttons["scratchpad-use-shared"].tap()
        XCTAssertTrue(editor.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: editor, value: shared))
        XCTAssertTrue(wait(for: status, label: "Shared with forks of \(title)"))

        // Replace shared with mine keeps it instead.
        type(editor, "\n- Push needs its own ADR.")
        let mine = shared + "\n- Push needs its own ADR."
        try server.set(seed)
        XCTAssertTrue(wait(for: status, label: "Conflict"))
        attach(app, "Conflict")
        app.buttons["scratchpad-review-changes"].tap()
        let replace = app.buttons["scratchpad-replace-shared"]
        XCTAssertTrue(replace.waitForExistence(timeout: 5))
        replace.tap()
        XCTAssertTrue(editor.waitForExistence(timeout: 5))
        XCTAssertTrue(eventually(timeout: 10) { (try? self.server.content()) == mine }, "Mine didn't replace the shared copy")
        XCTAssertEqual(editor.value as? String, mine)

        // Closing the pad saves what's typed without waiting.
        type(editor, "\n- Bye")
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(eventually(timeout: 4) { (try? self.server.content()) == mine + "\n- Bye" }, "Closing didn't save")
    }

    // MARK: Helpers

    private func element(_ app: XCUIApplication, _ label: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", label)).firstMatch
    }

    /// Types at the end of the pad.
    private func type(_ editor: XCUIElement, _ text: String) {
        editor.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9)).tap()
        editor.typeText(text)
    }

    private func wait(for element: XCUIElement, value: String, timeout: TimeInterval = 10) -> Bool {
        eventually(timeout: timeout) { element.value as? String == value }
    }

    /// Waits for a label, which may lead with the screen's title when the
    /// header reads as one element.
    private func wait(for element: XCUIElement, label: String, timeout: TimeInterval = 15) -> Bool {
        eventually(timeout: timeout) { element.label == label || element.label.hasSuffix(", " + label) }
    }

    private func eventually(timeout: TimeInterval, _ condition: @escaping () -> Bool) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if condition() { return true }
            RunLoop.current.run(until: Date().addingTimeInterval(0.25))
        }
        return condition()
    }

    @MainActor
    private func attach(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}

/// The other client: reads and writes the pad through the server's API,
/// with the isolated home's credentials.
private struct ScratchpadServer {
    let url: URL
    let headers: [String: String]

    init(home: String, session: String) throws {
        let run = URL(fileURLWithPath: home).appendingPathComponent("run")
        let info = try JSONSerialization.jsonObject(with: Data(contentsOf: run.appendingPathComponent("server.json")))
            as? [String: Any] ?? [:]
        let token = try String(contentsOf: run.appendingPathComponent("server.token"), encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard let base = info["url"] as? String, let instance = info["instanceId"] as? String,
              let version = info["protocolVersion"] else { throw XCTSkip("The home's server.json is incomplete.") }
        url = URL(string: base + "/v1/sessions/\(session)/scratchpad")!
        headers = ["Authorization": "Bearer \(token)", "X-Kit-Instance-ID": instance,
                   "X-Kit-Protocol-Version": "\(version)", "Content-Type": "application/json"]
    }

    func content() throws -> String { try record()["content"] as? String ?? "" }

    func set(_ content: String) throws {
        let revision = try record()["revision"] as? String ?? "0"
        let body = try JSONSerialization.data(withJSONObject: ["content": content, "expectedRevision": revision])
        _ = try request("PUT", body: body)
    }

    private func record() throws -> [String: Any] {
        try JSONSerialization.jsonObject(with: request("GET")) as? [String: Any] ?? [:]
    }

    private func request(_ method: String, body: Data? = nil) throws -> Data {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.httpBody = body
        for (name, value) in headers { request.setValue(value, forHTTPHeaderField: name) }
        final class Box: @unchecked Sendable { var result: Result<Data, Error> = .failure(URLError(.timedOut)) }
        let box = Box(), done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: request) { data, response, error in
            defer { done.signal() }
            if let error { box.result = .failure(error); return }
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            guard (200..<300).contains(status) else {
                box.result = .failure(NSError(domain: "scratchpad", code: status, userInfo: [
                    NSLocalizedDescriptionKey: "\(method) scratchpad: \(status) \(String(decoding: data ?? Data(), as: UTF8.self))"]))
                return
            }
            box.result = .success(data ?? Data())
        }.resume()
        _ = done.wait(timeout: .now() + 10)
        return try box.result.get()
    }
}
