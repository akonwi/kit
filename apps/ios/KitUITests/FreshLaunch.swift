import XCTest

extension XCUIApplication {
    /// The app, set to start without the composer drafts earlier tests left,
    /// since drafts are kept between launches.
    @MainActor static func withoutDrafts() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment["KIT_FORGET_DRAFTS"] = "1"
        return app
    }
}
