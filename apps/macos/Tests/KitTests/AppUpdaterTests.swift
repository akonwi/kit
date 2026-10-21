import Foundation
import Testing
@testable import Kit

struct ReleaseVersionTests {
    @Test func parsesReleaseVersionsAndComparesThemNumerically() throws {
        let version = try #require(ReleaseVersion("v0.10.2"))
        #expect(version.description == "0.10.2")
        #expect(try #require(ReleaseVersion("0.9.9")) < version)
        #expect(try #require(ReleaseVersion("1.0.0")) > version)
        #expect(ReleaseVersion("0.10.2") == version)
    }

    @Test(arguments: ["", "dev", "1.2", "1.2.3.4", "1.2.3-rc.1", "01.2.3", "1..3", "vv1.2.3"])
    func rejectsAnythingButAReleaseVersion(text: String) {
        #expect(ReleaseVersion(text) == nil)
    }

    @Test func eachArtifactReadsOnlyItsOwnTags() {
        #expect(ReleaseArtifact.app.version(of: "macos-v0.5.0")?.description == "0.5.0")
        #expect(ReleaseArtifact.app.version(of: "v0.48.0") == nil)
        #expect(ReleaseArtifact.cli.version(of: "v0.48.0")?.description == "0.48.0")
        #expect(ReleaseArtifact.cli.version(of: "macos-v0.5.0") == nil)
    }
}

struct KitReleasesTests {
    @Test func newestSkipsDraftsPrereleasesAndOtherArtifacts() throws {
        let data = AppUpdateFixture.list([
            .init(tag: "macos-v0.7.0", draft: true),
            .init(tag: "macos-v0.6.0", prerelease: true),
            .init(tag: "v0.49.0"),
            .init(tag: "macos-v0.5.1", digest: "sha256:ABCDEF"),
            .init(tag: "macos-v0.5.0"),
        ])

        let app = try #require(try KitReleases.newest(.app, in: data))
        #expect(app.tag == "macos-v0.5.1")
        #expect(app.notes == "## Fixed\n- macos-v0.5.1")
        #expect(app.url == URL(string: "https://github.com/akonwi/kit/releases/tag/macos-v0.5.1"))
        #expect(app.appArchive == KitRelease.Asset(
            name: "kit_macos-v0.5.1_darwin_arm64.zip",
            url: URL(string: "https://example.invalid/kit_macos-v0.5.1_darwin_arm64.zip")!,
            size: 1024, sha256: "abcdef"))
        #expect(try KitReleases.newest(.cli, in: data)?.tag == "v0.49.0")
    }
}

struct AppUpdateInstallerTests {
    @Test func preparesAnArchiveThatMatchesItsDigestAndRelease() throws {
        let fixture = try AppUpdateFixture.archive(identifier: AppUpdateInstaller.bundleIdentifier, version: "0.5.1")
        defer { fixture.remove() }

        let app = try AppUpdateInstaller.prepare(
            archive: fixture.archive, release: fixture.release, asset: fixture.asset, in: fixture.staging,
            verifySignature: { _ in nil })

        #expect(app == fixture.staging.appendingPathComponent("macos-v0.5.1/Kit.app", isDirectory: true))
        #expect(Bundle(url: app)?.bundleIdentifier == AppUpdateInstaller.bundleIdentifier)
    }

