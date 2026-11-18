import XCTest

extension XCUIApplication {
    /// Sets the composer's send button style, `hold` or `split`, for this launch.
    func composerControl(_ style: String) {
        launchArguments += ["-composerControlStyle", style]
    }

    /// Opens the composer's menu: the split style's chevron, or a press
    /// and hold on the send button.
    @MainActor func openComposerMenu() {
        let more = buttons["composer-more"]
        if more.exists {
            more.tap()
            return
        }
        let mic = buttons["dictate"]
        let control = mic.exists ? mic : buttons["composer-action"]
        XCTAssertTrue(control.waitForExistence(timeout: 15))
        control.press(forDuration: 1)
    }

    /// Opens the command search from the composer's menu.
    @MainActor func openCommands() {
        openComposerMenu()
        let item = buttons["Commands"]
        XCTAssertTrue(item.waitForExistence(timeout: 5), "Expected Commands in the composer's menu")
        item.tap()
    }
}
