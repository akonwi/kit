import Foundation
import OpenAPIRuntime

/// The observed pull request of a session workspace's named branch, with a
/// bounded summary of its checks, as the server's pull request stream reports
/// it. Every value has passed the protocol's rules at this client's boundary.
public struct SessionPullRequestUpdate: Equatable, Sendable {
    public let sessionID: String
    public let cwd: String
    /// The named branch, or nil outside one.
    public let branch: String?
    /// The branch's pull request, or nil without one, while the lookup is
    /// pending, or when GitHub is unavailable.
    public let pullRequest: PullRequestStatus?

    public init(sessionID: String, cwd: String, branch: String?, pullRequest: PullRequestStatus?) {
        self.sessionID = sessionID
        self.cwd = cwd
        self.branch = branch
        self.pullRequest = pullRequest
    }
}

/// One pull request and its checks.
public struct PullRequestStatus: Codable, Equatable, Sendable {
    public enum State: String, Codable, Sendable { case open, closed, merged }

    public let number: Int
    public let url: URL
    public let state: State
    public let draft: Bool
    public let checks: PullRequestChecks

    public init(number: Int, url: URL, state: State, draft: Bool, checks: PullRequestChecks) {
        self.number = number
        self.url = url
        self.state = state
        self.draft = draft
        self.checks = checks
    }
}

/// A pull request's checks: the overall state, every check counted by state,
/// and at most `maxListed` checks listed, most urgent first.
public struct PullRequestChecks: Codable, Equatable, Sendable {
    /// Failing when any check failed or was canceled, otherwise pending when
    /// any is running, otherwise passing when any passed, otherwise none.
    public enum State: String, Codable, Sendable { case passing, failing, pending, none }
    /// Whether the repository's required checks are satisfied.
    public enum Requirement: String, Codable, Sendable { case unknown, pending, satisfied, unsatisfied }
    public enum CheckState: String, Codable, Sendable { case passed, failed, pending, skipped, canceled, unknown }

    public struct Check: Codable, Equatable, Sendable {
        public let name: String
        public let workflow: String?
        public let state: CheckState
        public let required: Bool

        public init(name: String, workflow: String? = nil, state: CheckState, required: Bool = false) {
            self.name = name
            self.workflow = workflow
            self.state = state
            self.required = required
        }
    }

    public struct Counts: Codable, Equatable, Sendable {
        public var passed = 0, failed = 0, pending = 0, skipped = 0, canceled = 0, unknown = 0
        public var total: Int { passed + failed + pending + skipped + canceled + unknown }

        public init(passed: Int = 0, failed: Int = 0, pending: Int = 0, skipped: Int = 0, canceled: Int = 0, unknown: Int = 0) {
            self.passed = passed; self.failed = failed; self.pending = pending
            self.skipped = skipped; self.canceled = canceled; self.unknown = unknown
        }
    }

    /// The most checks a status lists; `omitted` counts the rest.
    public static let maxListed = 50
    /// The most UTF-8 bytes in a listed check's name or workflow.
    public static let maxTextBytes = 256

    public let state: State
    public let required: Requirement
    public let counts: Counts
    public let items: [Check]
    public let omitted: Int

    public init(state: State, required: Requirement, counts: Counts, items: [Check], omitted: Int = 0) {
        self.state = state
        self.required = required
        self.counts = counts
        self.items = items
        self.omitted = omitted
    }
}

/// An older server doesn't serve the pull request stream. Clients show no
/// check status rather than an error.
public struct PullRequestStreamNotServed: Error, Equatable {
    public init() {}
}

/// Pull request status payloads from the generated `streamSessionPullRequest`
/// operation, validated and projected at the client's boundary.
public enum PullRequestStatusRecords {
    /// The only record name `streamSessionPullRequest` declares.
    public static let recordName = "pullRequest.status"

    /// Decodes one dispatched stream event. Comment-only dispatches carry no
    /// fields and return nil. Any other event must be exactly one
    /// `pullRequest.status` record with a single data line and no id, whose
    /// payload passes strict decoding and the protocol's rules.
    public static func decode(_ event: ServerSentEvent, session: String) throws -> SessionPullRequestUpdate? {
        if event.event == nil, event.data == nil, event.id == nil, event.retry == nil { return nil }
        guard event.event == recordName, event.id == nil, event.retry == nil,
              let data = event.data, !data.contains("\n") else { throw ClientError.invalidPayload }
        return try validated(payload(Data(data.utf8)), session: session)
    }

