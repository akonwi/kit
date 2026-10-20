import Foundation
import Testing
@testable import Kit

/// The isolated home is a static computed property reading process state, so
/// these tests exercise the resolver through injectable inputs rather than
/// this process's own environment and bundle.
struct DevHomeResolutionTests {
    @Test func environmentOverridesBundledDevelopmentHome() throws {
        let home = try DevHomeResolver.resolve(
            environment: ["KIT_HOME": "/tmp/env-home"],
            bundledHome: "/tmp/bundled-home"
        )
        #expect(home == URL(fileURLWithPath: "/tmp/env-home", isDirectory: true).standardizedFileURL)
    }

    @Test func bundledDevelopmentHomeBeatsSharedDefault() throws {
        let home = try DevHomeResolver.resolve(
            environment: [:],
            bundledHome: "/tmp/bundled-home"
        )
        #expect(home == URL(fileURLWithPath: "/tmp/bundled-home", isDirectory: true).standardizedFileURL)
    }

    @Test func noHomeFallsBackToSharedDefault() throws {
        let home = try DevHomeResolver.resolve(environment: [:], bundledHome: nil)
        #expect(home.path.hasSuffix("/.kit"))
    }

    @Test func emptyValuesAreIgnored() throws {
        let home = try DevHomeResolver.resolve(
            environment: ["KIT_HOME": ""],
            bundledHome: ""
        )
        #expect(home.path.hasSuffix("/.kit"))
    }
}