    @Test func refusesAnArchiveWithoutTheExpectedDigest() throws {
        let fixture = try AppUpdateFixture.archive(identifier: AppUpdateInstaller.bundleIdentifier, version: "0.5.1")
        defer { fixture.remove() }
        let mismatched = KitRelease.Asset(name: fixture.asset.name, url: fixture.asset.url, size: 1, sha256: String(repeating: "0", count: 64))
        let missing = KitRelease.Asset(name: fixture.asset.name, url: fixture.asset.url, size: 1, sha256: nil)

        #expect(throws: AppUpdateInstaller.Failure.digestMismatch) {
            try AppUpdateInstaller.prepare(archive: fixture.archive, release: fixture.release, asset: mismatched,
                                           in: fixture.staging, verifySignature: { _ in nil })
        }
        #expect(throws: AppUpdateInstaller.Failure.noDigest) {
            try AppUpdateInstaller.prepare(archive: fixture.archive, release: fixture.release, asset: missing,
                                           in: fixture.staging, verifySignature: { _ in nil })
        }
    }

    @Test func refusesAnotherAppOrVersion() throws {
        for (identifier, version) in [("com.akonwi.kit.dev", "0.5.1"), (AppUpdateInstaller.bundleIdentifier, "0.5.0")] {
            let fixture = try AppUpdateFixture.archive(identifier: identifier, version: version)
            defer { fixture.remove() }
            #expect(throws: AppUpdateInstaller.Failure.wrongApp) {
                try AppUpdateInstaller.prepare(archive: fixture.archive, release: fixture.release, asset: fixture.asset,
                                               in: fixture.staging, verifySignature: { _ in nil })
            }
        }
    }

    @Test func refusesAnAppThatFailsSignatureChecks() throws {
        let fixture = try AppUpdateFixture.archive(identifier: AppUpdateInstaller.bundleIdentifier, version: "0.5.1")
        defer { fixture.remove() }

        #expect(throws: AppUpdateInstaller.Failure.notSigned) {
            try AppUpdateInstaller.prepare(archive: fixture.archive, release: fixture.release, asset: fixture.asset,
                                           in: fixture.staging)
        }
    }

    @Test func installReplacesTheAppWithTheStagedOne() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("kit-install-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: root) }
        let installed = root.appendingPathComponent("Applications/Kit.app")
        let staged = root.appendingPathComponent("staged/Kit.app")
        for (app, marker) in [(installed, "old"), (staged, "new")] {
            try FileManager.default.createDirectory(at: app.appendingPathComponent("Contents"), withIntermediateDirectories: true)
            try Data(marker.utf8).write(to: app.appendingPathComponent("marker"))
        }
        let info: [String: Any] = ["CFBundleIdentifier": AppUpdateInstaller.bundleIdentifier, "CFBundleShortVersionString": "0.5.1"]
        try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0)
            .write(to: staged.appendingPathComponent("Contents/Info.plist"))
        let release = KitRelease(tag: "macos-v0.5.1", version: ReleaseVersion("0.5.1")!, notes: "",
                                 url: KitReleases.page(tag: "macos-v0.5.1"), publishedAt: nil, assets: [])

        try AppUpdateInstaller.install(staged, over: installed, release: release, verifyCode: { _ in nil })

        #expect(try String(contentsOf: installed.appendingPathComponent("marker"), encoding: .utf8) == "new")
    }

    @Test func installChecksTheStagedAppAgainAndKeepsTheInstalledOne() throws {
        let fixture = try AppUpdateFixture.archive(identifier: AppUpdateInstaller.bundleIdentifier, version: "0.5.1")
        defer { fixture.remove() }
        let staged = try AppUpdateInstaller.prepare(archive: fixture.archive, release: fixture.release, asset: fixture.asset,
                                                    in: fixture.staging, verifySignature: { _ in nil })
        let installed = fixture.root.appendingPathComponent("Applications/Kit.app")
        try FileManager.default.createDirectory(at: installed, withIntermediateDirectories: true)
        try Data("old".utf8).write(to: installed.appendingPathComponent("marker"))

        #expect(throws: AppUpdateInstaller.Failure.notSigned) {
            try AppUpdateInstaller.install(staged, over: installed, release: fixture.release, verifyCode: { _ in .notSigned })
        }
        // The system cleaned the staged app away while it waited.
        try FileManager.default.removeItem(at: staged.appendingPathComponent("Contents/Info.plist"))
        #expect(throws: AppUpdateInstaller.Failure.wrongApp) {
            try AppUpdateInstaller.install(staged, over: installed, release: fixture.release, verifyCode: { _ in nil })
        }
        #expect(try String(contentsOf: installed.appendingPathComponent("marker"), encoding: .utf8) == "old")
    }

    @Test func discardRemovesTheFolderTheAppWasUnpackedInto() throws {
        let fixture = try AppUpdateFixture.archive(identifier: AppUpdateInstaller.bundleIdentifier, version: "0.5.1")
        defer { fixture.remove() }
        let staged = try AppUpdateInstaller.prepare(archive: fixture.archive, release: fixture.release, asset: fixture.asset,
                                                    in: fixture.staging, verifySignature: { _ in nil })

        AppUpdateInstaller.discard(staged)

        #expect(!FileManager.default.fileExists(atPath: fixture.staging.path))
    }
}

