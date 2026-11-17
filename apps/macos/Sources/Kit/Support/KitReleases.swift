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

extension KitRelease: Identifiable {
    var id: String { tag }
}

/// A release's notes as the app shows them: the lead paragraph, What's
/// changed, and Compatibility. Install, Limitations and Validation stay on
/// GitHub.
struct ReleaseNotes: Equatable, Sendable {
    /// The paragraph that summarises the release, without the sentences
    /// every release ends it with and without the build lines.
    let lead: String
    /// What's changed as written, its topics as `###` headings.
    let changes: String
    /// The Compatibility section as written.
    let compatibility: String

    /// The lead and What's changed, the notes the sheet shows first.
    var summary: String {
        [lead, changes].filter { !$0.isEmpty }.joined(separator: "\n\n")
    }

    /// Sentences that close every lead and say what Compatibility says.
    private static let boilerplate = [
        "This release works with", "This release needs", "See Compatibility.", "The app is released independently",
    ]

    init(markdown: String) {
        var lines = markdown.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
        while let first = lines.first, first.trimmingCharacters(in: .whitespaces).isEmpty { lines.removeFirst() }
        if let first = lines.first, first.hasPrefix("# ") { lines.removeFirst() }
        var preamble: [String] = []
        var sections: [(title: String, lines: [String])] = []
        for line in lines {
            if line.hasPrefix("## ") {
                sections.append((String(line.dropFirst(3)).trimmingCharacters(in: .whitespaces), []))
            } else if sections.isEmpty {
                preamble.append(line)
            } else {
                sections[sections.count - 1].lines.append(line)
            }
        }
        func section(_ title: String) -> String {
            let wanted = Self.folded(title)
            guard let found = sections.first(where: { Self.folded($0.title) == wanted }) else { return "" }
            return found.lines.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        }
        lead = Self.lead(preamble)
        changes = section("What's changed")
        compatibility = section("Compatibility")
    }

    private static func folded(_ title: String) -> String {
        title.replacingOccurrences(of: "\u{2019}", with: "'").lowercased()
    }

    /// The preamble's paragraphs without the build lines, cut before the
    /// first closing sentence. A lead that would be left empty stays whole.
    private static func lead(_ preamble: [String]) -> String {
        let paragraphs = preamble.joined(separator: "\n").components(separatedBy: "\n\n")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty && !$0.hasPrefix("Source commit:") && !$0.hasPrefix("Bundle build:") }
        let lead = paragraphs.joined(separator: "\n\n")
        var cut = lead.endIndex
        for marker in boilerplate {
            for written in [marker, "**" + marker] {
                var from = lead.startIndex
                while let found = lead.range(of: written, range: from..<lead.endIndex) {
                    if found.lowerBound == lead.startIndex || lead[lead.index(before: found.lowerBound)].isWhitespace {
                        cut = min(cut, found.lowerBound)
                        break
                    }
                    from = found.upperBound
                }
            }
        }
        let trimmed = lead[..<cut].trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? lead : trimmed
    }
}

/// One page of GitHub's release list, which holds every artifact's releases.
struct KitReleasePage: Sendable {
    /// How many entries GitHub listed, of every artifact, drafts included.
    let count: Int
    private let listed: [KitReleases.Listed]

    init(_ data: Data) throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        listed = try decoder.decode([KitReleases.Listed].self, from: data)
        count = listed.count
    }

    /// The published releases of artifact on this page, newest first.
    /// Drafts, prereleases, and other artifacts' tags are skipped.
    func releases(_ artifact: ReleaseArtifact) -> [KitRelease] {
        listed.compactMap { listed -> KitRelease? in
            guard !listed.draft, !listed.prerelease, let version = artifact.version(of: listed.tagName) else { return nil }
            return KitRelease(
                tag: listed.tagName, version: version, notes: listed.body ?? "",
                url: KitReleases.page(tag: listed.tagName), publishedAt: listed.publishedAt,
                assets: listed.assets.map { asset in
                    let digest = asset.digest.flatMap { $0.hasPrefix("sha256:") ? String($0.dropFirst(7)).lowercased() : nil }
                    return KitRelease.Asset(name: asset.name, url: asset.browserDownloadUrl, size: asset.size, sha256: digest)
                })
        }
        .sorted { $0.version > $1.version }
    }
}

enum KitReleases {
    static let releasesPage = URL(string: "https://github.com/akonwi/kit/releases")!
    /// Entries asked for on each page of the list, GitHub's most.
    static let pageSize = 100
    /// The most pages Kit reads looking for a version, keeping well inside
    /// GitHub's anonymous rate limit.
    static let maxPages = 5
    /// How often a running app checks again.
    static let checkInterval: Duration = .seconds(2 * 60 * 60)

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

    /// Page `number` of Kit's release list, counting from 1.
    static func listURL(page number: Int) -> URL {
        URL(string: "https://api.github.com/repos/akonwi/kit/releases?per_page=\(pageSize)&page=\(number)")!
    }

    /// The newest published release of artifact in a GitHub release list.
    /// Drafts, prereleases, and other artifacts' tags are skipped.
    static func newest(_ artifact: ReleaseArtifact, in data: Data) throws -> KitRelease? {
        try KitReleasePage(data).releases(artifact).first
    }

    /// Fetches page `number` of Kit's release list from GitHub.
    static func fetchList(page number: Int, session: URLSession = .shared) async throws -> Data {
        var request = URLRequest(url: listURL(page: number), timeoutInterval: 30)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("2022-11-28", forHTTPHeaderField: "X-GitHub-Api-Version")
        let (data, response) = try await session.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else { throw Failure.unavailable(status) }
        return data
    }

    fileprivate struct Listed: Decodable, Sendable {
        let tagName: String
        let body: String?
        let draft: Bool
        let prerelease: Bool
        let publishedAt: Date?
        let assets: [ListedAsset]
    }

    fileprivate struct ListedAsset: Decodable, Sendable {
        let name: String
        let browserDownloadUrl: URL
        let size: Int64
        let digest: String?
    }
}
