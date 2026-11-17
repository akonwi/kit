import AppKit
import SwiftUI
import Testing
@testable import Kit

/// Release notes written as Kit's app releases are: a title, a lead that
/// ends with sentences every release repeats, the build lines, then
/// sections.
enum ReleaseNotesFixture {
    static func body(_ version: String, lead: String, topics: [(String, [String])], compatibility: String = "",
                     lineEnding: String = "\n") -> String {
        var lines = [
            "# Kit for macOS \(version)", "",
            lead + " This release works with Kit 0.51.0 or newer servers (session protocol 50), the same as 0.18.0. "
                + "See Compatibility. The app is released independently from the Kit CLI and does not include a server.", "",
            "Source commit: `117d5b0378953d5a25c777b6f8326ccb12ceb1d9`  ", "Bundle build: `1249`", "",
            "## What's changed", "",
        ]
        for (topic, items) in topics {
            lines += ["### \(topic)", ""] + items.map { "- " + $0 } + [""]
        }
        lines += ["## Compatibility", "", compatibility.isEmpty ? "Works with Kit 0.51.0 or newer servers." : compatibility, "",
                  "## Install or upgrade", "", "Download the ZIP.", "",
                  "## Limitations", "", "- Intel Macs are unsupported.", "",
                  "## Validation", "", "- The full suite passed."]
        return lines.joined(separator: lineEnding)
    }

    static let v0191 = body("0.19.1", lead: "**Terminals follow the theme and color scheme**. **Kit launches faster**.",
                            topics: [("Terminals", ["Every terminal takes a change to light or dark."]),
                                     ("Fixes", ["Wrapped lines stay inside the scratchpad editor."])],
                            compatibility: "It attaches to Kit CLI/server 0.51.0 or newer.")
    static let v0190 = body("0.19.0", lead: "**Annotate Mermaid diagrams** from a reply or a Diagram tab.",
                            topics: [("Annotate Mermaid diagrams", ["A diagram's card has an Annotate button."])])
    static let v0180 = body("0.18.0", lead: "**Find in a file tab** with ⌘F.",
                            topics: [("Find in a file", ["Edit › Find › Find… opens a find bar."])])
}

struct ReleaseNotesTests {
    @Test func theLeadLosesItsBuildLinesAndClosingSentences() {
        let notes = ReleaseNotes(markdown: ReleaseNotesFixture.v0191)
        #expect(notes.lead == "**Terminals follow the theme and color scheme**. **Kit launches faster**.")
    }