@MainActor struct AppUpdaterTests {
    @Test func checkOffersANewerAppAndRemembersTheNewestCLI() async throws {
        let updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: .fixture(list: AppUpdateFixture.list([.init(tag: "macos-v0.5.1"), .init(tag: "v0.49.0")])), defaults: AppUpdateFixture.defaults())

        #expect(await updater.check() == true)
        #expect(updater.state == .available(try #require(try KitReleases.newest(.app, in: AppUpdateFixture.list([.init(tag: "macos-v0.5.1")])))))
        #expect(updater.newestCLI?.tag == "v0.49.0")
    }

    @Test func checkLeavesAnUpToDateAppIdle() async {
        let updater = AppUpdater(current: ReleaseVersion("0.5.1"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: .fixture(list: AppUpdateFixture.list([.init(tag: "macos-v0.5.1")])), defaults: AppUpdateFixture.defaults())

        #expect(await updater.check() == false)
        #expect(updater.state == .idle)
    }

    @Test func developmentBuildsNeverCheck() async {
        var fetched = false
        var effects = AppUpdater.Effects.fixture(list: Data())
        effects.fetchList = { fetched = true; return Data() }
        let updater = AppUpdater(current: nil, installed: URL(fileURLWithPath: "/Applications/Kit.app"), effects: effects, defaults: AppUpdateFixture.defaults())

        #expect(await updater.check() == false)
        #expect(!fetched)
        #expect(updater.releaseNotesURL == URL(string: "https://github.com/akonwi/kit/releases"))
    }

    @Test func releaseNotesOpenTheRunningAppsRelease() {
        let updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: .fixture(list: Data()), defaults: AppUpdateFixture.defaults())
        #expect(updater.releaseNotesURL == URL(string: "https://github.com/akonwi/kit/releases/tag/macos-v0.5.0"))
    }

    @Test func aVerifiedUpdateWaitsForQuitToInstall() async throws {
        var installs: [(URL, URL)] = []
        var discarded: [URL] = []
        var effects = AppUpdater.Effects.fixture(list: AppUpdateFixture.list([.init(tag: "macos-v0.5.1")]))
        effects.prepare = { _, _, _ in URL(fileURLWithPath: "/tmp/staged/Kit.app") }
        effects.install = { staged, installed, _ in installs.append((staged, installed)) }
        effects.discard = { discarded.append($0) }
        let updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: effects, defaults: AppUpdateFixture.defaults())
        await updater.check()
        let release = try #require(updater.state.release)

        await updater.update()?.value
        #expect(updater.state == .ready(release))
        #expect(installs.isEmpty)

        updater.installOnQuit()
        #expect(installs.map(\.0.path) == ["/tmp/staged/Kit.app"])
        #expect(installs.map(\.1.path) == ["/Applications/Kit.app"])
        #expect(discarded.map(\.path) == ["/tmp/staged/Kit.app"])
    }

    @Test func aFailedInstallAtQuitReopensTheOldAppAndShowsTheFailureNextTime() async throws {
        let defaults = AppUpdateFixture.defaults()
        var relaunched: [URL] = []
        var effects = AppUpdater.Effects.fixture(list: AppUpdateFixture.list([.init(tag: "macos-v0.5.1")]))
        effects.prepare = { _, _, _ in URL(fileURLWithPath: "/tmp/staged/Kit.app") }
        effects.install = { _, _, _ throws(AppUpdateInstaller.Failure) in throw .install("The disk is full.") }
        effects.relaunch = { relaunched.append($0) }
        // Quitting proceeds, so the app installs as it would on quit.
        let installed = URL(fileURLWithPath: "/Applications/Kit.app")
        var updater: AppUpdater?
        effects.terminate = { updater?.installOnQuit() }
        updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: installed, effects: effects, defaults: defaults)
        let quitting = try #require(updater)
        await quitting.check()
        await quitting.update()?.value

        quitting.relaunch()
        #expect(relaunched == [installed])

        let reopened = AppUpdater(current: ReleaseVersion("0.5.0"), installed: installed, effects: effects, defaults: defaults)
        await reopened.check()
        let release = try #require(reopened.state.release)
        #expect(reopened.state == .failed(release, .install("The disk is full.")))
    }

    @Test func aCancelledQuitDoesNotRelaunchALaterOne() async throws {
        var relaunched = 0
        var effects = AppUpdater.Effects.fixture(list: AppUpdateFixture.list([.init(tag: "macos-v0.5.1")]))
        effects.prepare = { _, _, _ in URL(fileURLWithPath: "/tmp/staged/Kit.app") }
        effects.relaunch = { _ in relaunched += 1 }
        effects.terminate = {}
        let updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: effects, defaults: AppUpdateFixture.defaults())
        await updater.check()
        await updater.update()?.value

        updater.relaunch()
        updater.installOnQuit()

        #expect(relaunched == 0)
    }

    @Test func anAppKitCannotReplaceFailsBeforeDownloading() async throws {
        var downloads = 0
        var effects = AppUpdater.Effects.fixture(list: AppUpdateFixture.list([.init(tag: "macos-v0.5.1")]))
        effects.canReplace = { _ in false }
        effects.download = { _, _ in downloads += 1; return URL(fileURLWithPath: "/tmp/none.zip") }
        let updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"),
                                 effects: effects, defaults: AppUpdateFixture.defaults())
        await updater.check()
        let release = try #require(updater.state.release)

        #expect(updater.update() == nil)
        #expect(downloads == 0)
        #expect(updater.state == .failed(release, .install("Kit can’t change /Applications.")))
    }

    @Test func aFailedVerificationLeavesTheAppAndOffersARetry() async throws {
        var installed = false
        var effects = AppUpdater.Effects.fixture(list: AppUpdateFixture.list([.init(tag: "macos-v0.5.1")]))
        effects.prepare = { _, _, _ throws(AppUpdateInstaller.Failure) in throw .digestMismatch }
        effects.install = { _, _, _ in installed = true }
        let updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"), effects: effects, defaults: AppUpdateFixture.defaults())
        await updater.check()
        let release = try #require(updater.state.release)

        await updater.update()?.value
        updater.installOnQuit()

        #expect(updater.state == .failed(release, .digestMismatch))
        #expect(!installed)
    }

    @Test func aNewerReleaseDoesNotInterruptAStartedUpdate() async throws {
        var list = AppUpdateFixture.list([.init(tag: "macos-v0.5.1")])
        var effects = AppUpdater.Effects.fixture(list: Data())
        effects.fetchList = { list }
        effects.prepare = { _, _, _ in URL(fileURLWithPath: "/tmp/staged/Kit.app") }
        let updater = AppUpdater(current: ReleaseVersion("0.5.0"), installed: URL(fileURLWithPath: "/Applications/Kit.app"), effects: effects, defaults: AppUpdateFixture.defaults())
        await updater.check()
        await updater.update()?.value
        let ready = updater.state

        list = AppUpdateFixture.list([.init(tag: "macos-v0.6.0")])
        await updater.check()

        #expect(updater.state == ready)
    }
}

