import CryptoKit
import Foundation
import Security

/// Checks a downloaded app release and puts it in place of the running app
/// (ADR 0048). Nothing here changes the installed app until `install`.
enum AppUpdateInstaller {
    static let bundleIdentifier = "com.akonwi.kit"
    /// Kit's Developer ID team.
    static let teamIdentifier = "M7B73F53MK"

    /// Why an update could not be installed. The installed app is unchanged.
    enum Failure: Error, Equatable {
        case download(String)
        case noArchive
        case noDigest
        case digestMismatch
        case unpack
        case wrongApp
        case notSigned
        case notNotarized
        case install(String)

        /// What went wrong, in one sentence.
        var title: String {
            switch self {
            case .download: "The download didn’t finish."
            case .noArchive: "This release has no app for Apple Silicon."
            case .noDigest: "GitHub lists no checksum for the download."
            case .digestMismatch: "The download doesn’t match its checksum."
            case .unpack: "Kit couldn’t open the download."
            case .wrongApp: "The download isn’t the expected Kit release."
            case .notSigned: "The download isn’t signed by Kit’s developer."
            case .notNotarized: "Apple hasn’t notarized the download."
            case .install: "Kit couldn’t replace the installed app."
            }
        }

        /// What to do about it.
        var detail: String {
            switch self {
            case .download(let reason): "\(reason) Try again, or download the release yourself."
            case .noArchive, .wrongApp, .unpack: "Try again later, or download the release yourself."
            case .noDigest, .digestMismatch:
                "Kit only installs downloads that match the checksum GitHub publishes. Try again, or download the release yourself."
            case .notSigned: "Kit only installs updates signed with its Developer ID. Try again, or download the release yourself."
            case .notNotarized: "Kit only installs updates that pass Gatekeeper. Try again, or download the release yourself."
            case .install(let reason): "\(reason) Move Kit to a folder you can change, such as Applications, or download the release yourself."
            }
        }
    }

    /// The lowercase hex SHA-256 digest of a file.
    static func sha256(of file: URL) throws -> String {
        let handle = try FileHandle(forReadingFrom: file)
        defer { try? handle.close() }
        var hash = SHA256()
        while let chunk = try handle.read(upToCount: 1 << 20), !chunk.isEmpty {
            hash.update(data: chunk)
        }
        return hash.finalize().map { String(format: "%02x", $0) }.joined()
    }

    /// Checks a downloaded archive against release and unpacks its app into
    /// directory, returning the verified app.
    static func prepare(archive: URL, release: KitRelease, asset: KitRelease.Asset, in directory: URL,
                        verifySignature: (URL) -> Failure? = verifySignature) throws(Failure) -> URL {
        guard let expected = asset.sha256, !expected.isEmpty else { throw .noDigest }
        guard (try? sha256(of: archive)) == expected else { throw .digestMismatch }
        let destination = directory.appendingPathComponent(release.tag, isDirectory: true)
        try? FileManager.default.removeItem(at: destination)
        do {
            try FileManager.default.createDirectory(at: destination, withIntermediateDirectories: true)
        } catch { throw .unpack }
        let ditto = Process()
        ditto.executableURL = URL(fileURLWithPath: "/usr/bin/ditto")
        ditto.arguments = ["-x", "-k", archive.path, destination.path]
        do {
            try ditto.run()
            ditto.waitUntilExit()
        } catch { throw .unpack }
        guard ditto.terminationStatus == 0 else { throw .unpack }
        let app = destination.appendingPathComponent("Kit.app", isDirectory: true)
        guard let bundle = Bundle(url: app), bundle.bundleIdentifier == bundleIdentifier,
              let version = bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String,
              ReleaseVersion(version) == release.version else { throw .wrongApp }
        if let failure = verifySignature(app) { throw failure }
        return app
    }

    /// Checks that app is signed by Kit's Developer ID and that Gatekeeper
    /// accepts it, which requires Apple's notarization.
    static func verifySignature(_ app: URL) -> Failure? {
        var code: SecStaticCode?
        guard SecStaticCodeCreateWithPath(app as CFURL, [], &code) == errSecSuccess, let code else { return .notSigned }
        let text = "anchor apple generic and identifier \"\(bundleIdentifier)\" and certificate leaf[subject.OU] = \"\(teamIdentifier)\""
        var requirement: SecRequirement?
        guard SecRequirementCreateWithString(text as CFString, [], &requirement) == errSecSuccess, let requirement else { return .notSigned }
        let flags = SecCSFlags(rawValue: SecCSFlags.RawValue(kSecCSCheckAllArchitectures | kSecCSStrictValidate | kSecCSCheckNestedCode))
        guard SecStaticCodeCheckValidity(code, flags, requirement) == errSecSuccess else { return .notSigned }
        let assessment = Process()
        assessment.executableURL = URL(fileURLWithPath: "/usr/sbin/spctl")
        assessment.arguments = ["--assess", "--type", "execute", app.path]
        assessment.standardOutput = FileHandle.nullDevice
        assessment.standardError = FileHandle.nullDevice
        do {
            try assessment.run()
            assessment.waitUntilExit()
        } catch { return .notNotarized }
        return assessment.terminationStatus == 0 ? nil : .notNotarized
    }

    /// Whether the installed app's folder can be changed, so an install
    /// can succeed.
    static func canReplace(_ installed: URL) -> Bool {
        FileManager.default.isWritableFile(atPath: installed.deletingLastPathComponent().path)
            && FileManager.default.isWritableFile(atPath: installed.path)
    }

    /// Replaces the installed app with the verified staged one.
    static func install(_ staged: URL, over installed: URL) throws(Failure) {
        do {
            _ = try FileManager.default.replaceItemAt(installed, withItemAt: staged)
        } catch {
            throw .install(error.localizedDescription)
        }
    }

    /// Opens app once the process pid has exited.
    static func relaunch(_ app: URL, after pid: Int32) {
        let waiter = Process()
        waiter.executableURL = URL(fileURLWithPath: "/bin/sh")
        waiter.arguments = ["-c", "while /bin/kill -0 \(pid) 2>/dev/null; do /bin/sleep 0.2; done; /usr/bin/open \"$0\"", app.path]
        try? waiter.run()
    }
}
