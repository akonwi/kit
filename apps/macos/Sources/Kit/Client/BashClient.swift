import Foundation

protocol BashClient: SessionClient {
    func startBash(_ session: String, input: WireBashExecutionInput) async throws -> BashExecution
    func bash(_ session: String, id: String) async throws -> BashExecution
    func abortBash(_ session: String, id: String) async throws
}

extension LocalClient: BashClient {
    func startBash(_ session: String, input: WireBashExecutionInput) async throws -> BashExecution {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.startBash(session, input: input)
    }
    func bash(_ session: String, id: String) async throws -> BashExecution { try await HTTPClient.local().bash(session, id: id) }
    func abortBash(_ session: String, id: String) async throws { try await HTTPClient.local().abortBash(session, id: id) }
}

struct BashExecution: Decodable, Sendable, Equatable, Identifiable {
    let id: String
    let command: String
    let status: String
    let output: String
    let exitCode: Int?
    let excluded: Bool
    let truncated: Bool
    let timedOut: Bool
    let error: String?
    var running: Bool { status == "running" }
    var statusLabel: String {
        if running { return "Running" }
        if timedOut { return "Timed out" }
        if status == "completed", let exitCode { return exitCode == 0 ? "Completed" : "Exit \(exitCode)" }
        return status.capitalized
    }
    init(_ wire: WireBashExecution, session: String) throws {
        guard wire.sessionId == session, wire.id.hasPrefix("bash_"), wire.sequence >= 0,
              !wire.command.isEmpty, wire.command.utf8.count <= 64 * 1024,
              !wire.command.contains("\0"), (wire.output?.utf8.count ?? 0) <= 64 * 1024,
              Self.validDate(wire.startedAt) else { throw ClientError.invalidPayload }
        if wire.status.rawValue == "running" {
            guard wire.exitCode == nil, wire.completedAt == nil, (wire.output ?? "").isEmpty,
                  wire.truncated != true, wire.timedOut != true, (wire.errorMessage ?? "").isEmpty else { throw ClientError.invalidPayload }
        } else {
            guard let date = wire.completedAt, Self.validDate(date) else { throw ClientError.invalidPayload }
            if wire.status.rawValue == "completed" {
                guard (wire.errorMessage ?? "").isEmpty else { throw ClientError.invalidPayload }
            } else {
                guard !(wire.errorMessage ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw ClientError.invalidPayload }
            }
        }
        id = wire.id; command = wire.command; status = wire.status.rawValue; output = wire.output ?? ""
        exitCode = wire.exitCode; excluded = wire.excludeFromContext == true
        truncated = wire.truncated == true; timedOut = wire.timedOut == true; error = wire.errorMessage
    }
    private static func validDate(_ value: String) -> Bool {
        let parser = ISO8601DateFormatter()
        if parser.date(from: value) != nil { return true }
        parser.formatOptions.insert(.withFractionalSeconds)
        return parser.date(from: value) != nil
    }
    var message: TranscriptMessage { .init(id: id, role: "bash", text: "", tools: [], bash: self) }
}

struct BashDraft: Equatable {
    let command: String
    let excluded: Bool
    init?(_ text: String) {
        guard text.hasPrefix("!") else { return nil }
        excluded = text.hasPrefix("!!")
        command = String(text.dropFirst(excluded ? 2 : 1)).trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

extension BashExecution {
    static func boundary(_ message: WireTranscriptMessage) throws -> BashExecution? {
        guard message.boundaryKind == "bash", let details = message.details, let id = message.boundaryId else { return nil }
        return try boundary(id: id, details: details, content: message.content ?? [], sequence: message.sequence)
    }
    static func boundary(id: String, details: WireJSON, content: [WireTranscriptContent], sequence: Int64 = 0) throws -> BashExecution {
        struct Details: Decodable {
            let version: Int
            let command: String
            let status: WireBashExecutionStatus
            let exitCode: Int?
            let truncated: Bool?
            let timedOut: Bool?
            let errorMessage: String?
            let startedAt: String
            let completedAt: String
        }
        let value = try JSONDecoder().decode(Details.self, from: JSONEncoder().encode(details))
        guard value.version == 1 else { throw ClientError.invalidPayload }
        var output = SessionProjection.visibleText(content)
        let prefix = "[bash command: " + value.command + "]" + (value.exitCode.map { $0 == 0 ? "" : " (exit code: \($0))" } ?? "")
        if output.hasPrefix(prefix) { output = String(output.dropFirst(prefix.count)).trimmingCharacters(in: .newlines) }
        let notices = [value.timedOut == true ? "[timed out]" : nil,
                       value.truncated == true ? "[output truncated]" : nil,
                       value.errorMessage.map { "[" + value.status.rawValue + ": " + $0 + "]" }].compactMap { $0 }
        for notice in notices.reversed() where output.hasSuffix(notice) {
            output = String(output.dropLast(notice.count)).trimmingCharacters(in: .newlines)
        }
        return try BashExecution(.init(id: id, sessionId: "boundary", sequence: sequence, command: value.command,
            status: value.status, output: output, exitCode: value.exitCode, excludeFromContext: false,
            truncated: value.truncated, timedOut: value.timedOut, errorMessage: value.errorMessage,
            startedAt: value.startedAt, completedAt: value.completedAt), session: "boundary")
    }
}