struct ServersSettingsTests {
    @Test func readsTheLocalServerVersionFromItsRegistry() throws {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent("kit-home-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: home) }
        try FileManager.default.createDirectory(at: home.appendingPathComponent("run"), withIntermediateDirectories: true)
        let registry = #"{"registryVersion":1,"protocolVersion":1,"kitVersion":"0.48.0","url":"http://127.0.0.1:1","instanceId":"i","pid":\#(ProcessInfo.processInfo.processIdentifier)}"#
        try Data(registry.utf8).write(to: home.appendingPathComponent("run/server.json"))

        let status = LocalServerStatus.read(home: home)

        #expect(status.running)
        #expect(status.version == "0.48.0")
    }

    @Test func aHomeWithoutARegistryHasNoRunningServer() {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent("kit-home-\(UUID().uuidString)")
        let status = LocalServerStatus.read(home: home)
        #expect(!status.running)
        #expect(status.version == nil)
    }

    @Test func updateCommandNamesTheAppsHome() {
        #expect(KitUpdateTerminal.command(home: URL(fileURLWithPath: "/Users/me/.kit")) == "KIT_HOME='/Users/me/.kit' kit update\n")
        #expect(KitUpdateTerminal.command(home: URL(fileURLWithPath: "/tmp/it's")) == "KIT_HOME='/tmp/it'\\''s' kit update\n")
        #expect(KitUpdateTerminal.command(home: URL(fileURLWithPath: "/tmp/line\nbreak")) == nil)
    }
}

enum AppUpdateFixture {
    /// Defaults no other test shares.
    static func defaults() -> UserDefaults {
        let suite = "kit-update-tests-\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defaults.removePersistentDomain(forName: suite)
        return defaults
    }

