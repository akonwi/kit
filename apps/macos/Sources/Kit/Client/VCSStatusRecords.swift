import Foundation
import OpenAPIRuntime

/// Repository-status payloads from the generated `vcs` operations, projected
/// onto the client's wire records after boundary validation.
enum VCSStatusRecords {
    /// The only record name `streamSessionVCS` declares.
    static let recordName = "vcs.status"

    /// Decodes one dispatched stream event. Comment-only dispatches, such as
    /// the initial comment and heartbeats, carry no fields and return nil.
    /// Any other event must be exactly one `vcs.status` record with a single
    /// data line and no id, whose payload passes strict decoding and the
    /// protocol's semantic rules.
    static func decode(_ event: ServerSentEvent, session: String) throws -> WireSessionVCSStatus? {
        if event.event == nil, event.data == nil, event.id == nil, event.retry == nil { return nil }
        guard event.event == recordName, event.id == nil, event.retry == nil,
              let data = event.data, !data.contains("\n") else { throw ClientError.invalidPayload }
        return try validated(payload(Data(data.utf8)), session: session)
    }

    /// Strictly decodes one payload with the generated schema type, which
    /// rejects unknown members; duplicate members are rejected beforehand
    /// because Foundation collapses them.
    static func payload(_ data: Data) throws -> Components.Schemas.SessionVCSStatus {
        guard StrictJSON.uniqueMembers(data) else { throw ClientError.invalidPayload }
        do { return try JSONDecoder().decode(Components.Schemas.SessionVCSStatus.self, from: data) }
        catch { throw ClientError.invalidPayload }
    }

    /// Applies the protocol's semantic rules to a streamed payload.
    static func validated(_ payload: Components.Schemas.SessionVCSStatus, session: String) throws -> WireSessionVCSStatus {
        guard payload.sessionId == session, safePath(payload.cwd) else { throw ClientError.invalidPayload }
        if let status = payload.status {
            guard safePath(status.root) else { throw ClientError.invalidPayload }
            let head = status.head
            switch head.kind {
            case .branch, .unborn:
                guard let name = head.name, !name.isEmpty, PluginCommand.safeText(name, limit: 4096),
                      head.oid == nil || head.oid == "" else { throw ClientError.invalidPayload }
            case .detached:
                guard head.name == nil || head.name == "", let oid = head.oid, [40, 64].contains(oid.utf8.count),
                      oid.allSatisfy({ $0.isASCII && $0.isHexDigit }) else { throw ClientError.invalidPayload }
            }
            if let pull = status.pullRequest {
                guard head.kind == .branch, PullRequestLink.destination(number: pull.number, url: pull.url) != nil
                else { throw ClientError.invalidPayload }
            }
        }
        return WireSessionVCSStatus(payload)
    }

    private static func safePath(_ value: String) -> Bool {
        value.hasPrefix("/") && PluginCommand.safeText(value, limit: 4096)
    }
}

extension WireSessionVCSStatus {
    /// Projects a generated repository-status payload without validation.
    init(_ payload: Components.Schemas.SessionVCSStatus) {
        var status: WireVCSStatus?
        if let value = payload.status {
            let kind: WireVCSHeadKind
            switch value.head.kind {
            case .branch: kind = .value0
            case .detached: kind = .value1
            case .unborn: kind = .value2
            }
            let head = WireVCSHead(kind: kind, name: value.head.name, oid: value.head.oid)
            let pull = value.pullRequest.map { WireGitHubPullRequest(number: $0.number, url: $0.url) }
            status = WireVCSStatus(pullRequest: pull, root: value.root, head: head, dirty: value.dirty)
        }
        self.init(sessionId: payload.sessionId, cwd: payload.cwd, status: status)
    }
}

/// Strict JSON checks Foundation's decoders do not perform.
enum StrictJSON {
    /// Rejects duplicate members in any object, nesting beyond 32 levels, and
    /// unbalanced objects. Foundation collapses duplicate keys silently.
    static func uniqueMembers(_ data: Data) -> Bool {
        let bytes = Array(data)
        var index = 0, objects: [Set<String>] = []
        while index < bytes.count {
            if bytes[index] == 123 {
                guard objects.count < 32 else { return false }
                objects.append([]); index += 1; continue
            }
            if bytes[index] == 125 {
                guard !objects.isEmpty else { return false }
                objects.removeLast(); index += 1; continue
            }
            guard bytes[index] == 34 else { index += 1; continue }
            let start = index
            index += 1
            while index < bytes.count && bytes[index] != 34 { index += bytes[index] == 92 ? 2 : 1 }
            guard index < bytes.count else { return false }
            index += 1
            let end = index
            while index < bytes.count && [9, 10, 13, 32].contains(bytes[index]) { index += 1 }
            if index < bytes.count && bytes[index] == 58 {
                guard !objects.isEmpty,
                      let key = try? JSONDecoder().decode(String.self, from: Data(bytes[start..<end])),
                      objects[objects.count - 1].insert(key).inserted else { return false }
            }
        }
        return objects.isEmpty
    }
}
