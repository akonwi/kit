import Foundation

struct ScratchpadRecord: Codable, Equatable, Sendable {
    let owner: String
    let content: String
    let revision: Int64
    let updatedAt: String

    init(owner: String, content: String, revision: Int64, updatedAt: String) {
        self.owner = owner
        self.content = content
        self.revision = revision
        self.updatedAt = updatedAt
    }

    init(_ wire: WireScratchpad) throws {
        guard !wire.ownerSessionId.isEmpty,
              let revision = Int64(wire.revision), revision > 0,
              String(revision) == wire.revision,
              Self.validContent(wire.content),
              wire.updatedAt.hasSuffix("Z") else { throw ClientError.invalidPayload }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard formatter.date(from: wire.updatedAt) != nil || ISO8601DateFormatter().date(from: wire.updatedAt) != nil else {
            throw ClientError.invalidPayload
        }
        owner = wire.ownerSessionId
        content = wire.content
        self.revision = revision
        updatedAt = wire.updatedAt
    }

    static func validContent(_ content: String) -> Bool {
        content.utf8.count <= 64 * 1024 && !content.unicodeScalars.contains { scalar in
            if scalar == "\n" || scalar == "\t" { return false }
            return scalar.properties.generalCategory == .control || scalar.properties.generalCategory == .format
        }
    }
}

enum ScratchpadFailure: LocalizedError, Equatable {
    case conflict(ScratchpadRecord)
    case rejected(String)
    var errorDescription: String? {
        switch self {
        case .conflict: "Shared scratchpad changed"
        case .rejected(let message): message
        }
    }
}

protocol ScratchpadClient: SessionClient {
    func scratchpad(session: String) async throws -> ScratchpadRecord
    func updateScratchpad(session: String, content: String, expectedRevision: Int64) async throws -> ScratchpadRecord
}
