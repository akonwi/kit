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
        let repository = Self.repositoryText(session)
        let dirtyNote = session?.gitDirty == true ? " · uncommitted changes" : " · clean"
        HStack(spacing: 4) {
            ViewThatFits(in: .horizontal) {
                Text(displayPath).fixedSize()
                Text(Self.shortened(displayPath)).lineLimit(1).truncationMode(.head)
            }
            if let repository {
                // SwiftUI's attributed-text link opens through the environment's
                // openURL action and limits activation to the PR text range.
                Text(repository)
                    .lineLimit(1).truncationMode(.middle).layoutPriority(1)
            }
        }
        .font(.kit(size: 12, design: .monospaced))
        .help(path + (head.map { "\nGit snapshot: \($0)\(dirtyNote)" } ?? ""))
        .frame(maxWidth: 340, alignment: .trailing)
    }
}
