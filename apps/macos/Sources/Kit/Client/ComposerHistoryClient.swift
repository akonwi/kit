import Foundation

struct WireMessageHistoryPage: Decodable {
    let sessionID: String
    let messages: [WireTranscriptMessage]?
    let nextCursor: String?
    let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case sessionID = "SessionID"
        case messages = "Messages"
        case nextCursor
        case hasMore = "HasMore"
    }
}

struct ComposerMessageHistoryEntry: Sendable, Equatable, Identifiable {
    let id: String
    let text: String
}

struct ComposerMessageHistoryPage: Sendable, Equatable {
    let entries: [ComposerMessageHistoryEntry]
    let nextCursor: String?
    let hasMore: Bool
}

struct ComposerBashHistoryEntry: Sendable, Equatable, Identifiable {
    let id: String
    let sequence: Int64
    let command: String
    let excluded: Bool
}

struct ComposerBashHistoryPage: Sendable, Equatable {
    let entries: [ComposerBashHistoryEntry]
    let nextCursor: String?
    let hasMore: Bool

    init(entries: [ComposerBashHistoryEntry], nextCursor: String?, hasMore: Bool) {
        self.entries = entries; self.nextCursor = nextCursor; self.hasMore = hasMore
    }

    init(_ page: WireBashHistoryPage, session: String, before: String?) throws {
        let source = page.entries ?? []
        guard page.sessionId == session, source.count <= 200,
              source.allSatisfy({ validHistoryIdentifier($0.id, prefix: "bash_") && $0.sequence >= 0 &&
                  !$0.command.isEmpty && $0.command.utf8.count <= 64 * 1024 && !$0.command.contains("\0") &&
                  validHistoryDate($0.startedAt) && ($0.completedAt == nil || validHistoryDate($0.completedAt!)) }),
              Set(source.map(\.id)).count == source.count,
              zip(source, source.dropFirst()).allSatisfy({ $0.sequence > $1.sequence }) else {
            throw ClientError.invalidPayload
        }
        if let before, let boundary = UInt64(before) {
            guard source.allSatisfy({ UInt64($0.sequence) < boundary }) else { throw ClientError.invalidPayload }
        }
        if page.hasMore {
            guard let last = source.last, let cursor = page.nextCursor.flatMap(UInt64.init), cursor > 0,
                  cursor == UInt64(last.sequence),
                  before.flatMap(UInt64.init).map({ cursor < $0 }) ?? true else { throw ClientError.invalidPayload }
        } else if page.nextCursor != nil && page.nextCursor != "" { throw ClientError.invalidPayload }
        entries = source.map { .init(id: $0.id, sequence: $0.sequence, command: $0.command,
                                     excluded: $0.excludeFromContext == true) }
        nextCursor = page.nextCursor; hasMore = page.hasMore
    }
}

extension WireMessageHistoryPage {
    func validatedMessages(session: String, before: String?) throws -> [WireTranscriptMessage] {
        let source = messages ?? []
        guard sessionID == session, source.count <= 100,
              source.allSatisfy({ !$0.id.isEmpty && !$0.turnId.isEmpty &&
                  $0.role == "user" && $0.sequence >= 0 && validHistoryDate($0.createdAt) &&
                  validUserHistoryMessage($0) }),
              Set(source.map(\.id)).count == source.count,
              zip(source, source.dropFirst()).allSatisfy({ $0.sequence > $1.sequence }) else {
            throw ClientError.invalidPayload
        }
        if let before, let boundary = UInt64(before) {
            guard source.allSatisfy({ UInt64($0.sequence) < boundary }) else { throw ClientError.invalidPayload }
        }
        if hasMore {
            guard let last = source.last, let cursor = nextCursor.flatMap(UInt64.init), cursor > 0,
                  cursor == UInt64(last.sequence),
                  before.flatMap(UInt64.init).map({ cursor < $0 }) ?? true else { throw ClientError.invalidPayload }
        } else if nextCursor != nil && nextCursor != "" { throw ClientError.invalidPayload }
        return source
    }
}

private func validHistoryIdentifier(_ value: String, prefix: String) -> Bool {
    guard value.hasPrefix(prefix) else { return false }
    let suffix = value.dropFirst(prefix.count)
    return suffix.utf8.count == 32 && suffix.utf8.allSatisfy {
        ($0 >= 48 && $0 <= 57) || ($0 >= 97 && $0 <= 102) || ($0 >= 65 && $0 <= 70)
    }
}

