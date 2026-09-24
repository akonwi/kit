import Foundation
import Testing
@testable import Kit

struct PullRequestLinkTests {
    private let safe = "https://github.com/akonwi/kit/pull/42"

    @Test func acceptsSafeAbsoluteHTTPTargets() {
        #expect(PullRequestLink.destination(number: 42, url: safe)?.absoluteString == safe)
        #expect(PullRequestLink.destination(number: 1, url: "http://github.internal/pull/1") != nil)
        #expect(PullRequestLink.destination(number: PullRequestLink.maxNumber, url: safe) != nil)
    }

    @Test func rejectsInvalidNumbers() {
        #expect(PullRequestLink.destination(number: nil, url: safe) == nil)
        #expect(PullRequestLink.destination(number: 0, url: safe) == nil)
        #expect(PullRequestLink.destination(number: -7, url: safe) == nil)
        #expect(PullRequestLink.destination(number: PullRequestLink.maxNumber + 1, url: safe) == nil)
    }

    @Test func rejectsUnsafeURLs() {
        let unsafe: [String] = [
            "", "/pull/42", "pull/42",
            "javascript:alert(1)", "mailto:pr@example.com", "file:///etc/passwd", "ftp://github.com/pull/42",
            "https:///pull/42",
            "https://user:secret@github.com/pull/42", "https://user@github.com/pull/42",
            "https://github.com/pull/4 2", "https://github.com/pull/42\n", "https://github.com/pull/42\t",
            "https://github.com/pull/\u{1b}42", "https://github.com/pull\\42",
            "https://github.com/pull/\u{200e}42", // Cf format character
            "https://github.com/pull/" + String(repeating: "4", count: 4097),
        ]
        for raw in unsafe {
            #expect(PullRequestLink.destination(number: 42, url: raw) == nil, "accepted \(raw)")
        }
    }

    @Test func boundsURLLengthInUTF8Bytes() {
        let path = "https://github.com/"
        let fitting = path + String(repeating: "a", count: PullRequestLink.maxURLBytes - path.utf8.count)
        #expect(PullRequestLink.destination(number: 42, url: fitting) != nil)
        #expect(PullRequestLink.destination(number: 42, url: fitting + "a") == nil)
        // Multibyte characters count as their encoded size, not their length.
        let wide = path + String(repeating: "\u{00e9}", count: (PullRequestLink.maxURLBytes - path.utf8.count) / 2 + 1)
        #expect(wide.count < PullRequestLink.maxURLBytes)
        #expect(PullRequestLink.destination(number: 42, url: wide) == nil)
    }

    @Test func branchWithoutANameCannotCarryPullRequestAction() {
        for head in [nil, ""] as [String?] {
            let result = WireSessionVCSStatus(sessionId: "session", cwd: "/tmp/repo", status: .init(
                pullRequest: .init(number: 42, url: safe), root: "/tmp/repo",
                head: .init(kind: .value0, name: head, oid: nil), dirty: false))
            #expect(PullRequestLink.sanitized(result).status?.pullRequest == nil)
        }
    }

    @Test func sanitizedStripsUnsafeOrNonBranchWirePullRequests() {
        func result(kind: WireVCSHeadKind, number: Int, url: String) -> WireSessionVCSStatus {
            .init(sessionId: "session", cwd: "/tmp/repo", status: .init(
                pullRequest: .init(number: number, url: url),
                root: "/tmp/repo", head: .init(kind: kind, name: "main", oid: nil), dirty: false))
        }
        #expect(PullRequestLink.sanitized(result(kind: .value0, number: 42, url: safe)).status?.pullRequest?.number == 42)
        #expect(PullRequestLink.sanitized(result(kind: .value0, number: 0, url: safe)).status?.pullRequest == nil)
        #expect(PullRequestLink.sanitized(result(kind: .value0, number: 42, url: "javascript:alert(1)")).status?.pullRequest == nil)
        #expect(PullRequestLink.sanitized(result(kind: .value1, number: 42, url: safe)).status?.pullRequest == nil)
        #expect(PullRequestLink.sanitized(result(kind: .value2, number: 42, url: safe)).status?.pullRequest == nil)
        // Stripping preserves the rest of the repository status.
        let stripped = PullRequestLink.sanitized(result(kind: .value0, number: 0, url: safe))
        #expect(stripped.sessionId == "session" && stripped.cwd == "/tmp/repo")
        #expect(stripped.status?.head.name == "main" && stripped.status?.dirty == false)
        // Results without pull request metadata pass through untouched.
        let plain = WireSessionVCSStatus(sessionId: "session", cwd: "/tmp/repo", status: nil)
        #expect(PullRequestLink.sanitized(plain).status == nil)
    }
}
