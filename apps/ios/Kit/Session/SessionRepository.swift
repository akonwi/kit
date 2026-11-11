import Foundation
import KitClient
import Observation

/// The session's repository as one `vcs.status` record reports it.
struct RepositoryHead: Equatable {
    enum Head: Equatable {
        case branch(String)
        /// A branch with no commits yet.
        case unborn(String)
        /// A commit, by its full object ID.
        case detached(String)
    }

    let cwd: String
    let head: Head
    let dirty: Bool
    /// The branch's pull request, when it has one.
    let pullRequest: PullRequest?

    struct PullRequest: Equatable {
        let number: Int
        let url: URL
    }

    /// The repository a record reports, or nil outside one.
    init?(_ record: WireSessionVCSStatus) {
        guard let status = record.status else { return nil }
        let head = status.head
        switch head.kind {
        case .value0:
            guard let name = head.name, !name.isEmpty else { return nil }
            self.head = .branch(name)
        case .value2:
            guard let name = head.name, !name.isEmpty else { return nil }
            self.head = .unborn(name)
        case .value1:
            guard let oid = head.oid, !oid.isEmpty else { return nil }
            self.head = .detached(oid)
        }
        cwd = record.cwd
        dirty = status.dirty
        // A pull request belongs only to a named branch.
        if case .branch = self.head, let pull = status.pullRequest,
           let url = PullRequestLink.destination(number: pull.number, url: pull.url) {
            pullRequest = PullRequest(number: pull.number, url: url)
        } else {
            pullRequest = nil
        }
    }

    init(cwd: String, head: Head, dirty: Bool, pullRequest: PullRequest? = nil) {
        self.cwd = cwd
        self.head = head
        self.dirty = dirty
        self.pullRequest = pullRequest
    }
}

/// The session's repository as the status strip shows it: the branch, its
/// pull request, and the pull request's checks.
struct RepositoryStatus: Equatable {
    /// The branch, or "detached" and a short commit, with * when there are
    /// uncommitted changes.
    let branch: String
    let pullRequest: PullRequest?

    struct PullRequest: Equatable {
        let number: Int
        let url: URL
        /// Nil until the pull request stream reports checks, or when there
        /// are none.
        let checks: ChecksPresentation?
    }

    /// The strip for a repository. `update` adds checks when it is for the
    /// same folder, branch, and pull request.
    init(_ repository: RepositoryHead, pullRequest update: SessionPullRequestUpdate?) {
        let label: String
        var branchName: String?
        switch repository.head {
        case .branch(let name): label = name; branchName = name
        case .unborn(let name): label = name
        case .detached(let oid): label = "detached \(oid.prefix(8))"
        }
        branch = label + (repository.dirty ? "*" : "")
        guard let pull = repository.pullRequest else {
            pullRequest = nil
            return
        }
        var checks: ChecksPresentation?
        if let update, update.cwd == repository.cwd, update.branch == branchName,
           let reported = update.pullRequest, reported.number == pull.number {
            checks = ChecksPresentation(reported)
        }
        pullRequest = PullRequest(number: pull.number, url: pull.url, checks: checks)
    }
}

/// What the checks chip and sheet show for a pull request's checks.
struct ChecksPresentation: Equatable {
    enum Tone: Equatable { case success, danger, warning, muted }

    struct Row: Equatable {
        let name: String
        let workflow: String?
        let symbol: String
        let tone: Tone
        let required: Bool
    }

    let number: Int
    let symbol: String
    let tone: Tone
    /// Passed checks of those that ran or will run, such as "10/12".
    let count: String
    /// Counts by state, most urgent first, such as "2 failing, 10 passed".
    let summary: String
    /// The required checks' state, when GitHub reported it.
    let requirement: String?
    /// The listed checks, most urgent first, as the server sends them.
    let rows: [Row]
    /// Checks the server counted but didn't list.
    let omitted: Int
    /// The pull request's checks on GitHub.
    let url: URL

