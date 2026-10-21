import Foundation

/// A Kit release version, such as 0.5.0, compared numerically (ADR 0048).
struct ReleaseVersion: Comparable, CustomStringConvertible, Sendable {
    let major: Int
    let minor: Int
    let patch: Int

    /// Parses "X.Y.Z" with an optional leading "v". Prerelease and build
    /// suffixes are not release versions.
    init?(_ text: String) {
        let trimmed = text.hasPrefix("v") ? String(text.dropFirst()) : text
        let parts = trimmed.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 3 else { return nil }
        var numbers: [Int] = []
        for part in parts {
            guard !part.isEmpty, part.allSatisfy({ $0.isASCII && $0.isNumber }),
                  part == "0" || !part.hasPrefix("0"), let number = Int(part) else { return nil }
            numbers.append(number)
        }
        (major, minor, patch) = (numbers[0], numbers[1], numbers[2])
    }

    static func < (lhs: Self, rhs: Self) -> Bool {
        (lhs.major, lhs.minor, lhs.patch) < (rhs.major, rhs.minor, rhs.patch)
    }

    var description: String { "\(major).\(minor).\(patch)" }
}

/// Kit's independently versioned artifacts, told apart by tag prefix.
enum ReleaseArtifact: String, Sendable {
    /// The macOS app, tagged macos-vX.Y.Z.
    case app = "macos-v"
    /// The `kit` executable, tagged vX.Y.Z.
    case cli = "v"

    /// The version a tag names for this artifact, or nil for another
    /// artifact's tag.
    func version(of tag: String) -> ReleaseVersion? {
        guard tag.hasPrefix(rawValue) else { return nil }
        let rest = tag.dropFirst(rawValue.count)
        guard !rest.hasPrefix("v") else { return nil }
        return ReleaseVersion(String(rest))
    }
}

/// A published Kit release.
struct KitRelease: Equatable, Sendable {
    struct Asset: Equatable, Sendable {
        let name: String
        let url: URL
        let size: Int64
        /// The lowercase hex SHA-256 digest GitHub reports, if any.
        let sha256: String?
    }

    let tag: String
    let version: ReleaseVersion
    /// The release notes, in Markdown.
    let notes: String
    /// The release's page on GitHub, built from its tag.
    let url: URL
    let publishedAt: Date?
    let assets: [Asset]

    /// The app archive this release publishes for Apple Silicon.
    var appArchive: Asset? { assets.first { $0.name == "kit_\(tag)_darwin_arm64.zip" } }
}

enum KitReleases {
    static let releasesPage = URL(string: "https://github.com/akonwi/kit/releases")!
    static let listURL = URL(string: "https://api.github.com/repos/akonwi/kit/releases?per_page=50")!
    /// How often a running app checks again.
    static let checkInterval: Duration = .seconds(6 * 60 * 60)

    enum Failure: LocalizedError, Equatable {
        case unavailable(Int)

        var errorDescription: String? {
            switch self {
            case .unavailable(let status): "GitHub responded with status \(status)."
            }
        }
    }

    /// The GitHub page of the release with tag.
    static func page(tag: String) -> URL {
        releasesPage.appendingPathComponent("tag").appendingPathComponent(tag)
    }

    /// The newest published release of artifact in a GitHub release list.
    /// Drafts, prereleases, and other artifacts' tags are skipped.
    static func newest(_ artifact: ReleaseArtifact, in data: Data) throws -> KitRelease? {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        var newest: KitRelease?
        for listed in try decoder.decode([Listed].self, from: data) {
            guard !listed.draft, !listed.prerelease, let version = artifact.version(of: listed.tagName) else { continue }
            if let newest, version <= newest.version { continue }
            newest = KitRelease(
                tag: listed.tagName, version: version, notes: listed.body ?? "",
                url: page(tag: listed.tagName), publishedAt: listed.publishedAt,
                assets: listed.assets.map { asset in
                    let digest = asset.digest.flatMap { $0.hasPrefix("sha256:") ? String($0.dropFirst(7)).lowercased() : nil }
                    return KitRelease.Asset(name: asset.name, url: asset.browserDownloadUrl, size: asset.size, sha256: digest)
                })
        }
        return newest
    }

    /// Fetches Kit's release list from GitHub.
    static func fetchList(session: URLSession = .shared) async throws -> Data {
        var request = URLRequest(url: listURL, timeoutInterval: 30)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("2022-11-28", forHTTPHeaderField: "X-GitHub-Api-Version")
        let (data, response) = try await session.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else { throw Failure.unavailable(status) }
        return data
    }

    private struct Listed: Decodable {
        let tagName: String
        let body: String?
        let draft: Bool
        let prerelease: Bool
        let publishedAt: Date?
        let assets: [ListedAsset]
    }

    private struct ListedAsset: Decodable {
        let name: String
        let browserDownloadUrl: URL
        let size: Int64
        let digest: String?
    }
}
