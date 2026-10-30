import Foundation

public struct ScratchpadRecord: Codable, Equatable, Sendable {
    public let owner: String
    public let content: String
    public let revision: Int64
    public let updatedAt: String

    public init(owner: String, content: String, revision: Int64, updatedAt: String) {
        self.owner = owner
        self.content = content
        self.revision = revision
        self.updatedAt = updatedAt
    }

    public init(_ wire: WireScratchpad) throws {
        try self.init(ownerSessionId: wire.ownerSessionId, content: wire.content, revision: wire.revision, updatedAt: wire.updatedAt)
    }

    public init(_ wire: Components.Schemas.Scratchpad) throws {
        try self.init(ownerSessionId: wire.ownerSessionId, content: wire.content, revision: wire.revision, updatedAt: wire.updatedAt)
    }

    private init(ownerSessionId: String, content: String, revision encodedRevision: String, updatedAt: String) throws {
        guard !ownerSessionId.isEmpty,
              let revision = Int64(encodedRevision), revision > 0,
              String(revision) == encodedRevision,
              Self.validContent(content),
              updatedAt.hasSuffix("Z") else { throw ClientError.invalidPayload }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard formatter.date(from: updatedAt) != nil || ISO8601DateFormatter().date(from: updatedAt) != nil else {
            throw ClientError.invalidPayload
        }
        owner = ownerSessionId
        self.content = content
        self.revision = revision
        self.updatedAt = updatedAt
    }

    public static func validContent(_ content: String) -> Bool {
        content.utf8.count <= 64 * 1024 && !content.unicodeScalars.contains { scalar in
            if scalar == "\n" || scalar == "\t" { return false }
            return scalar.properties.generalCategory == .control || scalar.properties.generalCategory == .format
        }
    }
}

public enum ScratchpadFailure: LocalizedError, Equatable {
    case conflict(ScratchpadRecord)
    case rejected(String)
    public var errorDescription: String? {
        switch self {
        case .conflict: "Shared scratchpad changed"
        case .rejected(let message): message
        }
    }
}

public protocol ScratchpadClient: SessionClient {
    func scratchpad(session: String) async throws -> ScratchpadRecord
    func updateScratchpad(session: String, content: String, expectedRevision: Int64) async throws -> ScratchpadRecord
}