    /// Nil when the pull request has no checks.
    init?(_ status: PullRequestStatus) {
        let checks = status.checks
        switch checks.state {
        case .none: return nil
        case .passing: (symbol, tone) = ("checkmark.circle", .success)
        case .failing: (symbol, tone) = ("xmark.circle", .danger)
        case .pending: (symbol, tone) = ("clock", .warning)
        }
        let counts = checks.counts
        let parts: [(Int, String)] = [
            (counts.failed, "failing"), (counts.canceled, "canceled"), (counts.pending, "running"),
            (counts.passed, "passed"), (counts.skipped, "skipped"), (counts.unknown, "unknown"),
        ]
        number = status.number
        count = "\(counts.passed)/\(counts.total - counts.skipped)"
        summary = parts.filter { $0.0 > 0 }.map { "\($0.0) \($0.1)" }.joined(separator: ", ")
        requirement = switch checks.required {
        case .unknown: nil
        case .pending: "Required checks are running"
        case .satisfied: "Required checks passed"
        case .unsatisfied: "Required checks aren’t met"
        }
        rows = checks.items.map { check in
            let (symbol, tone) = Self.glyph(check.state)
            return Row(name: check.name, workflow: check.workflow, symbol: symbol, tone: tone, required: check.required)
        }
        omitted = checks.omitted
        url = status.url.appendingPathComponent("checks")
    }

    static func glyph(_ state: PullRequestChecks.CheckState) -> (String, Tone) {
        switch state {
        case .passed: ("checkmark.circle", .success)
        case .failed: ("xmark.circle", .danger)
        case .pending: ("clock", .warning)
        case .canceled: ("nosign", .muted)
        case .skipped: ("minus.circle", .muted)
        case .unknown: ("questionmark.circle", .muted)
        }
    }

    /// One line for VoiceOver.
    var spokenSummary: String {
        ["Checks: " + summary, requirement].compactMap { $0 }.joined(separator: ". ")
    }
}

/// Keeps the session's repository status current while its screen shows:
/// the repository stream always, and the pull request stream only while the
/// branch has a pull request. The last status stays while a stream
/// reconnects.
@MainActor @Observable
final class SessionRepository {
    let session: String
    @ObservationIgnored private let client: any SessionVCSClient
    /// The repository, or nil outside one or before the server reports.
    private(set) var repository: RepositoryHead?
    private(set) var pullRequest: SessionPullRequestUpdate?

    /// What the strip shows, or nil outside a repository.
    var status: RepositoryStatus? { repository.map { RepositoryStatus($0, pullRequest: pullRequest) } }

    /// The current watch, or nil while the screen isn't showing.
    @ObservationIgnored private var watching: UUID?
    /// The current pull request stream. Each stream has its own, so one
    /// that was stopped can't deliver after another starts.
    @ObservationIgnored private var pullRequestStream: UUID?
    @ObservationIgnored private var pullRequestTask: Task<Void, Never>?
    /// Set when the server doesn't serve the pull request stream; cleared
    /// when watching starts again.
    @ObservationIgnored private var pullRequestNotServed = false
    /// Delays between reconnection attempts, growing to the last.
    @ObservationIgnored var reconnectDelays: [Duration] = [.seconds(1), .seconds(2), .seconds(4), .seconds(8), .seconds(15)]

    init(session: String, client: any SessionVCSClient) {
        self.session = session
        self.client = client
    }

    /// Watches until the caller is cancelled. Deliveries stop as soon as it
    /// is; the status shown stays for when watching starts again.
    func watch() async {
        let watch = UUID()
        // A watch that is still ending hands over to this one; the checks
        // shown stay until the new stream reports.
        endPullRequestStream()
        pullRequestNotServed = false
        watching = watch
        let client = self.client, session = self.session
        await withTaskCancellationHandler {
            await follow { delivery in
                try await client.watchVCS(session) { [weak self] status in
                    await self?.receive(status, from: watch, delivery: delivery)
                }
            }
        } onCancel: {
            Task { @MainActor [weak self] in self?.end(watch) }
        }
        end(watch)
    }