private func validUserHistoryMessage(_ message: WireTranscriptMessage) -> Bool {
    func empty(_ value: String?) -> Bool { value == nil || value == "" }
    guard empty(message.stopReason), empty(message.errorMessage), empty(message.toolCallId), empty(message.toolName),
          empty(message.boundaryId), empty(message.boundaryKind), empty(message.boundarySource),
          message.details == nil, message.isError != true else { return false }
    return (message.content ?? []).allSatisfy { block in
        switch block.kind.rawValue {
        case "text":
            return block.text?.isEmpty == false && empty(block.toolCallId) && empty(block.toolName) &&
                empty(block.arguments) && block.argumentsTruncated != true && empty(block.filename) &&
                empty(block.mediaType) && (block.annotations?.isEmpty ?? true)
        case "image":
            return empty(block.text) && empty(block.toolCallId) && empty(block.toolName) &&
                empty(block.arguments) && block.argumentsTruncated != true && (block.annotations?.isEmpty ?? true) &&
                validHistoryMediaType(block.mediaType, imageOnly: true)
        case "file":
            return empty(block.text) && empty(block.toolCallId) && empty(block.toolName) &&
                empty(block.arguments) && block.argumentsTruncated != true && (block.annotations?.isEmpty ?? true) &&
                block.filename?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false &&
                validHistoryMediaType(block.mediaType, imageOnly: false)
        case "annotations":
            guard empty(block.text), empty(block.toolCallId), empty(block.toolName), empty(block.arguments),
                  block.argumentsTruncated != true, empty(block.filename), empty(block.mediaType),
                  let annotations = block.annotations, !annotations.isEmpty, annotations.count <= 64 else { return false }
            return (try? annotations.forEach { _ = try FileAnnotation($0) }) != nil
        default:
            return false
        }
    }
}

private func validHistoryMediaType(_ raw: String?, imageOnly: Bool) -> Bool {
    guard let raw, let segments = splitMediaType(raw) else { return false }
    let type = segments[0].trimmingCharacters(in: .whitespacesAndNewlines)
    let parts = type.split(separator: "/", omittingEmptySubsequences: false)
    guard parts.count == 2, parts.allSatisfy({ validMediaToken(String($0)) }),
          parts[0] != "*", parts[1] != "*", !imageOnly || parts[0].lowercased() == "image" else { return false }

    var names = Set<String>()
    for rawParameter in segments.dropFirst() {
        let parameter = rawParameter.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let equals = parameter.firstIndex(of: "=") else { return false }
        let name = String(parameter[..<equals]).trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        let value = String(parameter[parameter.index(after: equals)...]).trimmingCharacters(in: .whitespacesAndNewlines)
        guard validMediaToken(name), validMediaParameterValue(value), names.insert(name).inserted else { return false }
    }
    return true
}

private func splitMediaType(_ raw: String) -> [String]? {
    var segments = [""]
    var quoted = false
    var escaped = false
    for scalar in raw.unicodeScalars {
        if scalar.value == 127 || scalar.value < 32 && scalar != "\t" { return nil }
        if escaped {
            segments[segments.count - 1].unicodeScalars.append(scalar)
            escaped = false
        } else if quoted && scalar == "\\" {
            segments[segments.count - 1].unicodeScalars.append(scalar)
            escaped = true
        } else if scalar == "\"" {
            quoted.toggle()
            segments[segments.count - 1].unicodeScalars.append(scalar)
        } else if scalar == ";" && !quoted {
            segments.append("")
        } else {
            segments[segments.count - 1].unicodeScalars.append(scalar)
        }
    }
    return quoted || escaped ? nil : segments
}

private func validMediaToken(_ value: String) -> Bool {
    let punctuation = Set("!#$%&'*+-.^_`|~".utf8)
    return !value.isEmpty && value.utf8.allSatisfy {
        ($0 >= 48 && $0 <= 57) || ($0 >= 65 && $0 <= 90) || ($0 >= 97 && $0 <= 122) || punctuation.contains($0)
    }
}

private func validMediaParameterValue(_ value: String) -> Bool {
    if validMediaToken(value) { return true }
    guard value.count >= 2, value.first == "\"", value.last == "\"" else { return false }
    var escaped = false
    for scalar in value.dropFirst().dropLast().unicodeScalars {
        if scalar.value < 32 || scalar.value == 127 { return false }
        if escaped { escaped = false; continue }
        if scalar == "\\" { escaped = true; continue }
        if scalar == "\"" { return false }
    }
    return !escaped
}

private func validHistoryDate(_ value: String) -> Bool {
    let parser = ISO8601DateFormatter()
    if parser.date(from: value) != nil { return true }
    parser.formatOptions.insert(.withFractionalSeconds)
    return parser.date(from: value) != nil
}

protocol ComposerHistoryClient: SessionClient {
    func messageHistory(_ session: String, before: String?) async throws -> ComposerMessageHistoryPage
    func bashHistory(_ session: String, before: String?, limit: Int) async throws -> ComposerBashHistoryPage
}

extension LocalClient: ComposerHistoryClient {
    func messageHistory(_ session: String, before: String?) async throws -> ComposerMessageHistoryPage {
        try await HTTPClient.local().messageHistory(session, before: before)
    }
    func bashHistory(_ session: String, before: String?, limit: Int) async throws -> ComposerBashHistoryPage {
        try await HTTPClient.local().bashHistory(session, before: before, limit: limit)
    }
}
