import Foundation
import KitClient
import Observation

/// One session as this screen shows it: the live snapshot the server
/// streams, with older pages the reader loaded joined in front.
@MainActor @Observable
final class SessionFeed {
    enum Connection: Equatable {
        /// Waiting for the first snapshot.
        case connecting
        case live
        /// The stream dropped; the transcript stays as it was.
        case reconnecting(since: Date)
        /// The session can't be shown, and retrying won't help.
        case unavailable(String)
    }

    let identity: SessionIdentity
    private let client: any TranscriptPagingClient
    private(set) var session: SessionExcerpt?
    private(set) var connection = Connection.connecting
    private(set) var historyLoading = false
    private(set) var historyError: String?
    /// Called with each snapshot as shown, after history is joined to it.
    @ObservationIgnored var onSnapshot: (@MainActor (SessionExcerpt) -> Void)?

    /// Older pages loaded in front of the snapshot, oldest first.
    private var historyPrefix: [TranscriptMessage] = []
    /// The first history sequence of the snapshot the prefix joins.
    private var historyBoundary: Int64?
    /// The cursor for the page before everything shown.
    private var historyCursor: String?
    private var historyGeneration = UUID()
    /// Set when the server no longer knew a history cursor, so the next
    /// snapshot starts the transcript afresh.
    private var resetHistoryOnReceive = false
    private var lastSnapshot: SessionExcerpt?
    /// Whether the current connection has delivered a snapshot.
    private var delivered = false

    /// Delays between reconnection attempts, growing to the last.
    static let reconnectDelays: [Duration] = [.seconds(1), .seconds(2), .seconds(4), .seconds(8), .seconds(15)]

    init(identity: SessionIdentity, client: any TranscriptPagingClient) {
        self.identity = identity
        self.client = client
    }

    // MARK: Watching

    /// Watches the session until cancelled, reconnecting after transient
    /// failures. Each connection starts from a fresh snapshot.
    func run() async {
        var attempt = 0
        while !Task.isCancelled {
            delivered = false
            do {
                try await client.watch(identity.session) { [weak self] snapshot in
                    await self?.receive(snapshot)
                }
                // The stream ended without an error; connect again.
            } catch {
                if isCancellation(error) { return }
                if let reason = Self.terminal(error) {
                    connection = .unavailable(reason)
                    return
                }
            }
            if Task.isCancelled { return }
            if delivered { attempt = 0 }
            if case .live = connection { connection = .reconnecting(since: Date()) }
            if session == nil { connection = .reconnecting(since: Date()) }
            let delay = Self.reconnectDelays[min(attempt, Self.reconnectDelays.count - 1)]
            attempt += 1
            do { try await Task.sleep(for: delay) } catch { return }
        }
    }

    /// Why an error means retrying can't help, or nil when it can.
    static func terminal(_ error: any Error) -> String? {
        switch error {
        case ClientError.missingSession, ClientError.http(404), ClientError.http(410):
            return "This session is no longer on the server."
        case ClientError.needsPairing:
            return "This iPhone is no longer paired with the server. Pair again from Servers."
        case ClientError.incompatibleDaemon, ClientError.incompatible:
            return (error as? LocalizedError)?.errorDescription ?? "The server and this app don’t match."
        default:
            return nil
        }
    }

    /// Accepts a snapshot, keeping loaded history that still joins it.
    func receive(_ snapshot: SessionExcerpt) {
        guard snapshot.id == identity.session else { return }
        lastSnapshot = snapshot
        delivered = true
        if resetHistoryOnReceive || historyBoundary != snapshot.historyStart {
            historyGeneration = UUID()
            historyLoading = false
            // A fresh bounded snapshot can move the recent-history boundary.
            // Keep only the prefix joined to it by a message both hold.
            if !resetHistoryOnReceive, snapshot.historyCursor != nil, let first = snapshot.messages.first,
               let index = session?.messages.firstIndex(where: { $0.id == first.id }) {
                historyPrefix = Array(session!.messages.prefix(index))
            } else {
                historyPrefix = []
                historyCursor = snapshot.historyCursor
            }
            resetHistoryOnReceive = false
            historyBoundary = snapshot.historyStart
            historyError = nil
        }
        var merged = snapshot
        merged.messages = historyPrefix + snapshot.messages
        merged.historyCursor = historyCursor
        session = merged
        connection = .live
        onSnapshot?(merged)
    }

    /// Whether the session can't be shown at all.
    var unavailable: Bool {
        if case .unavailable = connection { true } else { false }
    }

    // MARK: History

    /// Whether older messages can be loaded.
    var hasEarlier: Bool { session?.historyCursor != nil }

    /// Loads the page before the earliest message shown.
    func loadEarlier() async {
        guard !historyLoading, let cursor = session?.historyCursor else { return }
        let generation = historyGeneration
        historyLoading = true
        historyError = nil
        do {
            let page = try await client.history(identity.session, before: cursor)
            guard historyGeneration == generation, session?.historyCursor == cursor else { return }
            guard page.sessionID == identity.session else { throw ClientError.invalidPayload }
            let shown = Set(session?.messages.map(\.id) ?? [])
            guard page.messages.allSatisfy({ !shown.contains($0.id) }) else { throw ClientError.invalidPayload }
            historyPrefix = page.messages + historyPrefix
            historyCursor = page.previousCursor
            session?.messages.insert(contentsOf: page.messages, at: 0)
            session?.historyCursor = page.previousCursor
            historyLoading = false
        } catch {
            guard historyGeneration == generation else { return }
            historyLoading = false
            if case ClientError.http(409) = error {
                // The server no longer knows this cursor. Start again from
                // the snapshot rather than mixing pages.
                historyPrefix = []
                historyBoundary = nil
                resetHistoryOnReceive = true
                if let lastSnapshot { receive(lastSnapshot) }
                historyError = "History changed on the server, so the transcript starts from the recent messages again."
            } else if !isCancellation(error) {
                historyError = error.localizedDescription
            }
        }
    }
}