    @Test func whatsChangedAndCompatibilityAreTheirSectionsAsWritten() {
        let notes = ReleaseNotes(markdown: ReleaseNotesFixture.v0191)
        #expect(notes.changes == """
            ### Terminals

            - Every terminal takes a change to light or dark.

            ### Fixes

            - Wrapped lines stay inside the scratchpad editor.
            """)
        #expect(notes.compatibility == "It attaches to Kit CLI/server 0.51.0 or newer.")
        // The sheet's notes: the lead, then What's changed.
        #expect(notes.summary == notes.lead + "\n\n" + notes.changes)
    }

    @Test func notesWrittenWithWindowsLineEndingsReadTheSame() {
        let crlf = ReleaseNotesFixture.body("0.19.1", lead: "**Terminals follow the theme**.",
                                            topics: [("Terminals", ["Every terminal follows it."])], lineEnding: "\r\n")
        let notes = ReleaseNotes(markdown: crlf)
        #expect(notes.lead == "**Terminals follow the theme**.")
        #expect(notes.changes == "### Terminals\n\n- Every terminal follows it.")
    }

    /// Early releases had no What's changed, and a lead that is only a
    /// closing sentence is shown as written.
    @Test func notesWithoutTheUsualShapeShowWhatTheyHave() {
        let early = ReleaseNotes(markdown: "# Kit for macOS 0.1.0\r\n\r\nThe first regular release.\r\n\r\n## Installation\r\n\r\nUnzip it.")
        #expect(early.lead == "The first regular release.")
        #expect(early.changes.isEmpty && early.compatibility.isEmpty)
        #expect(early.summary == "The first regular release.")
        let bare = ReleaseNotes(markdown: "This release needs a Kit 0.51.0 or newer server.")
        #expect(bare.lead == "This release needs a Kit 0.51.0 or newer server.")
        let bold = ReleaseNotes(markdown: "Sessions are shared. **This release needs a Kit 0.51.0 or newer server**.")
        #expect(bold.lead == "Sessions are shared.")
    }
}

/// GitHub's release list, served a page at a time, recording each request.
@MainActor final class ReleasePages {
    struct Unavailable: Error {}

    /// Each page's entries; a missing page fails.
    var pages: [Int: [AppUpdateFixture.Listed]]
    private(set) var requested: [Int] = []

    init(_ pages: [Int: [AppUpdateFixture.Listed]]) { self.pages = pages }

    func fetch(_ number: Int) throws -> Data {
        requested.append(number)
        guard let page = pages[number] else { throw Unavailable() }
        return AppUpdateFixture.list(page)
    }

    /// A full page: releases, then CLI releases up to GitHub's page size.
    static func full(_ releases: [AppUpdateFixture.Listed], cliFrom start: Int) -> [AppUpdateFixture.Listed] {
        releases + (0..<(KitReleases.pageSize - releases.count)).map { .init(tag: "v0.\(start - $0).0") }
    }
}

@MainActor struct AppUpdatePagingTests {
    private func updater(_ current: String?, pages: ReleasePages) -> AppUpdater {
        var effects = AppUpdater.Effects.fixture(list: Data())
        effects.fetchList = { try pages.fetch($0) }
        return AppUpdater(current: current.flatMap(ReleaseVersion.init), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                          effects: effects, defaults: TestDefaults.make())
    }

    @Test func theListIsEveryReleaseSinceYoursNewestFirst() async {
        let pages = ReleasePages([1: [.init(tag: "macos-v0.19.1"), .init(tag: "v0.64.0"), .init(tag: "macos-v0.18.0"),
                                      .init(tag: "macos-v0.19.0"), .init(tag: "macos-v0.17.0"), .init(tag: "macos-v0.16.0")]])
        let updater = updater("0.17.0", pages: pages)

        #expect(await updater.check() == true)
        #expect(updater.state.release?.tag == "macos-v0.19.1")
        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.1", "macos-v0.19.0", "macos-v0.18.0"])
        // Your version is on the first page, so one request.
        #expect(pages.requested == [1])
    }

    @Test func oneReleaseBehindListsOnlyIt() async {
        let pages = ReleasePages([1: [.init(tag: "macos-v0.19.1"), .init(tag: "macos-v0.19.0")]])
        let updater = updater("0.19.0", pages: pages)

        await updater.check()

        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.1"])
    }

    @Test func kitReadsMorePagesUntilItReachesYourVersion() async {
        let pages = ReleasePages([
            1: ReleasePages.full([.init(tag: "macos-v0.19.1"), .init(tag: "macos-v0.19.0")], cliFrom: 64),
            2: [.init(tag: "macos-v0.18.0"), .init(tag: "v0.30.0"), .init(tag: "macos-v0.17.0"), .init(tag: "macos-v0.16.0")],
        ])
        let updater = updater("0.17.0", pages: pages)

        await updater.check()

        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.1", "macos-v0.19.0", "macos-v0.18.0"])
        #expect(pages.requested == [1, 2])
    }

    @Test func laterChecksDoNotReadThePagesAgainUntilANewerReleaseIsOut() async {
        let pages = ReleasePages([
            1: ReleasePages.full([.init(tag: "macos-v0.19.0")], cliFrom: 64),
            2: [.init(tag: "macos-v0.18.0"), .init(tag: "macos-v0.17.0")],
        ])
        let updater = updater("0.17.0", pages: pages)
        await updater.check()
        await updater.check()
        #expect(pages.requested == [1, 2, 1])
        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.0", "macos-v0.18.0"])

        pages.pages[1] = ReleasePages.full([.init(tag: "macos-v0.19.1"), .init(tag: "macos-v0.19.0")], cliFrom: 64)
        await updater.check()

        #expect(pages.requested == [1, 2, 1, 1, 2])
        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.1", "macos-v0.19.0", "macos-v0.18.0"])
    }

    /// The releases on a page that failed are left out, the update is still
    /// offered, and the next check reads the page again.
    @Test func aPageThatFailsLeavesTheReleasesThatLoaded() async {
        let pages = ReleasePages([1: ReleasePages.full([.init(tag: "macos-v0.19.1"), .init(tag: "macos-v0.19.0")], cliFrom: 64)])
        let updater = updater("0.17.0", pages: pages)

        #expect(await updater.check() == true)
        #expect(updater.state.release?.tag == "macos-v0.19.1")
        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.1", "macos-v0.19.0"])

        pages.pages[2] = [.init(tag: "macos-v0.18.0"), .init(tag: "macos-v0.17.0")]
        await updater.check()

        #expect(pages.requested == [1, 2, 1, 2])
        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.1", "macos-v0.19.0", "macos-v0.18.0"])
    }

    @Test func kitReadsAtMostFivePages() async {
        var listed: [Int: [AppUpdateFixture.Listed]] = [:]
        for number in 1...7 {
            listed[number] = ReleasePages.full([.init(tag: "macos-v1.\(10 - number).0")], cliFrom: 1000 - number * 100)
        }
        let pages = ReleasePages(listed)
        let updater = updater("0.1.0", pages: pages)

        await updater.check()

        #expect(pages.requested == [1, 2, 3, 4, 5])
        #expect(updater.releasesSince.map(\.tag) == ["macos-v1.9.0", "macos-v1.8.0", "macos-v1.7.0", "macos-v1.6.0", "macos-v1.5.0"])
    }

    @Test func aStartedUpdateKeepsTheListItWasOfferedWith() async throws {
        let pages = ReleasePages([1: [.init(tag: "macos-v0.19.0"), .init(tag: "macos-v0.18.0")]])
        var effects = AppUpdater.Effects.fixture(list: Data())
        effects.fetchList = { try pages.fetch($0) }
        effects.prepare = { _, _, _ in URL(fileURLWithPath: "/tmp/staged/Kit.app") }
        let updater = AppUpdater(current: ReleaseVersion("0.18.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: effects, defaults: TestDefaults.make())
        await updater.check()
        await updater.update()?.value

        pages.pages[1] = [.init(tag: "macos-v0.19.1"), .init(tag: "macos-v0.19.0"), .init(tag: "macos-v0.18.0")]
        await updater.check()

        #expect(updater.state == .ready(try #require(updater.releasesSince.first)))
        #expect(updater.releasesSince.map(\.tag) == ["macos-v0.19.0"])
    }
}

@MainActor struct ReleaseNotesMenuTests {
    private func updater(_ current: String?, fetch: @escaping (Int) throws -> Data, window: NSWindow? = nil) -> AppUpdater {
        var effects = AppUpdater.Effects.fixture(list: Data())
        effects.fetchList = { try fetch($0) }
        let updater = AppUpdater(current: current.flatMap(ReleaseVersion.init), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: effects, defaults: TestDefaults.make())
        updater.frontWindow = { window }
        return updater
    }

    @Test func releaseNotesShowThisVersionsNotesInTheFrontWindow() async {
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 400, height: 300))
        defer { window.close() }
        let pages = ReleasePages([1: [.init(tag: "macos-v0.19.1", body: ReleaseNotesFixture.v0191), .init(tag: "macos-v0.19.0")]])
        let updater = updater("0.19.1", fetch: pages.fetch, window: window)
        var opened: [URL] = []

        await updater.showReleaseNotes { opened.append($0) }

        #expect(updater.releaseNotesPending?.tag == "macos-v0.19.1")
        #expect(updater.releaseNotesPending?.notes == ReleaseNotesFixture.v0191)
        #expect(opened.isEmpty)
        updater.releaseNotesShown()
        #expect(updater.releaseNotesPending == nil)
    }

    @Test func releaseNotesUseTheReleaseACheckAlreadyListed() async {
        let pages = ReleasePages([1: [.init(tag: "macos-v0.19.1"), .init(tag: "macos-v0.19.0")]])
        let updater = updater("0.19.0", fetch: pages.fetch)
        await updater.check()

        #expect(await updater.installedReleaseNotes()?.tag == "macos-v0.19.0")
        #expect(pages.requested == [1])
    }

    @Test func releaseNotesPageToFindAnOlderVersion() async {
        let pages = ReleasePages([
            1: ReleasePages.full([.init(tag: "macos-v0.19.1")], cliFrom: 64),
            2: [.init(tag: "macos-v0.15.0"), .init(tag: "macos-v0.14.0")],
        ])
        let updater = updater("0.15.0", fetch: pages.fetch)

        #expect(await updater.installedReleaseNotes()?.tag == "macos-v0.15.0")
        #expect(pages.requested == [1, 2])
    }

    @Test func releaseNotesOpenGitHubWhenKitCantShowThem() async {
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 400, height: 300))
        defer { window.close() }
        let offline = updater("0.19.1", fetch: { _ in throw ReleasePages.Unavailable() }, window: window)
        var opened: [URL] = []
        await offline.showReleaseNotes { opened.append($0) }
        #expect(opened == [URL(string: "https://github.com/akonwi/kit/releases/tag/macos-v0.19.1")!])
        #expect(offline.releaseNotesPending == nil)

        // No session window to show them in.
        let pages = ReleasePages([1: [.init(tag: "macos-v0.19.1")]])
        let windowless = updater("0.19.1", fetch: pages.fetch, window: nil)
        await windowless.showReleaseNotes { opened.append($0) }
        #expect(opened.last == URL(string: "https://github.com/akonwi/kit/releases/tag/macos-v0.19.1")!)
    }

    @Test func aDevelopmentBuildOpensTheReleasesPageWithoutAsking() async {
        var fetched = false
        let updater = updater(nil, fetch: { _ in fetched = true; return Data() })
        var opened: [URL] = []

        await updater.showReleaseNotes { opened.append($0) }

        #expect(opened == [URL(string: "https://github.com/akonwi/kit/releases")!])
        #expect(!fetched)
    }
}

struct ThisMacServerLineTests {
    private static func identity(_ version: String) -> ServerIdentity {
        ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i", kitVersion: version,
                       protocolVersion: kitWireVersion, machine: String(repeating: "b", count: 64), home: "/Users/me")
    }

    @Test func thisMacsServerOlderThanTheNewestKitGetsTheLine() {
        let line = ServerRowPresentation.thisMacUpdate(name: ServerClient.localID, connector: .home(path: "/Users/me/.kit"),
                                                       status: .connected(Self.identity("0.62.0")), thisMac: true,
                                                       newest: ReleaseVersion("0.64.0"))
        #expect(line == .init(running: "0.62.0", newest: "0.64.0"))
    }

    @Test(arguments: [
        // Current.
        (ServerClient.localID, "0.64.0", true as Bool?),
        // Verified on another machine.
        (ServerClient.localID, "0.62.0", false as Bool?),
        // A saved server, updated where it runs.
        ("devbox", "0.62.0", true as Bool?),
    ])
    func otherServersGetNoLine(name: String, version: String, thisMac: Bool?) {
        #expect(ServerRowPresentation.thisMacUpdate(name: name, connector: .home(path: "/Users/me/.kit"),
                                                    status: .connected(Self.identity(version)), thisMac: thisMac,
                                                    newest: ReleaseVersion("0.64.0")) == nil)
    }
}
