import SwiftUI

struct WorkspaceLocation: View {
    let session: SessionExcerpt?

    nonisolated static func homeRelative(_ path: String, home: String = FileManager.default.homeDirectoryForCurrentUser.path) -> String {
        if path == home { return "~" }
        guard home != "/", path.hasPrefix(home + "/") else { return path }
        return "~" + path.dropFirst(home.count)
    }

    nonisolated static func shortened(_ path: String) -> String {
        let parts = path.split(separator: "/")
        guard parts.count > 2 else { return path }
        return "…/" + parts.suffix(2).joined(separator: "/")
    }

    nonisolated static func branchLabel(_ session: SessionExcerpt?) -> String? {
        guard let head = session?.gitHead, !head.isEmpty else { return nil }
        let label = session?.gitHeadKind == "detached" ? "detached \(head)" : head
        return label + (session?.gitDirty == true ? "*" : "")
    }

    nonisolated static func repositoryLabel(_ session: SessionExcerpt?) -> String? {
        guard var label = branchLabel(session) else { return nil }
        if let number = pullRequestNumber(session) { label += " · PR #\(number)" }
        return label
    }

    /// Keep path, branch and punctuation plain; only the PR label owns a link.
    nonisolated static func repositoryText(_ session: SessionExcerpt?) -> AttributedString? {
        guard let head = branchLabel(session) else { return nil }
        var result = AttributedString("(\(head)")
        if let number = pullRequestNumber(session), let destination = pullRequestDestination(session) {
            result += AttributedString(" · ")
            var pullRequest = AttributedString("PR #\(number)")
            pullRequest.link = destination
            pullRequest.underlineStyle = .single
            result += pullRequest
        }
        result += AttributedString(")")
        return result
    }

    /// The pull request number for presentation, only when its click target is
    /// also safe: partial metadata renders nothing rather than an inert label.
    nonisolated static func pullRequestNumber(_ session: SessionExcerpt?) -> Int? {
        guard pullRequestDestination(session) != nil else { return nil }
        return session?.pullRequestNumber
    }

    /// Validates the pull request target for opening via the shared
    /// model-layer validator: a named branch head with a positive number and a
    /// safe absolute http(s) URL.
    nonisolated static func pullRequestDestination(_ session: SessionExcerpt?) -> URL? {
        guard let session, session.gitHeadKind == "branch",
              let head = session.gitHead, !head.isEmpty else { return nil }
        return PullRequestLink.destination(number: session.pullRequestNumber, url: session.pullRequestURL)
    }

    var body: some View {
        let path = session?.cwd ?? session?.workspace ?? ""
        let displayPath = Self.homeRelative(path)
        let head = Self.repositoryLabel(session)
        let dirtyNote = session?.gitDirty == true ? " · uncommitted changes" : " · clean"
        HStack(spacing: 12) {
            if !path.isEmpty {
                HStack(spacing: 5) {
                    IconView(.folderSimple, size: 12)
                    ViewThatFits(in: .horizontal) {
                        Text(displayPath).fixedSize()
                        Text(Self.shortened(displayPath)).lineLimit(1).truncationMode(.head)
                    }
                }
                .help(path)
            }
            if let branch = Self.branchLabel(session) {
                HStack(spacing: 5) {
                    IconView(.gitBranch, size: 12)
                    Text(branch).lineLimit(1).truncationMode(.middle)
                }
                .layoutPriority(1)
                .help(head.map { "Git snapshot: \($0)\(dirtyNote)" } ?? "")
            }
            if let number = Self.pullRequestNumber(session), let destination = Self.pullRequestDestination(session) {
                PullRequestChip(number: number, destination: destination).layoutPriority(2)
            }
        }
        .font(.kit(size: 11, design: .monospaced))
        .foregroundStyle(theme.muted)
        .lineLimit(1)
    }
    @Environment(\.mica) private var theme
}

/// Opens the session branch's pull request.
private struct PullRequestChip: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    let number: Int
    let destination: URL

    var body: some View {
        Button { openURL(destination) } label: {
            HStack(spacing: 5) {
                IconView(.gitPullRequest, size: 12)
                Text("PR #\(number)")
            }
            .foregroundStyle(theme.text)
            .padding(.horizontal, 6)
            .frame(height: 20)
            .background(theme.frameHover, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help(destination.absoluteString)
        .accessibilityLabel("Open pull request \(number)")
    }
}