    /// Accepts a repository status, and opens or closes the pull request
    /// stream to match it while watching.
    func apply(_ status: WireSessionVCSStatus) {
        guard status.sessionId == session else { return }
        let repository = RepositoryHead(status)
        if self.repository != repository { self.repository = repository }
        if repository?.pullRequest == nil { stopPullRequest() } else if watching != nil { startPullRequest() }
    }

    /// Accepts a pull request status for the session.
    func apply(_ update: SessionPullRequestUpdate) {
        guard update.sessionID == session, pullRequest != update else { return }
        pullRequest = update
    }

    private func receive(_ status: WireSessionVCSStatus, from watch: UUID, delivery: Delivery) {
        guard watching == watch, !Task.isCancelled else { return }
        delivery.happened = true
        apply(status)
    }

    private func receive(_ update: SessionPullRequestUpdate, from stream: UUID, delivery: Delivery) {
        guard pullRequestStream == stream, !Task.isCancelled else { return }
        delivery.happened = true
        apply(update)
    }

    private func end(_ watch: UUID) {
        guard watching == watch else { return }
        watching = nil
        endPullRequestStream()
    }

    private func startPullRequest() {
        guard pullRequestTask == nil, !pullRequestNotServed,
              let client = client as? any SessionPullRequestClient else { return }
        let stream = UUID(), session = self.session
        pullRequestStream = stream
        pullRequestTask = Task { [weak self] in
            await self?.follow { [weak self] delivery in
                do {
                    try await client.watchPullRequest(session) { [weak self] update in
                        await self?.receive(update, from: stream, delivery: delivery)
                    }
                } catch let error as PullRequestStreamNotServed {
                    // An older server: the strip shows no checks.
                    if self?.pullRequestStream == stream { self?.pullRequestNotServed = true }
                    throw error
                }
            }
        }
    }

    /// Closes the pull request stream, keeping the checks shown.
    private func endPullRequestStream() {
        pullRequestTask?.cancel()
        pullRequestTask = nil
        pullRequestStream = nil
    }

    /// Closes the pull request stream and forgets its checks.
    private func stopPullRequest() {
        endPullRequestStream()
        if pullRequest != nil { pullRequest = nil }
    }

    /// Whether one connection of a stream delivered anything.
    @MainActor private final class Delivery {
        var happened = false
    }

    /// Runs a stream until cancelled or until it fails in a way retrying
    /// can't repair, reconnecting after transient failures. A connection
    /// that delivered reconnects promptly; others back off.
    private func follow(_ stream: (Delivery) async throws -> Void) async {
        var attempt = 0
        while !Task.isCancelled {
            let delivery = Delivery()
            do {
                try await stream(delivery)
            } catch is CancellationError {
                return
            } catch where Self.terminal(error) {
                return
            } catch {}
            if Task.isCancelled { return }
            if delivery.happened { attempt = 0 }
            let delay = reconnectDelays[min(attempt, reconnectDelays.count - 1)]
            attempt += 1
            do { try await Task.sleep(for: delay) } catch { return }
        }
    }

    /// Whether reconnecting can't help: the session is gone, the device
    /// isn't paired, the server broke the protocol, or it doesn't serve the
    /// stream.
    nonisolated static func terminal(_ error: any Error) -> Bool {
        switch error {
        case ClientError.http(let code) where [401, 403, 404, 410].contains(code): true
        case ClientError.invalidPayload, ClientError.oversized, ClientError.incompatible,
             ClientError.incompatibleDaemon, ClientError.missingSession, ClientError.needsPairing: true
        case is DecodingError, is PullRequestStreamNotServed: true
        default: false
        }
    }
}
