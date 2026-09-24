import Foundation

/// Client-side safety gate for pull request metadata that crossed the wire.
/// The server validates before sending, but the client re-validates at its own
/// boundary so a compromised or older server cannot inject an unsafe click
/// target into the footer. Rules mirror the protocol contract: positive number
/// within JavaScript-safe integer range, and an absolute, credential-free
/// http(s) URL of bounded size with no whitespace, control, or format
/// characters and no backslash.
enum PullRequestLink {
    static let maxURLBytes = 4096
    /// 2^53 - 1, the largest integer every wire consumer represents exactly.
    static let maxNumber = 9_007_199_254_740_991

    /// The validated pull request target, or nil when any rule fails.
    static func destination(number: Int?, url: String?) -> URL? {
        guard let number, number > 0, number <= maxNumber,
              let raw = url, !raw.isEmpty, raw.utf8.count <= maxURLBytes,
              !raw.contains("\\"),
              raw.unicodeScalars.allSatisfy(isSafeScalar),
              let components = URLComponents(string: raw),
              let scheme = components.scheme?.lowercased(), scheme == "https" || scheme == "http",
              let host = components.host, !host.isEmpty,
              components.user == nil, components.password == nil
        else { return nil }
        return components.url
    }

    /// Strips invalid or non-branch pull request metadata from a wire VCS
    /// result so downstream state never stores an unsafe target.
    static func sanitized(_ result: WireSessionVCSStatus) -> WireSessionVCSStatus {
        guard let status = result.status, let pull = status.pullRequest else { return result }
        if status.head.kind == .value0, let head = status.head.name, !head.isEmpty,
           destination(number: pull.number, url: pull.url) != nil {
            return result
        }
        return WireSessionVCSStatus(
            sessionId: result.sessionId, cwd: result.cwd,
            status: WireVCSStatus(pullRequest: nil, root: status.root, head: status.head, dirty: status.dirty))
    }

    private static func isSafeScalar(_ scalar: Unicode.Scalar) -> Bool {
        if CharacterSet.controlCharacters.contains(scalar) { return false }
        if CharacterSet.whitespacesAndNewlines.contains(scalar) { return false }
        if CharacterSet.illegalCharacters.contains(scalar) { return false }
        if scalar.properties.generalCategory == .format { return false }
        return true
    }
}
