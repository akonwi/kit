import AppKit
import Foundation
import Observation

/// Checks GitHub for newer releases of the app and of the `kit` CLI, and
/// downloads, verifies, and installs the app's own (ADR 0048). A build
/// without a release version never checks.
@MainActor @Observable
final class AppUpdater {
    enum State: Equatable {
        case idle
        case available(KitRelease)
        /// Downloading and verifying, with the download's fraction once known.
        case updating(KitRelease, progress: Double?)
        /// Verified and waiting for a relaunch or quit to install.
        case ready(KitRelease)
        case failed(KitRelease, AppUpdateInstaller.Failure)

        var release: KitRelease? {
            switch self {
            case .idle: nil
            case .available(let release), .updating(let release, _), .ready(let release), .failed(let release, _): release
            }
        }
    }

    /// The updater's work, replaced in tests.
    struct Effects {
        var fetchList: () async throws -> Data
        /// Downloads an asset to a file, reporting the fraction received.
        var download: (KitRelease.Asset, @escaping @MainActor (Double?) -> Void) async throws -> URL
        /// Checks an archive and unpacks the verified app.
        var prepare: (URL, KitRelease, KitRelease.Asset) async throws(AppUpdateInstaller.Failure) -> URL
        var install: (URL, URL) throws(AppUpdateInstaller.Failure) -> Void
        var relaunch: (URL) -> Void
        var canReplace: (URL) -> Bool
    }

    static let shared = AppUpdater()

    private(set) var state: State = .idle
    /// The newest `kit` release, for Settings › Servers.
    private(set) var newestCLI: KitRelease?
    /// Asks the key window to show the update sheet; bumped by the menu.
    private(set) var sheetRequest = 0
    /// This app's release version, or nil for a development build.
    let current: ReleaseVersion?
    /// The installed app this updater replaces.
    let installed: URL
    @ObservationIgnored private let effects: Effects
    @ObservationIgnored private var schedule: Task<Void, Never>?
    @ObservationIgnored private var staged: URL?
    @ObservationIgnored private var relaunchRequested = false

    init(current: ReleaseVersion? = AppUpdater.releaseVersion(), installed: URL = Bundle.main.bundleURL,
         effects: Effects = .live) {
        self.current = current
        self.installed = installed
        self.effects = effects
    }

    /// The running bundle's release version: nil for a development bundle,
    /// which has its own identifier, or one bound to a channel's home.
    static func releaseVersion(bundle: Bundle = .main) -> ReleaseVersion? {
        guard bundle.bundleIdentifier == AppUpdateInstaller.bundleIdentifier,
              bundle.object(forInfoDictionaryKey: DevHomeResolver.infoKey) == nil,
              let version = bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String else { return nil }
        return ReleaseVersion(version)
    }

    /// The running release's notes on GitHub, or the releases list for a
    /// development build.
    var releaseNotesURL: URL {
        current.map { KitReleases.page(tag: ReleaseArtifact.app.rawValue + $0.description) } ?? KitReleases.releasesPage
    }

    /// Checks now and every six hours while the app runs.
    func start() {
        guard current != nil, schedule == nil else { return }
        schedule = Task { [weak self] in
            while !Task.isCancelled {
                _ = await self?.check()
                try? await Task.sleep(for: KitReleases.checkInterval)
            }
        }
    }

    /// Checks GitHub once. Returns whether an app update is known, or nil
    /// when the check failed. A failed check changes nothing.
    @discardableResult
    func check() async -> Bool? {
        guard let current else { return false }
        guard let data = try? await effects.fetchList() else { return nil }
        newestCLI = try? KitReleases.newest(.cli, in: data)
        guard let newest = try? KitReleases.newest(.app, in: data) else { return nil }
        guard newest.version > current else { return false }
        // A newer release replaces an older offer, but never interrupts an
        // update that has started.
        switch state {
        case .idle, .available, .failed: state = .available(newest)
        case .updating, .ready: break
        }
        return true
    }

    /// Kit › Check for Updates…: shows the sheet for an update, or says
    /// that Kit is up to date.
    func checkFromMenu() async {
        guard let current else {
            Self.alert("Updates are off for this build", "Development builds don’t check for updates.")
            return
        }
        switch await check() {
        case true?: sheetRequest += 1
        case false?: Self.alert("Kit is up to date", "Kit \(current) is the newest version.")
        case nil: Self.alert("Kit couldn’t check for updates", "GitHub couldn’t be reached. Try again later.")
        }
    }