    /// Strictly decodes one payload with the generated schema type, which
    /// rejects unknown members; duplicate members are rejected beforehand.
    public static func payload(_ data: Data) throws -> Components.Schemas.SessionPullRequestStatus {
        guard StrictJSON.uniqueMembers(data) else { throw ClientError.invalidPayload }
        do { return try JSONDecoder().decode(Components.Schemas.SessionPullRequestStatus.self, from: data) }
        catch { throw ClientError.invalidPayload }
    }

    /// Applies the protocol's rules and projects the payload.
    public static func validated(_ payload: Components.Schemas.SessionPullRequestStatus, session: String) throws -> SessionPullRequestUpdate {
        guard payload.sessionId == session, payload.cwd.hasPrefix("/"),
              PluginCommand.safeText(payload.cwd, limit: 4096) else { throw ClientError.invalidPayload }
        let branch = payload.branch.flatMap { $0.isEmpty ? nil : $0 }
        if let branch { guard PluginCommand.safeText(branch, limit: 4096) else { throw ClientError.invalidPayload } }
        guard let pull = payload.pullRequest else {
            return SessionPullRequestUpdate(sessionID: session, cwd: payload.cwd, branch: branch, pullRequest: nil)
        }
        guard branch != nil, let url = PullRequestLink.destination(number: pull.number, url: pull.url),
              let state = PullRequestStatus.State(rawValue: pull.state.rawValue) else { throw ClientError.invalidPayload }
        let status = PullRequestStatus(number: pull.number, url: url, state: state, draft: pull.draft, checks: try checks(pull.checks))
        return SessionPullRequestUpdate(sessionID: session, cwd: payload.cwd, branch: branch, pullRequest: status)
    }

    private static func checks(_ value: Components.Schemas.PullRequestChecks) throws -> PullRequestChecks {
        let wire = value.counts
        let counts = PullRequestChecks.Counts(passed: wire.passed, failed: wire.failed, pending: wire.pending,
                                              skipped: wire.skipped, canceled: wire.canceled, unknown: wire.unknown)
        let all = [counts.passed, counts.failed, counts.pending, counts.skipped, counts.canceled, counts.unknown]
        guard all.allSatisfy({ (0...10_000).contains($0) }), counts.total <= 10_000,
              value.items.count <= PullRequestChecks.maxListed, value.omitted >= 0,
              value.items.count + value.omitted == counts.total,
              value.omitted == 0 || value.items.count == PullRequestChecks.maxListed,
              let state = PullRequestChecks.State(rawValue: value.state.rawValue),
              let required = PullRequestChecks.Requirement(rawValue: value.required.rawValue),
              state == overall(counts) else { throw ClientError.invalidPayload }
        var listed = PullRequestChecks.Counts()
        let items = try value.items.map { item -> PullRequestChecks.Check in
            let workflow = item.workflow.flatMap { $0.isEmpty ? nil : $0 }
            guard !item.name.isEmpty, PluginCommand.safeText(item.name, limit: PullRequestChecks.maxTextBytes),
                  workflow.map({ PluginCommand.safeText($0, limit: PullRequestChecks.maxTextBytes) }) ?? true,
                  let state = PullRequestChecks.CheckState(rawValue: item.state) else { throw ClientError.invalidPayload }
            switch state {
            case .passed: listed.passed += 1
            case .failed: listed.failed += 1
            case .pending: listed.pending += 1
            case .skipped: listed.skipped += 1
            case .canceled: listed.canceled += 1
            case .unknown: listed.unknown += 1
            }
            return PullRequestChecks.Check(name: item.name, workflow: workflow, state: state, required: item.required)
        }
        guard listed.passed <= counts.passed, listed.failed <= counts.failed, listed.pending <= counts.pending,
              listed.skipped <= counts.skipped, listed.canceled <= counts.canceled, listed.unknown <= counts.unknown
        else { throw ClientError.invalidPayload }
        return PullRequestChecks(state: state, required: required, counts: counts, items: items, omitted: value.omitted)
    }

    private static func overall(_ counts: PullRequestChecks.Counts) -> PullRequestChecks.State {
        if counts.failed > 0 || counts.canceled > 0 { return .failing }
        if counts.pending > 0 { return .pending }
        if counts.passed > 0 { return .passing }
        return .none
    }
}