    struct Listed {
        var tag: String
        var draft = false
        var prerelease = false
        var digest: String? = nil
    }

    static func list(_ releases: [Listed]) -> Data {
        let objects: [[String: Any]] = releases.map { release in
            var asset: [String: Any] = [
                "name": "kit_\(release.tag)_darwin_arm64.zip",
                "browser_download_url": "https://example.invalid/kit_\(release.tag)_darwin_arm64.zip",
                "size": 1024,
            ]
            if let digest = release.digest { asset["digest"] = digest }
            return [
                "tag_name": release.tag, "body": "## Fixed\n- \(release.tag)", "draft": release.draft,
                "prerelease": release.prerelease, "published_at": "2025-05-01T12:00:00Z", "assets": [asset],
            ]
        }
        return try! JSONSerialization.data(withJSONObject: objects)
    }

    struct Archive {
        let root: URL
        let archive: URL
        let staging: URL
        let release: KitRelease
        let asset: KitRelease.Asset

        func remove() { try? FileManager.default.removeItem(at: root) }
    }

    /// A zipped Kit.app with identifier and version, listed as macos-v0.5.1.
    static func archive(identifier: String, version: String) throws -> Archive {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("kit-update-\(UUID().uuidString)")
        let contents = root.appendingPathComponent("build/Kit.app/Contents", isDirectory: true)
        try FileManager.default.createDirectory(at: contents, withIntermediateDirectories: true)
        let info: [String: Any] = ["CFBundleIdentifier": identifier, "CFBundleShortVersionString": version, "CFBundlePackageType": "APPL"]
        try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0)
            .write(to: contents.appendingPathComponent("Info.plist"))
        let archive = root.appendingPathComponent("Kit.zip")
        let ditto = Process()
        ditto.executableURL = URL(fileURLWithPath: "/usr/bin/ditto")
        ditto.arguments = ["-c", "-k", "--keepParent", contents.deletingLastPathComponent().path, archive.path]
        try ditto.run()
        ditto.waitUntilExit()
        let staging = root.appendingPathComponent("staging", isDirectory: true)
        try FileManager.default.createDirectory(at: staging, withIntermediateDirectories: true)
        let asset = KitRelease.Asset(name: "kit_macos-v0.5.1_darwin_arm64.zip", url: archive, size: 1,
                                     sha256: try AppUpdateInstaller.sha256(of: archive))
        let release = KitRelease(tag: "macos-v0.5.1", version: ReleaseVersion("0.5.1")!, notes: "",
                                 url: KitReleases.page(tag: "macos-v0.5.1"), publishedAt: nil, assets: [asset])
        return Archive(root: root, archive: archive, staging: staging, release: release, asset: asset)
    }
}

extension AppUpdater.Effects {
    static func fixture(list: Data) -> Self {
        Self(
            fetchList: { list },
            download: { _, _ in
                let file = FileManager.default.temporaryDirectory.appendingPathComponent("kit-update-\(UUID().uuidString).zip")
                try Data().write(to: file)
                return file
            },
            prepare: { _, _, _ throws(AppUpdateInstaller.Failure) in throw .unpack },
            install: { _, _, _ in },
            discard: { _ in },
            relaunch: { _ in },
            canReplace: { _ in true },
            terminate: {})
    }
}