    /// Downloads and verifies the available update in the background,
    /// returning the work when it started.
    @discardableResult
    func update() -> Task<Void, Never>? {
        guard let release = state.release else { return nil }
        switch state {
        case .available, .failed: break
        default: return nil
        }
        guard let asset = release.appArchive else {
            state = .failed(release, .noArchive)
            return nil
        }
        state = .updating(release, progress: nil)
        return Task {
            do {
                let archive = try await effects.download(asset) { [weak self] fraction in
                    guard let self, case .updating(let shown, let progress) = self.state, shown == release else { return }
                    // Redraw at whole percents.
                    let rounded = fraction.map { ($0 * 100).rounded(.down) / 100 }
                    if rounded != progress { self.state = .updating(release, progress: rounded) }
                }
                defer { try? FileManager.default.removeItem(at: archive) }
                let app = try await effects.prepare(archive, release, asset)
                staged = app
                state = .ready(release)
            } catch let failure as AppUpdateInstaller.Failure {
                state = .failed(release, failure)
            } catch {
                state = .failed(release, .download(error.localizedDescription))
            }
        }
    }

    /// Relaunch to update: quits, installing the update, and opens the new app.
    func relaunch() {
        guard case .ready(let release) = state else { return }
        guard effects.canReplace(installed) else {
            state = .failed(release, .install("Kit can’t change \(installed.deletingLastPathComponent().path)."))
            return
        }
        relaunchRequested = true
        NSApp.terminate(nil)
    }

    /// Quitting was cancelled, so a relaunch no longer follows it.
    func terminationCancelled() {
        relaunchRequested = false
    }

    /// Installs a verified update as the app quits, then relaunches it when
    /// that was asked for.
    func installOnQuit() {
        guard case .ready(let release) = state, let staged else { return }
        do throws(AppUpdateInstaller.Failure) {
            try effects.install(staged, installed)
            self.staged = nil
            if relaunchRequested { effects.relaunch(installed) }
        } catch {
            state = .failed(release, error)
        }
    }

    private static func alert(_ title: String, _ message: String) {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = message
        alert.runModal()
    }
}

extension AppUpdater.Effects {
    static var live: Self {
        Self(
            fetchList: { try await KitReleases.fetchList() },
            download: { asset, report in try await AppUpdateDownload.fetch(asset, report: report) },
            prepare: { archive, release, asset throws(AppUpdateInstaller.Failure) in
                let directory: URL
                do {
                    directory = try FileManager.default.url(for: .itemReplacementDirectory, in: .userDomainMask,
                                                            appropriateFor: Bundle.main.bundleURL, create: true)
                } catch { throw .unpack }
                return try AppUpdateInstaller.prepare(archive: archive, release: release, asset: asset, in: directory)
            },
            install: { staged, installed throws(AppUpdateInstaller.Failure) in try AppUpdateInstaller.install(staged, over: installed) },
            relaunch: { AppUpdateInstaller.relaunch($0, after: ProcessInfo.processInfo.processIdentifier) },
            canReplace: AppUpdateInstaller.canReplace)
    }
}

/// Downloads a release asset, reporting progress.
enum AppUpdateDownload {
    static func fetch(_ asset: KitRelease.Asset, report: @escaping @MainActor (Double?) -> Void) async throws -> URL {
        let observer = Observer(report: report)
        let (temporary, response) = try await URLSession.shared.download(from: asset.url, delegate: observer)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        // The system may remove its file once this returns, so keep a copy.
        let kept = FileManager.default.temporaryDirectory.appendingPathComponent("kit-update-\(UUID().uuidString).zip")
        try FileManager.default.moveItem(at: temporary, to: kept)
        guard status == 200 else {
            try? FileManager.default.removeItem(at: kept)
            throw AppUpdateInstaller.Failure.download("GitHub responded with status \(status).")
        }
        return kept
    }

    private final class Observer: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
        let report: @MainActor (Double?) -> Void
        private var observation: NSKeyValueObservation?

        init(report: @escaping @MainActor (Double?) -> Void) { self.report = report }

        func urlSession(_ session: URLSession, didCreateTask task: URLSessionTask) {
            observation = task.progress.observe(\.fractionCompleted) { [report] progress, _ in
                let fraction: Double? = progress.totalUnitCount > 0 ? progress.fractionCompleted : nil
                Task { @MainActor in report(fraction) }
            }
        }
    }
}
