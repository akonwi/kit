import Foundation

/// A session as a server lists it in the attention order, with what it's
/// doing. See ADR 0061.
public struct ListedSession: Sendable, Equatable {
    public let session: SessionExcerpt
    public let status: SessionTabStatus
    /// When its oldest pending interaction was created, while it awaits a
    /// response.
    public let waitingSince: Date?

    public init(session: SessionExcerpt, status: SessionTabStatus, waitingSince: Date? = nil) {
        self.session = session
        self.status = status
        self.waitingSince = waitingSince
    }
}

/// The attention order of `contract.CompareAttention`: sessions awaiting a
/// response first, the one waiting longest first; then running sessions;
/// then idle ones, each most recently updated first. Ties fall back to
/// ascending session ID. Clients that merge lists from several servers place
/// sessions with it.
public enum AttentionOrder {
    /// What a session is ordered by.
    public struct Key: Sendable, Equatable {
        public let status: SessionTabStatus
        /// When it started waiting. An older server doesn't say, so a waiting
        /// session without it is placed by `updated` instead.
        public let waitingSince: Date?
        public let updated: Date?
        public let id: String

        public init(status: SessionTabStatus, waitingSince: Date? = nil, updated: Date?, id: String) {
            self.status = status
            self.waitingSince = waitingSince
            self.updated = updated
            self.id = id
        }
    }

    /// Whether `a` comes before `b`.
    public static func precedes(_ a: Key, _ b: Key) -> Bool {
        if rank(a.status) != rank(b.status) { return rank(a.status) < rank(b.status) }
        if a.status == .awaitingResponse {
            let first = a.waitingSince ?? a.updated ?? .distantPast
            let second = b.waitingSince ?? b.updated ?? .distantPast
            if first != second { return first < second }
        } else {
            let first = a.updated ?? .distantPast, second = b.updated ?? .distantPast
            if first != second { return first > second }
        }
        return a.id < b.id
    }

    /// The group a status sorts in: 0 awaiting a response, 1 running, 2 idle.
    public static func rank(_ status: SessionTabStatus) -> Int {
        switch status {
        case .awaitingResponse: 0
        case .running: 1
        case .idle: 2
        }
    }
}

/// A server's sessions, listed with the attention order asked for.
public enum AttentionListing: Sendable, Equatable {
    /// In the attention order, each with what it's doing.
    case ordered([ListedSession])
    /// From a server that predates the order, which ignores it: in its own
    /// order and without what they're doing. Clients ask for the activity
    /// separately, as before, and order the sessions themselves.
    case unordered([SessionExcerpt])
}

/// A server that lists its sessions in the attention order, each with what
/// it's doing, in one request.
public protocol SessionAttentionClient: SessionClient {
    /// The server's sessions, asked for in the attention order.
    func sessionsByAttention() async throws -> AttentionListing
}

extension ServerClient: SessionAttentionClient {
    public func sessionsByAttention() async throws -> AttentionListing {
        try await resolve().sessionsByAttention()
    }
}
