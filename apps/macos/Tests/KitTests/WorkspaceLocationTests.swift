import SwiftUI
import Testing
@testable import Kit

struct WorkspaceLocationTests {
    @Test func onlyPullRequestTextOwnsLinkAndUnderlineAttributes() throws {
        var session = excerpt()
        session.gitDirty = true
        let text = try #require(WorkspaceLocation.repositoryText(session))
        #expect(String(text.characters) == "(main* · PR #123)")
        let runs = text.runs.map { run in
            (String(text[run.range].characters), run.link, run.underlineStyle)
        }
        #expect(runs.count == 3)
        #expect(runs[0].0 == "(main* · ")
        #expect(runs[0].1 == nil && runs[0].2 == nil)
        #expect(runs[1].0 == "PR #123")
        #expect(runs[1].1?.absoluteString == "https://github.com/akonwi/kit/pull/123")
        #expect(runs[1].2 == .single)
        #expect(runs[2].0 == ")")
        #expect(runs[2].1 == nil && runs[2].2 == nil)
    }

    @Test func plainRepositoryRemainsPlainWithoutAValidPullRequest() throws {
        let text = try #require(WorkspaceLocation.repositoryText(excerpt(url: "javascript:alert(1)")))
        #expect(String(text.characters) == "(main)")
        #expect(text.runs.count == 1)
        #expect(text.runs.first?.link == nil)
        #expect(text.runs.first?.underlineStyle == nil)
    }

    @Test func homePathsUseTildeAtDirectoryBoundary() {
        #expect(WorkspaceLocation.homeRelative("/Users/person/Developer/kit", home: "/Users/person") == "~/Developer/kit")
        #expect(WorkspaceLocation.homeRelative("/Users/person", home: "/Users/person") == "~")
        #expect(WorkspaceLocation.homeRelative("/Users/person-two/kit", home: "/Users/person") == "/Users/person-two/kit")
        #expect(WorkspaceLocation.homeRelative("/tmp/kit", home: "/Users/person") == "/tmp/kit")
    }

    @Test func shorteningKeepsLastTwoSegments() {
        #expect(WorkspaceLocation.shortened("/Users/person/Developer/agent/kit-v2") == "…/agent/kit-v2")
        #expect(WorkspaceLocation.shortened("/repo") == "/repo")
        #expect(WorkspaceLocation.shortened("project/src") == "project/src")
    }

    private func excerpt(kind: String? = "branch", number: Int? = 123, url: String? = "https://github.com/akonwi/kit/pull/123") -> SessionExcerpt {
        var session = SessionExcerpt(id: "session", title: "Repository", sourceTitle: "Test", model: "p/m",
            thinking: "off", workspace: "repo", cwd: "/tmp/repo", date: "", messages: [])
        session.gitHead = "main"
        session.gitHeadKind = kind
        session.pullRequestNumber = number
        session.pullRequestURL = url
        return session
    }

    @Test func pullRequestLabelJoinsBranchAndDirtyMarker() {
        var session = excerpt()
        #expect(WorkspaceLocation.repositoryLabel(session) == "main · PR #123")
        session.gitDirty = true
        #expect(WorkspaceLocation.repositoryLabel(session) == "main* · PR #123")
    }

    @Test func pullRequestDestinationAcceptsOnlySafeAbsoluteHTTPTargets() {
        #expect(WorkspaceLocation.pullRequestDestination(excerpt())?.absoluteString == "https://github.com/akonwi/kit/pull/123")
        #expect(WorkspaceLocation.pullRequestDestination(excerpt(url: "http://github.internal/pull/123")) != nil)
        for unsafe in [
            "", "/pull/123", "javascript:alert(1)", "mailto:pr@example.com", "file:///etc/passwd",
            "https://user:secret@github.com/pull/123", "https://user@github.com/pull/123",
            "https://github.com/pull/12 3", "https://github.com/pull/123\n", "https://github.com/pull/\u{1b}123",
        ] {
            #expect(WorkspaceLocation.pullRequestDestination(excerpt(url: unsafe)) == nil, "accepted \(unsafe)")
        }
    }

    @Test func missingBranchLabelDoesNotMakeThePathClickable() {
        for head in [nil, ""] as [String?] {
            var session = excerpt()
            session.gitHead = head
            #expect(WorkspaceLocation.repositoryLabel(session) == nil)
            #expect(WorkspaceLocation.pullRequestDestination(session) == nil)
        }
    }

    @Test func pullRequestRequiresBranchHeadAndPositiveNumber() {
        #expect(WorkspaceLocation.pullRequestDestination(excerpt(kind: "detached")) == nil)
        #expect(WorkspaceLocation.pullRequestDestination(excerpt(kind: "unborn")) == nil)
        #expect(WorkspaceLocation.pullRequestDestination(excerpt(kind: nil)) == nil)
        #expect(WorkspaceLocation.pullRequestDestination(excerpt(number: 0)) == nil)
        #expect(WorkspaceLocation.pullRequestDestination(excerpt(number: -7)) == nil)
        #expect(WorkspaceLocation.pullRequestDestination(excerpt(number: nil)) == nil)
        // Without a safe destination, no PR label is advertised at all.
        #expect(WorkspaceLocation.repositoryLabel(excerpt(url: "javascript:alert(1)")) == "main")
        #expect(WorkspaceLocation.repositoryLabel(excerpt(kind: "detached")) == "detached main")
    }
}
