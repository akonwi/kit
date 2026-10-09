import Foundation
import OpenAPIRuntime

private struct ChildTranscriptCursorUnavailable: Error {}
import CryptoKit

/// Refuse redirects rather than forwarding a bearer token to another endpoint.
private final class NoRedirects: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

/// Keeps the live annotation preview private until the adjacent submission event resolves it.
struct LiveAnnotationGate {
    private var candidateTurn: String?
    private(set) var releasedOrdinaryCandidate = false

    mutating func shouldPublish(_ events: [WireSessionEvent]) -> Bool {
        releasedOrdinaryCandidate = false
        for event in events {
            switch event.kind.rawValue {
            case "user.message.added":
                candidateTurn = event.text == "Annotations" && (event.content?.isEmpty ?? true) ? event.turnId : nil
            case "annotation.submitted":
                candidateTurn = nil
            default:
                // A normal message whose text happens to be "Annotations" is
                // released when the next event isn't a submission.
                if candidateTurn != nil { releasedOrdinaryCandidate = true }
                candidateTurn = nil
            }
        }
        return candidateTurn == nil
    }
}

struct LocalDaemonRegistry: Decodable {
    let registryVersion: Int
    let protocolVersion: Int
    let kitVersion: String
    let url: String
    let instanceId: String
    let pid: Int
}

struct LocalDaemonHealth: Decodable {
    let instanceId: String
    let protocolVersion: Int
    let kitVersion: String
    let pid: Int
    let databaseReady: Bool
}

final class HTTPClient: ScratchpadClient, DiffClient, AnnotationClient, WorkspaceFileClient, SubagentDismissalClient, SubagentConfigurationClient, SubagentMessagingClient, BashClient, TranscriptPagingClient, SubagentStreamingClient, SessionMutationClient, SessionCreationClient, SessionNamingClient, SessionDirectoryClient, SessionReloadClient, SessionCompactionClient, PromptCommandClient, PluginCommandClient, PluginNotificationClient, SessionDeletionClient, SessionDisposalClient, SessionForkClient, ComposerClient, AttachmentClient {
    let serverID: String
    let isDemo = false
    let endpoint: URL
    private let token: String
    private let instance: String
    private let session: URLSession
    private let api: Client

    init(endpoint: URL, token: String, instance: String, serverID: String,
         configuration: URLSessionConfiguration = .ephemeral) throws {
        guard endpoint.scheme == "http", ["127.0.0.1", "::1", "[::1]"].contains(endpoint.host ?? ""),
              endpoint.port != nil, endpoint.user == nil, endpoint.password == nil,
              endpoint.query == nil, endpoint.fragment == nil, endpoint.path.isEmpty || endpoint.path == "/",
              !token.isEmpty, !instance.isEmpty else { throw ClientError.invalidEndpoint }
        self.endpoint = endpoint; self.token = token; self.instance = instance; self.serverID = serverID
        let configuration = configuration.copy() as! URLSessionConfiguration
        configuration.connectionProxyDictionary = [:]
        configuration.timeoutIntervalForRequest = 45
        configuration.timeoutIntervalForResource = .infinity
        let session = URLSession(configuration: configuration, delegate: NoRedirects(), delegateQueue: nil)
        self.session = session
        api = Client(serverURL: endpoint, transport: OpenAPITransport(endpoint: endpoint, token: token, instance: instance, session: session))
    }

    // URLSession retains its connections until explicitly invalidated, including
    // abandoned SSE requests. LocalClient creates a transport per operation.
    deinit { session.invalidateAndCancel() }

    static func local() async throws -> HTTPClient {
        // Discovery is read-only. Starting a daemon remains owned by Kit's CLI.
        let root = SharedSettingsStore.home.appendingPathComponent("run")
        let registry: LocalDaemonRegistry
        let token: String
        do {
            registry = try JSONDecoder().decode(LocalDaemonRegistry.self, from: Data(contentsOf: root.appendingPathComponent("server.json")))
            token = try String(contentsOf: root.appendingPathComponent("server.token"), encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
        } catch {
            let failure = error as NSError
            if failure.domain == NSCocoaErrorDomain && failure.code == NSFileReadNoSuchFileError {
                throw ClientError.noDaemon
            }
            let cause = failure.userInfo[NSUnderlyingErrorKey] as? NSError ?? failure
            throw ClientError.discovery(cause.localizedDescription)
        }
        guard registry.registryVersion == 1 else { throw ClientError.incompatible }
        guard let url = URL(string: registry.url), registry.pid > 0 else { throw ClientError.invalidEndpoint }
        let client = try HTTPClient(endpoint: url, token: token, instance: registry.instanceId, serverID: "local-v2")
        let output = try await client.generatedOperation { try await client.api.getHealth(headers: .init(xKitInstanceID: registry.instanceId)) }
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let payload = try response.body.json
        let health = LocalDaemonHealth(instanceId: payload.instanceId, protocolVersion: payload.protocolVersion,
                                       kitVersion: payload.kitVersion, pid: payload.pid, databaseReady: payload.databaseReady)
        try validateDiscovery(registry: registry, health: health, clientRelease: DaemonCompatibility.clientRelease())
        return client
    }

    static func validateDiscovery(registry: LocalDaemonRegistry, health: LocalDaemonHealth, clientRelease: String) throws {
        guard health.instanceId == registry.instanceId, health.pid == registry.pid,
              health.protocolVersion == registry.protocolVersion,
              health.kitVersion == registry.kitVersion else { throw ClientError.invalidPayload }
        guard health.databaseReady else { throw ClientError.daemonNotReady }
        if let reason = DaemonCompatibility.mismatch(clientVersion: clientRelease, daemonVersion: registry.kitVersion,
                                                      clientProtocol: kitWireVersion, daemonProtocol: registry.protocolVersion) {
            throw ClientError.incompatibleDaemon(clientRelease: clientRelease, daemonVersion: registry.kitVersion,
                                                 appProtocol: kitWireVersion, daemonProtocol: registry.protocolVersion, reason: reason)
        }
    }

    private func request(_ path: String, query: [URLQueryItem] = []) throws -> URLRequest {
        var parts = URLComponents(url: endpoint.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { parts.queryItems = query }
        guard let url = parts.url else { throw ClientError.invalidEndpoint }
        var request = URLRequest(url: url)
        request.setValue("Bearer " + token, forHTTPHeaderField: "Authorization")
        request.setValue(instance, forHTTPHeaderField: "X-Kit-Instance-ID")
        request.setValue(String(kitWireVersion), forHTTPHeaderField: "X-Kit-Protocol-Version")
        return request
    }

    private func generated<Source: Encodable, Destination: Decodable>(_ source: Source, as: Destination.Type) throws -> Destination {
        try JSONDecoder().decode(Destination.self, from: JSONEncoder().encode(source))
    }

    private func generatedOperation<Output>(_ operation: () async throws -> Output) async throws -> Output {
        do { return try await operation() }
        catch let error as OpenAPIRuntime.ClientError {
            if let failure = error.underlyingError as? ClientError { throw failure }
            if let status = error.response?.status.code {
                if (200..<300).contains(status) { throw ClientError.invalidPayload }
                throw ClientError.http(status)
            }
            throw error
        }
    }

    private func check(_ response: URLResponse) throws {
        guard let response = response as? HTTPURLResponse else { throw ClientError.invalidPayload }
        guard response.statusCode == 200 else { throw ClientError.http(response.statusCode) }
    }

    private func childTranscript(session id: String, conversation: String, before: String? = nil) async throws -> WireSubagentTranscript {
        guard [id, conversation].allSatisfy({ !$0.isEmpty && $0.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" } }) else { throw ClientError.invalidPayload }
        let output = try await api.getSubagentTranscript(path: .init(sessionID: id, conversationID: conversation), query: .init(before: before), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        switch output {
        case .ok(let response):
            let result: WireSubagentTranscript = try generated(try response.body.json, as: WireSubagentTranscript.self)
            guard result.conversationId == conversation else { throw ClientError.invalidPayload }
            return result
        case .conflict(let response):
            if case .json(let payload) = response.body, case .transcriptCursorUnavailable = payload.error {
                throw ChildTranscriptCursorUnavailable()
            }
            throw ClientError.http(409)
        default:
            throw ClientError.invalidPayload
        }
    }

    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] {
        var projection = SubagentStreamProjection()
        return try projection.project(await completeChildTranscript(session: session, conversation: conversation)).messages
    }

    private func completeChildTranscript(session id: String, conversation: String) async throws -> WireSubagentTranscript {
        do {
            return try await assembledChildTranscript(session: id, conversation: conversation)
        } catch is ChildTranscriptCursorUnavailable {
            return try await childTranscript(session: id, conversation: conversation)
        }
    }

    private func assembledChildTranscript(session id: String, conversation: String) async throws -> WireSubagentTranscript {
        var page = try await childTranscript(session: id, conversation: conversation)
        var messages = page.messages ?? []
        var guardCursor = UInt64.max
        while page.hasMoreMessages == true {
            guard let cursor = page.previousMessageCursor, let value = UInt64(cursor), value > 0, value < guardCursor else {
                throw ClientError.invalidPayload
            }
            guardCursor = value
            let older = try await childTranscript(session: id, conversation: conversation, before: cursor)
            let olderMessages = older.messages ?? []
            if let last = olderMessages.last?.sequence, let first = messages.first?.sequence, last >= first {
                throw ClientError.invalidPayload
            }
            messages = olderMessages + messages
            page = WireSubagentTranscript(conversationId: conversation, messages: messages,
                previousMessageCursor: older.previousMessageCursor, hasMoreMessages: older.hasMoreMessages)
        }
        return page
    }

    private func mergeChildTranscript(_ current: WireSubagentTranscript, newest: WireSubagentTranscript) -> WireSubagentTranscript {
        let currentMessages = current.messages ?? []
        let page = newest.messages ?? []
        guard let pageMin = page.first?.sequence, pageMin > 0, !currentMessages.isEmpty else { return newest }
        let prefix = currentMessages.filter { $0.sequence > 0 && $0.sequence < pageMin }
        guard !prefix.isEmpty else { return newest }
        let prefixIDs = Set(prefix.compactMap { $0.id.isEmpty ? nil : $0.id })
        guard page.allSatisfy({ $0.id.isEmpty || !prefixIDs.contains($0.id) }) else { return newest }
        return WireSubagentTranscript(conversationId: newest.conversationId, messages: prefix + page,
            previousMessageCursor: current.previousMessageCursor, hasMoreMessages: current.hasMoreMessages)
    }

    func watchSubagent(session id: String, conversation: String,
                       receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws {
        var projection = SubagentStreamProjection()
        var transcript = try await completeChildTranscript(session: id, conversation: conversation)
        var lastHistoryRead = Date.distantPast
        var resets = 0
        while !Task.isCancelled {
            let oldCursor = projection.cursor
            let oldStream = projection.stream
            let page: WireSubagentLiveEventPage
            do {
                let output = try await api.getSubagentEvents(path: .init(sessionID: id, conversationID: conversation), query: .init(stream: projection.stream, after: Int(projection.cursor)), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
                switch output {
                case .ok(let response): page = try generated(try response.body.json, as: WireSubagentLiveEventPage.self)
                case .notFound: throw ClientError.http(404)
                case .badRequest: throw ClientError.http(400)
                case .conflict: throw ClientError.http(409)
                case .tooManyRequests: throw ClientError.http(429)
                case .serviceUnavailable: throw ClientError.http(503)
                case .unauthorized: throw ClientError.http(401)
                case .forbidden: throw ClientError.http(403)
                case .misdirectedRequest: throw ClientError.http(421)
                case .upgradeRequired: throw ClientError.http(426)
                case .internalServerError: throw ClientError.http(500)
                case .undocumented(let status, _): throw ClientError.http(status)
                }
            } catch ClientError.http(404) {
                // Journals are runtime-local; a retained conversation may have no journal after restart.
                transcript = try await completeChildTranscript(session: id, conversation: conversation)
                projection = SubagentStreamProjection()
                await receive(try projection.project(transcript))
                try await Task.sleep(for: .seconds(1))
                continue
            }
            guard try projection.accept(page) else {
                resets += 1
                guard resets <= 3 else { throw ClientError.invalidPayload }
                transcript = try await completeChildTranscript(session: id, conversation: conversation)
                lastHistoryRead = .distantPast
                continue
            }
            resets = 0
            let changed = oldCursor != projection.cursor || oldStream != projection.stream
            let boundary = (page.events ?? []).contains {
                $0.sequence > oldCursor && ["message.completed", "tool.started", "tool.completed", "turn.settled", "execution.settled"].contains($0.kind)
            }
            let refresh = oldStream.isEmpty || boundary || Date().timeIntervalSince(lastHistoryRead) >= 3
            if refresh {
                // Read history after boundaries so persisted completions win over replayed evidence.
                transcript = mergeChildTranscript(transcript, newest: try await childTranscript(session: id, conversation: conversation))
                lastHistoryRead = Date()
            }
            if changed || refresh {
                try Task.checkCancellation()
                await receive(try projection.project(transcript, refresh: refresh))
            }
            try await Task.sleep(for: .milliseconds(250))
        }
        try Task.checkCancellation()
    }

    func diffTargets(_ id: String) async throws -> WireDiffTargetCatalog {
        let workspaceOutput = try await api.getWorkspace(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        guard case let .ok(workspaceResponse) = workspaceOutput else { throw WorkspaceFileError.unavailable }
        let workspace: WireWorkspaceRef = try generated(try workspaceResponse.body.json, as: WireWorkspaceRef.self)
        guard Self.validScratchpadSession(id), workspace.sessionId == id, workspace.state == "ready",
              let version = Operations.ListDiffTargets.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let input = Components.Schemas.ListDiffTargetsInput(workspaceId: workspace.workspaceId)
        let output = try await api.listDiffTargets(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(input))
        guard case let .ok(response) = output else { throw DiffReadError(code: "unavailable") }
        let result: WireDiffTargetCatalog = try generated(try response.body.json, as: WireDiffTargetCatalog.self)
        guard result.sessionId == id, result.workspaceId == workspace.workspaceId,
              let targets = result.targets, targets.count <= 42,
              Set(targets.map(\.targetId)).count == targets.count,
              targets.allSatisfy({ $0.targetId.hasPrefix("difftarget_") && $0.reference.hasPrefix("difftargetref_") && $0.reference.utf8.count <= 2048 && ["working_tree", "branch", "commit"].contains($0.kind) }) else { throw ClientError.invalidPayload }
        return result
    }

    func observeDiff(_ id: String, input: WireObserveDiffInput) async throws -> WireWorkingTreePage {
        guard Self.validScratchpadSession(id), let version = Operations.ObserveDiff.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let body: Components.Schemas.ObserveDiffInput = try generated(input, as: Components.Schemas.ObserveDiffInput.self)
        let output = try await api.observeDiff(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .ok(response) = output else { throw DiffReadError(code: "unavailable") }
        let page: WireWorkingTreePage = try generated(try response.body.json, as: WireWorkingTreePage.self)
        try DiffValidation.observation(page.observation, session: id, target: input.expectedTargetId, revision: input.expectedTargetRevision)
        guard page.observation.target.workspaceId == input.workspaceId, let files = page.files, files.count <= 200,
              Set(files.map(\.path)).count == files.count else { throw ClientError.invalidPayload }
        try files.forEach(DiffValidation.file)
        try DiffValidation.cursor(page.nextCursor)
        return page
    }

    func readDiff(_ id: String, input: WireReadFileDiffInput) async throws -> WireFileDiffPage {
        guard Self.validScratchpadSession(id), let version = Operations.ReadFileDiff.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let body: Components.Schemas.ReadFileDiffInput = try generated(input, as: Components.Schemas.ReadFileDiffInput.self)
        let output = try await api.readFileDiff(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .ok(response) = output else { throw DiffReadError(code: "unavailable") }
        let page: WireFileDiffPage = try generated(try response.body.json, as: WireFileDiffPage.self)
        try DiffValidation.observation(page.observation, session: id, target: input.targetId, revision: input.targetRevision)
        guard page.file.path == input.path, input.expectedFileRevision == nil || page.file.fileRevision == input.expectedFileRevision else { throw ClientError.invalidPayload }
        try DiffValidation.file(page.file)
        try DiffValidation.hunks(page)
        try DiffValidation.cursor(page.nextCursor)
        return page
    }

    private func annotationPath(_ id: String) throws -> String {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else {
            throw MutationNotSent(reason: "The session identity is invalid.")
        }
        return "v1/sessions/" + id + "/annotations"
    }

    func annotations(_ id: String) async throws -> [FileAnnotation] {
        guard Self.validScratchpadSession(id), let version = Operations.ListAnnotations.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        var result: [FileAnnotation] = []; var cursor: String?; var seen = Set<String>()
        repeat {
            let output = try await api.listAnnotations(path: .init(sessionID: id), query: .init(cursor: cursor, pageSize: 100), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version))
            guard case let .ok(response) = output else { throw ClientError.invalidPayload }
            let page: WireAnnotationPage = try generated(try response.body.json, as: WireAnnotationPage.self)
            guard page.sessionId == id, let entries = page.entries, entries.count <= 100 else { throw ClientError.invalidPayload }
            for entry in entries { let value = try FileAnnotation(entry, session: id); guard value.id > (result.last?.id ?? 0), result.count < 128 else { throw ClientError.invalidPayload }; result.append(value) }
            cursor = page.nextCursor.flatMap { $0.isEmpty ? nil : $0 }
            if let cursor { guard !entries.isEmpty, seen.insert(cursor).inserted, seen.count <= 2 else { throw ClientError.invalidPayload } }
        } while cursor != nil
        return result
    }

    func createAnnotation(_ id: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation {
        guard Self.validScratchpadSession(id), FileAnnotation.validBody(input.body), let version = Operations.CreateAnnotation.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw MutationNotSent(reason: "Enter a comment of at most 16 KiB.") }
        let body: Components.Schemas.CreateAnnotationInput = try generated(input, as: Components.Schemas.CreateAnnotationInput.self)
        do {
            let output = try await api.createAnnotation(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
            switch output {
            case .created(let response):
                return try FileAnnotation(try generated(try response.body.json, as: WireAnnotation.self), session: id)
            case .conflict(let response):
                switch try response.body.json.error {
                case .staleFile: throw AnnotationEvidenceConflict.staleFile
                case .staleWorkspace: throw AnnotationEvidenceConflict.staleWorkspace
                case .staleTarget: throw AnnotationEvidenceConflict.staleTarget
                default: throw ClientError.http(409)
                }
            default: throw ClientError.invalidPayload
            }
        } catch let error as AnnotationEvidenceConflict { throw error }
          catch let error as ClientError { throw error }
          catch {
            // A future conflict code is intentionally preserved as an HTTP
            // fallback even when the generated closed union cannot decode it.
            if String(describing: error).contains("response: 409") { throw ClientError.http(409) }
            throw error
        }
    }

    func updateAnnotation(_ id: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation {
        guard Self.validScratchpadSession(id), input.annotationId > 0, FileAnnotation.validBody(input.body), let version = Operations.UpdateAnnotation.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw MutationNotSent(reason: "Enter a comment of at most 16 KiB.") }
        let body: Components.Schemas.UpdateAnnotationInput = try generated(input, as: Components.Schemas.UpdateAnnotationInput.self)
        let output = try await api.updateAnnotation(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let record: WireAnnotation = try generated(try response.body.json, as: WireAnnotation.self)
        guard record.id == input.annotationId else { throw ClientError.invalidPayload }; return try FileAnnotation(record, session: id)
    }

    func deleteAnnotation(_ id: String, id annotationID: UInt64) async throws {
        guard Self.validScratchpadSession(id), annotationID > 0, annotationID <= UInt64(Int.max), let version = Operations.DeleteAnnotation.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let output = try await api.deleteAnnotation(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(.init(annotationId: Int(annotationID))))
        guard case .noContent = output else { throw ClientError.invalidPayload }
    }

    func deleteSession(_ id: String) async throws {
        guard Self.validScratchpadSession(id), let version = Operations.DeleteSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let output = try await api.deleteSession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version))
        guard case .noContent = output else { throw ClientError.invalidPayload }
    }

    func disposeSession(_ id: String) async throws {
        guard Self.validScratchpadSession(id), let version = Operations.DisposeTemporarySession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let output = try await api.disposeTemporarySession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version))
        guard case .noContent = output else { throw ClientError.invalidPayload }
    }

    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> ForkedSession {
        guard Self.validScratchpadSession(id), let version = Operations.ForkSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw MutationNotSent(reason: "The source session identity is invalid.") }
        let body: Components.Schemas.ForkSessionInput = try generated(input, as: Components.Schemas.ForkSessionInput.self)
        let output = try await api.forkSession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .created(response) = output else { throw ClientError.invalidPayload }
        let result: WireForkSessionResult = try response.body.json
        let record = result.session
        guard record.id != id, record.parentSessionId == id, record.temporary != true,
              input.prompt != nil || result.firstTurnError == nil else { throw ClientError.invalidPayload }
        if let message = result.firstTurnError?.message {
            guard !message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  PluginCommand.safeText(message, limit: .max) else { throw ClientError.invalidPayload }
        }
        return ForkedSession(session: try SessionProjection.summary(record), firstTurnError: result.firstTurnError?.message)
    }

    func renameSession(_ id: String, name: String) async throws -> SessionExcerpt {
        guard Self.validScratchpadSession(id), let name = SessionName.normalized(name), let version = Operations.RenameSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw MutationNotSent(reason: "Enter a valid session name of at most 256 bytes.") }
        let body: Components.Schemas.RenameSessionInput = try generated(WireRenameSessionInput(name: name), as: Components.Schemas.RenameSessionInput.self)
        let output = try await api.renameSession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let record: WireSessionInfo = try generated(try response.body.json, as: WireSessionInfo.self)
        guard record.id == id, record.name == name else { throw ClientError.invalidPayload }
        return try SessionProjection.summary(record)
    }

    private func bashPath(_ session: String, _ id: String? = nil) throws -> String {
        func valid(_ value: String) -> Bool { !value.isEmpty && value.allSatisfy { $0.isLetter || $0.isNumber || $0 == "_" || $0 == "-" } }
        guard valid(session), id.map(valid) ?? true else { throw ClientError.invalidPayload }
        return "v1/sessions/" + session + "/bash-executions" + (id.map { "/" + $0 } ?? "")
    }
    func dismissSubagent(_ session: String, conversation: String, generation: UInt64) async throws {
        guard !session.isEmpty, session.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" || $0 == "-" }),
              conversation.hasPrefix("subagent_"), conversation.dropFirst(9).count == 32,
              conversation.dropFirst(9).allSatisfy({ "0123456789abcdef".contains($0) }), generation > 0 else {
            throw MutationNotSent(reason: "Invalid subagent conversation identity.")
        }
        let input = WireSubagentOperationInput(action: .init(rawValue: "dismiss")!, agent: nil, message: nil,
            conversationId: conversation, taskId: nil, timeoutMs: nil, generation: generation)
        let body: Components.Schemas.SubagentOperationInput = try generated(input, as: Components.Schemas.SubagentOperationInput.self)
        let output = try await api.operateSubagent(path: .init(sessionID: session), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WireSubagentOperationResult = try generated(try response.body.json, as: WireSubagentOperationResult.self)
        guard result.dismissed == true else { throw ClientError.invalidPayload }
    }

    func configureSubagent(_ session: String, conversation: String, generation: UInt64,
                           model: String?, thinking: String?) async throws -> SubagentConfigurationResult {
        func rendererSafe(_ value: String, limit: Int) -> Bool {
            !value.isEmpty && value.utf8.count <= limit && !value.unicodeScalars.contains {
                $0.properties.generalCategory == .control || $0.properties.generalCategory == .format
            }
        }
        func validExactModel(_ value: String) -> Bool {
            let parts = value.split(separator: "/", omittingEmptySubsequences: false)
            return parts.count == 2 && !parts[0].isEmpty && !parts[1].isEmpty
                && value == value.trimmingCharacters(in: .whitespacesAndNewlines)
                && rendererSafe(value, limit: 256)
        }
        let validModel = model.map(validExactModel) ?? true
        guard !session.isEmpty, session.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" || $0 == "-" }),
              conversation.hasPrefix("subagent_"), conversation.dropFirst(9).count == 32,
              conversation.dropFirst(9).allSatisfy({ "0123456789abcdef".contains($0) }),
              generation > 0, generation <= UInt64(Int.max), model != nil || thinking != nil, validModel,
              thinking.map({ WireThinkingLevel(rawValue: $0) != nil }) ?? true else {
            throw MutationNotSent(reason: "Invalid subagent configuration.")
        }
        let body = Components.Schemas.ConfigureSubagentInput(generation: Int(generation), model: model, thinkingLevel: thinking)
        let output = try await generatedOperation {
            try await api.configureSubagent(path: .init(sessionID: session, conversationID: conversation),
                headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body))
        }
        switch output {
        case .ok(let response):
            let result = try response.body.json
            let applied = result.conversation
            // The response must describe exactly the requested conversation and patch.
            guard applied.id == conversation, applied.generation == Int(generation),
                  model.map({ $0 == applied.model }) ?? !applied.model.isEmpty,
                  thinking.map({ $0 == applied.thinkingLevel }) ?? (WireThinkingLevel(rawValue: applied.thinkingLevel) != nil),
                  (result.compacted == true) == !(result.checkpointId ?? "").isEmpty,
                  validExactModel(applied.model),
                  (result.warnings?.count ?? 0) <= 16,
                  (result.warnings ?? []).allSatisfy({ rendererSafe($0, limit: 4096) }) else { throw ClientError.invalidPayload }
            return SubagentConfigurationResult(model: applied.model, thinkingLevel: applied.thinkingLevel,
                                               warnings: result.warnings ?? [])
        case .badRequest(let response):
            switch try response.body.json.error {
            case .invalidRequest(let error): throw SubagentConfigurationRejected(message: error.message)
            }
        case .unprocessableContent(let response):
            switch try response.body.json.error {
            case .unprocessable(let error): throw SubagentConfigurationRejected(message: error.message)
            }
        case .unauthorized: throw ClientError.http(401)
        case .forbidden: throw ClientError.http(403)
        case .notFound: throw ClientError.http(404)
        case .conflict: throw ClientError.http(409)
        case .misdirectedRequest: throw ClientError.http(421)
        case .upgradeRequired: throw ClientError.http(426)
        case .tooManyRequests: throw ClientError.http(429)
        case .internalServerError: throw ClientError.http(500)
        case .serviceUnavailable: throw ClientError.http(503)
        case .undocumented(let status, _): throw ClientError.http(status)
        }
    }

    func sendSubagent(_ session: String, agent: String, conversation: String?, message: String) async throws -> SubagentReceipt {
        guard !session.isEmpty, session.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" || $0 == "-" }),
              !agent.isEmpty, agent.utf8.count <= 128, !agent.contains(where: { $0.isWhitespace || $0 == "/" || $0 == "\\" }),
              !message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              message.utf8.count <= 128 * 1024, !message.contains("\0") else {
            throw MutationNotSent(reason: "Invalid subagent message.")
        }
        let input = WireSubagentOperationInput(action: .init(rawValue: conversation == nil ? "start" : "message")!, agent: conversation == nil ? agent : nil,
            message: message, conversationId: conversation, taskId: nil, timeoutMs: nil, generation: nil)
        let body: Components.Schemas.SubagentOperationInput = try generated(input, as: Components.Schemas.SubagentOperationInput.self)
        let output = try await api.operateSubagent(path: .init(sessionID: session), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body))
        guard case let .accepted(response) = output else { throw ClientError.invalidPayload }
        let result: WireSubagentOperationResult = try generated(try response.body.json, as: WireSubagentOperationResult.self)
        return try SubagentReceipt(result, agent: agent, conversation: conversation)
    }

    func startBash(_ session: String, input: WireBashExecutionInput) async throws -> BashExecution {
        guard input.executionId.hasPrefix("bash_"), !input.command.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              input.command.utf8.count <= 64 * 1024, !input.command.contains("\0") else { throw MutationNotSent(reason: "Invalid shell command.") }
        let body: Components.Schemas.BashExecutionInput = try generated(input, as: Components.Schemas.BashExecutionInput.self)
        let output = try await api.startBash(path: .init(sessionID: session), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body))
        guard case let .accepted(response) = output else { throw ClientError.invalidPayload }
        let wire: WireBashExecution = try generated(try response.body.json, as: WireBashExecution.self)
        guard wire.id == input.executionId, wire.command == input.command,
              (wire.excludeFromContext == true) == (input.excludeFromContext == true) else { throw ClientError.invalidPayload }
        return try BashExecution(wire, session: session)
    }
    func bash(_ session: String, id: String) async throws -> BashExecution {
        let output = try await api.getBash(path: .init(sessionID: session, executionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let wire: WireBashExecution = try generated(try response.body.json, as: WireBashExecution.self)
        guard wire.id == id else { throw ClientError.invalidPayload }
        return try BashExecution(wire, session: session)
    }
    func abortBash(_ session: String, id: String) async throws {
        let output = try await api.abortBash(path: .init(sessionID: session, executionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        guard case let .accepted(response) = output else { throw ClientError.invalidPayload }
        let reply: [String: Bool] = try generated(try response.body.json, as: [String: Bool].self)
        guard reply["aborting"] == true else { throw ClientError.invalidPayload }
    }

    /// Blocks on the server-pushed repository-status stream: latest snapshot
    /// first, then deduplicated latest-only `vcs.status` records, with comment
    /// heartbeats. Reconnection is the caller's policy; every record is
    /// strictly decoded and re-validated at this boundary before delivery.
    func watchVCS(_ id: String, receive: @escaping @Sendable (WireSessionVCSStatus) async -> Void) async throws {
        guard Self.validScratchpadSession(id) else { throw ClientError.invalidPayload }
        guard let protocolVersion = Operations.StreamSessionVCS.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else {
            throw ClientError.incompatible
        }
        let output: Operations.StreamSessionVCS.Output
        do {
            output = try await api.streamSessionVCS(
                path: .init(sessionID: id),
                headers: .init(xKitInstanceID: instance, xKitProtocolVersion: protocolVersion)
            )
        } catch let error as OpenAPIRuntime.ClientError {
            // Preserve the transport's classifications, such as a stream
            // response with the wrong content type.
            if let failure = error.underlyingError as? ClientError { throw failure }
            // A response the contract cannot decode, such as an undeclared
            // code or a body that is not the error envelope, is a protocol
            // violation. Failures before any response remain transient.
            if error.response != nil { throw ClientError.invalidPayload }
            throw error
        }
        let body: HTTPBody
        switch output {
        case .ok(let response): body = try response.body.textEventStream
        case .badRequest: throw ClientError.http(400)
        case .unauthorized: throw ClientError.http(401)
        case .forbidden: throw ClientError.http(403)
        case .notFound: throw ClientError.http(404)
        case .conflict: throw ClientError.http(409)
        case .misdirectedRequest: throw ClientError.http(421)
        case .upgradeRequired: throw ClientError.http(426)
        case .tooManyRequests: throw ClientError.http(429)
        case .internalServerError: throw ClientError.http(500)
        case .serviceUnavailable: throw ClientError.http(503)
        case .undocumented: throw ClientError.invalidPayload // Not a declared pre-stream failure.
        }
        for try await event in body.asDecodedServerSentEvents() {
            try Task.checkCancellation()
            if let status = try VCSStatusRecords.decode(event, session: id) { await receive(status) }
        }
        try Task.checkCancellation()
        // A truncated record is not an update; the stream simply ended.
        throw ClientError.disconnected
    }

    func watchPluginNotifications(_ id: String, receive: @escaping @Sendable (PluginNotification) async -> Void) async throws {
        guard Self.validScratchpadSession(id) else { throw ClientError.invalidPayload }
        guard let protocolVersion = Operations.StreamPluginToasts.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else {
            throw ClientError.incompatible
        }
        let output: Operations.StreamPluginToasts.Output
        do {
            output = try await api.streamPluginToasts(
                path: .init(sessionID: id),
                headers: .init(xKitInstanceID: instance, xKitProtocolVersion: protocolVersion)
            )
        } catch let error as OpenAPIRuntime.ClientError {
            if let failure = error.underlyingError as? ClientError { throw failure }
            if error.response != nil { throw ClientError.invalidPayload }
            throw error
        }
        let body: HTTPBody
        switch output {
        case .ok(let response): body = try response.body.textEventStream
        case .badRequest: throw ClientError.http(400)
        case .unauthorized: throw ClientError.http(401)
        case .forbidden: throw ClientError.http(403)
        case .notFound: throw ClientError.http(404)
        case .conflict: throw ClientError.http(409)
        case .misdirectedRequest: throw ClientError.http(421)
        case .upgradeRequired: throw ClientError.http(426)
        case .tooManyRequests: throw ClientError.http(429)
        case .internalServerError: throw ClientError.http(500)
        case .serviceUnavailable: throw ClientError.http(503)
        case .undocumented: throw ClientError.invalidPayload
        }
        for try await event in body.asDecodedServerSentEvents() {
            try Task.checkCancellation()
            if let notification = try PluginToastRecords.decode(event) { await receive(notification) }
        }
        try Task.checkCancellation()
        throw ClientError.disconnected
    }

    func executePluginCommand(_ id: String, input: WirePluginCommandInput) async throws {
        guard !id.isEmpty, id.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") }),
              PluginCommand.validSelection(id: input.id, instance: input.instance), input.args.utf8.count <= 64 * 1024,
              !input.args.contains("\0") else { throw MutationNotSent(reason: "Invalid plugin command or arguments.") }
        guard let protocolVersion = Operations.ExecutePluginCommand.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else {
            throw ClientError.incompatible
        }
        let output: Operations.ExecutePluginCommand.Output
        do {
            output = try await api.executePluginCommand(
                path: .init(sessionID: id),
                headers: .init(xKitInstanceID: instance, xKitProtocolVersion: protocolVersion),
                body: .json(.init(args: input.args, id: input.id, instance: input.instance))
            )
        } catch let error as OpenAPIRuntime.ClientError {
            if let failure = error.underlyingError as? ClientError { throw failure }
            if error.response != nil { throw ClientError.invalidPayload }
            throw error
        }
        switch output {
        case .noContent: return
        case .conflict(let response):
            switch try response.body.json.error {
            case .pluginCommandUnavailable: throw PluginCommandFailure(unavailable: true)
            case .conflict, .instanceMismatch: throw ClientError.http(409)
            }
        case .unprocessableContent: throw PluginCommandFailure(unavailable: false)
        case .badRequest: throw ClientError.http(400)
        case .unauthorized: throw ClientError.http(401)
        case .forbidden: throw ClientError.http(403)
        case .notFound: throw ClientError.http(404)
        case .misdirectedRequest: throw ClientError.http(421)
        case .upgradeRequired: throw ClientError.http(426)
        case .internalServerError: throw ClientError.http(500)
        case .serviceUnavailable: throw ClientError.http(503)
        case .undocumented: throw ClientError.invalidPayload
        }
    }

    func submitPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WirePromptSubmission {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              PromptCommand.validName(input.name), (input.args?.utf8.count ?? 0) <= 128 * 1024,
              !(input.args ?? "").contains("\0") else { throw MutationNotSent(reason: "Invalid prompt command or arguments.") }
        guard let version = Operations.SubmitPromptCommand.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.incompatible }
        let body: Components.Schemas.PromptCommandInput = try generated(input, as: Components.Schemas.PromptCommandInput.self)
        let output = try await generatedOperation { try await api.submitPromptCommand(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body)) }
        guard case let .accepted(response) = output else { throw ClientError.invalidPayload }
        let result: WirePromptSubmission = try generated(try response.body.json, as: WirePromptSubmission.self)
        _ = try FollowUpState(result.queue)
        guard result.queued ? (result.reservation == nil && result.queue.count > 0) : result.reservation?.sessionId == id,
              result.queued || !(result.reservation?.turnId ?? "").isEmpty else { throw ClientError.invalidPayload }
        return result
    }

    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult {
        guard Self.validScratchpadSession(id), !input.operationId.isEmpty, input.operationId.utf8.count <= 256, let version = Operations.CompactSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let body: Components.Schemas.CompactSessionInput = try generated(input, as: Components.Schemas.CompactSessionInput.self)
        let output = try await api.compactSession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WireCompactSessionResult = try generated(try response.body.json, as: WireCompactSessionResult.self)
        guard result.operationId == input.operationId, result.eventStreamId.hasPrefix("stream_"), !result.compacted || !(result.checkpointId ?? "").isEmpty else { throw ClientError.invalidPayload }
        return result
    }

    func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        guard Self.validScratchpadSession(id), let version = Operations.ReloadSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let output = try await api.reloadSession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WireReloadSessionResult = try generated(try response.body.json, as: WireReloadSessionResult.self)
        guard result.sessionId == id, !result.eventStreamId.isEmpty, (1...512).contains(result.sources?.count ?? 0), (result.diagnostics?.count ?? 0) <= 256, (result.warnings?.count ?? 0) <= 8, (result.sources ?? []).allSatisfy({ !$0.id.isEmpty && !$0.sectionId.isEmpty }), (result.diagnostics ?? []).allSatisfy({ ["info", "warning"].contains($0.severity) && !$0.code.isEmpty && !$0.message.isEmpty }) else { throw ClientError.invalidPayload }
        return result
    }

    func changeDirectory(_ id: String, input: WireChangeCWDInput) async throws -> SessionExcerpt {
        guard Self.validScratchpadSession(id), !input.path.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, let version = Operations.ChangeSessionCWD.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let body: Components.Schemas.ChangeCWDInput = try generated(input, as: Components.Schemas.ChangeCWDInput.self)
        let output = try await api.changeSessionCWD(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WireChangeWorkspaceCWDResult = try generated(try response.body.json, as: WireChangeWorkspaceCWDResult.self)
        guard result.session.id == id, result.workspace.sessionId == id, result.workspace.cwd == result.session.cwd else { throw ClientError.invalidPayload }
        return try SessionProjection.summary(result.session)
    }

    func readWorkspaceFile(_ id: String, path: String, expectedRevision: String?) async throws -> WireWorkspaceFileRead {
        try await WorkspaceReadGate.shared.read {
            try await self.readWorkspaceFileNow(id, path: path, expectedRevision: expectedRevision)
        }
    }

    private func readWorkspaceFileNow(_ id: String, path: String, expectedRevision: String?) async throws -> WireWorkspaceFileRead {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              !path.isEmpty, !path.hasPrefix("/"), !path.split(separator: "/").contains("..") else { throw ClientError.invalidPayload }
        let workspaceOutput = try await api.getWorkspace(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        guard case let .ok(workspaceResponse) = workspaceOutput else { throw WorkspaceFileError.unavailable }
        let workspace: WireWorkspaceRef = try generated(try workspaceResponse.body.json, as: WireWorkspaceRef.self)
        guard workspace.sessionId == id, workspace.state == "ready" else { throw WorkspaceFileError.unavailable }
        do {
            let input = WireReadWorkspaceFileInput(workspaceId: workspace.workspaceId, path: path, expectedFileRevision: expectedRevision)
            let body: Components.Schemas.ReadWorkspaceFileInput = try generated(input, as: Components.Schemas.ReadWorkspaceFileInput.self)
            let output = try await api.readWorkspaceFile(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body))
            guard case let .ok(response) = output else { throw ClientError.invalidPayload }
            let result: WireWorkspaceFileRead = try generated(try response.body.json, as: WireWorkspaceFileRead.self)
            guard result.sessionId == id, result.path == path,
                  result.workspace.workspaceId == workspace.workspaceId, result.workspace.cwd == workspace.cwd,
                  result.returnedBytes == result.content.utf8.count,
                  result.returnedBytes <= workspace.limits.maxPreviewBytes else { throw ClientError.invalidPayload }
            return result
        } catch ClientError.http(let code) {
            throw WorkspaceFileError.response(code)
        } catch {
            let description = String(describing: error)
            for status in [400, 403, 404, 409, 413, 415, 429, 503] where description.contains("response: \(status)") {
                throw WorkspaceFileError.response(status)
            }
            throw error
        }
    }

    func vcs(_ id: String) async throws -> WireSessionVCSStatus {
        guard Self.validScratchpadSession(id) else { throw ClientError.invalidPayload }
        guard let protocolVersion = Operations.GetSessionVCS.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else {
            throw ClientError.incompatible
        }
        let output = try await api.getSessionVCS(
            path: .init(sessionID: id),
            headers: .init(xKitInstanceID: instance, xKitProtocolVersion: protocolVersion)
        )
        let payload: Components.Schemas.SessionVCSStatus
        switch output {
        case .ok(let response): payload = try response.body.json
        case .badRequest: throw ClientError.http(400)
        case .unauthorized: throw ClientError.http(401)
        case .forbidden: throw ClientError.http(403)
        case .notFound: throw ClientError.http(404)
        case .conflict: throw ClientError.http(409)
        case .misdirectedRequest: throw ClientError.http(421)
        case .upgradeRequired: throw ClientError.http(426)
        case .internalServerError: throw ClientError.http(500)
        case .serviceUnavailable: throw ClientError.http(503)
        case .undocumented(let status, _): throw ClientError.http(status)
        }
        guard payload.sessionId == id, payload.cwd.hasPrefix("/") else { throw ClientError.invalidPayload }
        return PullRequestLink.sanitized(WireSessionVCSStatus(payload))
    }

    func models() async throws -> [WireModelCapability] {
        let output = try await api.listModels(headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let catalog: WireModelCatalog = try generated(try response.body.json, as: WireModelCatalog.self)
        return try validatedModels(catalog)
    }

    func refreshModels() async throws -> [WireModelCapability] {
        let output = try await api.refreshModels(headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let catalog: WireModelCatalog = try generated(try response.body.json, as: WireModelCatalog.self)
        return try validatedModels(catalog)
    }

    private func validatedModels(_ catalog: WireModelCatalog) throws -> [WireModelCapability] {
        let models = catalog.models ?? []
        guard Set(models.map(\.id)).count == models.count,
              models.allSatisfy({ $0.id.contains("/") && !$0.name.isEmpty && !($0.thinkingLevels ?? []).isEmpty }) else { throw ClientError.invalidPayload }
        return models.filter(\.available)
    }

    func configure(_ id: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult {
        guard Self.validScratchpadSession(id), let version = Operations.ConfigureSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let body: Components.Schemas.ConfigureSessionInput = try generated(input, as: Components.Schemas.ConfigureSessionInput.self)
        let output = try await api.configureSession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WireConfigureSessionResult = try generated(try response.body.json, as: WireConfigureSessionResult.self)
        guard result.session.id == id, result.session.model == input.model, result.session.configurationRevision > input.expectedRevision, !result.eventStreamId.isEmpty else { throw ClientError.invalidPayload }
        _ = try SessionProjection.summary(result.session)
        return result
    }

    func files(_ id: String) async throws -> [String] { try await files(id, refresh: false) }

    func files(_ id: String, refresh: Bool) async throws -> [String] {
        try await fileIndex(id, refresh: refresh).paths
    }

    func fileIndex(_ id: String, refresh: Bool) async throws -> FileIndex {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw ClientError.invalidPayload }
        let output = try await api.getSessionFileIndex(path: .init(sessionID: id), query: .init(refresh: refresh ? true : nil), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let index: WireSessionFileIndex = try generated(try response.body.json, as: WireSessionFileIndex.self)
        guard index.sessionId == id, index.cwd.hasPrefix("/"), let entries = index.entries,
              entries.count <= 4000 else { throw ClientError.invalidPayload }
        for entry in entries {
            let path = entry.path
            guard !path.isEmpty, path.utf8.count <= 4096, !path.hasPrefix("/"),
                  !path.contains("\\"), !path.unicodeScalars.contains(where: { $0.value < 32 || $0.value == 127 }),
                  !path.split(separator: "/").contains(where: { $0 == "." || $0 == ".." }),
                  !path.contains("//"), path.hasSuffix("/") == (entry.isDir == true) else { throw ClientError.invalidPayload }
        }
        var seen = Set<String>()
        return FileIndex(paths: entries.map(\.path).filter { seen.insert($0).inserted }, cwd: index.cwd, truncated: index.truncated)
    }

    // Attachment bytes retain the narrow raw path: generated multipart encoding
    // labels the binary part as text/plain, while reads do not expose the
    // integrity headers this client validates before displaying content.
    private func sendAttachmentUpload(_ request: URLRequest) async throws -> WireAttachmentInfo {
        let (bytes, response) = try await session.bytes(for: request)
        defer { bytes.task.cancel() }
        return try await withTaskCancellationHandler {
            guard let response = response as? HTTPURLResponse else { throw ClientError.invalidPayload }
            guard response.statusCode == 201 else { throw ClientError.http(response.statusCode) }
            var data = Data()
            for try await byte in bytes { data.append(byte) }
            return try JSONDecoder().decode(WireAttachmentInfo.self, from: data)
        } onCancel: { bytes.task.cancel() }
    }

    func upload(_ id: String, filename: String, data: Data) async throws -> WireAttachmentInfo {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw ClientError.invalidPayload }
        guard !data.isEmpty, data.count <= 10 * 1024 * 1024 else { throw ClientError.oversized }
        let boundary = "Kit-" + UUID().uuidString
        let safeName = filename.replacingOccurrences(of: "\r", with: "").replacingOccurrences(of: "\n", with: "")
            .replacingOccurrences(of: "\"", with: "_").replacingOccurrences(of: "\\", with: "_")
        var body = Data("--\(boundary)\r\nContent-Disposition: form-data; name=\"file\"; filename=\"\(safeName)\"\r\nContent-Type: application/octet-stream\r\n\r\n".utf8)
        body.append(data)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        var request = try request("v1/sessions/" + id + "/attachments")
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=" + boundary, forHTTPHeaderField: "Content-Type")
        request.httpBody = body
        let info = try await sendAttachmentUpload(request)
        guard info.sessionId == id, info.id.hasPrefix("attachment_"), info.size == data.count,
              !info.filename.isEmpty, info.sha256.count == 64 else { throw ClientError.invalidPayload }
        return info
    }

    func resolveAttachments(_ id: String, ids: [String]) async throws -> WireAttachmentResolution {
        let input = WireAttachmentResolutionInput(attachmentIds: ids)
        let body: Components.Schemas.AttachmentResolutionInput = try generated(input, as: Components.Schemas.AttachmentResolutionInput.self)
        let output = try await api.resolveAttachments(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WireAttachmentResolution = try generated(try response.body.json, as: WireAttachmentResolution.self)
        let found = result.attachments ?? []
        let returned = found.map(\.id) + (result.missingAttachmentIds ?? [])
        guard Set(returned) == Set(ids), returned.count == Set(returned).count,
              found.allSatisfy({ $0.sessionId == id && $0.size > 0 && $0.size <= 16 * 1024 * 1024 }) else { throw ClientError.invalidPayload }
        return result
    }

    func attachmentContent(_ id: String, info: WireAttachmentInfo) async throws -> Data {
        guard info.sessionId == id, info.size > 0, info.size <= 16 * 1024 * 1024,
              info.id.hasPrefix("attachment_"), info.id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" }) else { throw ClientError.invalidPayload }
        let (bytes, response) = try await session.bytes(for: request("v1/sessions/" + id + "/attachments/" + info.id))
        defer { bytes.task.cancel() }
        return try await withTaskCancellationHandler {
            try check(response)
            guard let http = response as? HTTPURLResponse,
                  http.value(forHTTPHeaderField: "X-Kit-Attachment-ID") == info.id,
                  http.value(forHTTPHeaderField: "X-Kit-Session-ID") == id,
                  http.mimeType == info.mediaType else { throw ClientError.invalidPayload }
            var data = Data()
            for try await byte in bytes {
                guard data.count < info.size else { throw ClientError.oversized }
                data.append(byte)
            }
            guard data.count == info.size,
                  SHA256.hash(data: data).map({ String(format: "%02x", $0) }).joined() == info.sha256 else { throw ClientError.invalidPayload }
            return data
        } onCancel: { bytes.task.cancel() }
    }

    func respond(_ id: String, input: WireInteractionResponse) async throws {
        guard input.requestId.hasPrefix("interaction_"), input.requestId.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" }) else { throw ClientError.invalidPayload }
        let body: Components.Schemas.InteractionResponse = try generated(input, as: Components.Schemas.InteractionResponse.self)
        let output = try await generatedOperation { try await api.respondInteraction(path: .init(sessionID: id, interactionID: input.requestId), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body)) }
        guard case let .ok(response) = output, try response.body.json.settled else { throw ClientError.invalidPayload }
    }

    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        guard let version = Operations.CreateSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.incompatible }
        let body: Components.Schemas.CreateSessionInput = try generated(input, as: Components.Schemas.CreateSessionInput.self)
        let output = try await api.createSession(headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version), body: .json(body))
        guard case let .created(response) = output else { throw ClientError.invalidPayload }
        let record: WireSessionInfo = try generated(try response.body.json, as: WireSessionInfo.self)
        guard input.id == nil || input.id == record.id else { throw ClientError.invalidPayload }
        return try SessionProjection.summary(record)
    }

    func followUps(_ id: String) async throws -> FollowUpState { try await FollowUpState(wireSnapshot(id).followUps) }

    func submit(_ id: String, input: WirePromptInput) async throws -> WirePromptSubmission {
        let body: Components.Schemas.PromptInput = try generated(input, as: Components.Schemas.PromptInput.self)
        let output = try await generatedOperation { try await api.submitPrompt(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45), body: .json(body)) }
        guard case let .accepted(response) = output else { throw ClientError.invalidPayload }
        let result: WirePromptSubmission = try generated(try response.body.json, as: WirePromptSubmission.self)
        _ = try FollowUpState(result.queue)
        guard result.queued ? (result.reservation == nil && result.queue.count > 0) : result.reservation?.sessionId == id,
              result.queued || !(result.reservation?.turnId ?? "").isEmpty else { throw ClientError.invalidPayload }
        return result
    }
    func restoreFollowUps(_ id: String) async throws -> WireRestoreFollowUpsResult {
        let output = try await generatedOperation { try await api.restoreTurnFollowUps(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45)) }
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WireRestoreFollowUpsResult = try generated(try response.body.json, as: WireRestoreFollowUpsResult.self)
        _ = try FollowUpState(result.queue)
        guard result.queue.count == 0, (result.messages?.count ?? 0) <= 64,
              (result.messages ?? []).allSatisfy({ !$0.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !($0.attachmentIds ?? []).isEmpty }) else { throw ClientError.invalidPayload }
        return result
    }
    func promoteFollowUps(_ id: String) async throws -> WirePromoteFollowUpsResult {
        let output = try await generatedOperation { try await api.promoteTurnFollowUps(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45)) }
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let result: WirePromoteFollowUpsResult = try generated(try response.body.json, as: WirePromoteFollowUpsResult.self)
        _ = try FollowUpState(result.queue)
        guard result.promoted >= 0, result.promoted <= 64, result.queue.count == 0 else { throw ClientError.invalidPayload }
        return result
    }
    func abort(_ id: String, turn: String) async throws {
        guard !turn.isEmpty, turn.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw MutationNotSent(reason: "The turn identity is invalid.") }
        let output = try await generatedOperation { try await api.abortTurn(path: .init(sessionID: id, turnID: turn), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45)) }
        guard case let .accepted(response) = output, try response.body.json.aborting else { throw ClientError.invalidPayload }
    }

    func sessions() async throws -> [SessionExcerpt] {
        guard let version = Operations.ListSessions.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.incompatible }
        let output = try await api.listSessions(query: .init(), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let list: [WireSessionInfo] = try generated(try response.body.json.sessions, as: [WireSessionInfo].self)
        return try list.map { try SessionProjection.summary($0) }
    }

    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              let cursor = UInt64(before), cursor > 0, let beforeCursor = Int(exactly: cursor) else { throw ClientError.invalidPayload }
        let output = try await generatedOperation { try await api.getMessagePage(path: .init(sessionID: id), query: .init(before: beforeCursor), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45)) }
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let page: WireTranscriptPage = try generated(try response.body.json, as: WireTranscriptPage.self)
        return try TranscriptHistoryPage(page, sessionID: id, before: before)
    }

    func messageHistory(_ id: String, before: String?) async throws -> ComposerMessageHistoryPage {
        guard Self.validHistorySession(id),
              before == nil || (before.flatMap(UInt64.init) ?? 0) > 0 else { throw ClientError.invalidPayload }
        let cursor = before.flatMap(Int.init)
        let output = try await generatedOperation { try await api.getMessagePage(path: .init(sessionID: id), query: .init(before: cursor, limit: 100, role: ["user"]), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45)) }
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let page: WireMessageHistoryPage = try generated(try response.body.json, as: WireMessageHistoryPage.self)
        let source = try page.validatedMessages(session: id, before: before)
        let entries = source.compactMap { message -> ComposerMessageHistoryEntry? in
            let text = SessionProjection.visibleText(message.content ?? []).trimmingCharacters(in: .whitespacesAndNewlines)
            return text.isEmpty ? nil : .init(id: message.id, text: text)
        }
        return .init(entries: entries, nextCursor: page.nextCursor, hasMore: page.hasMore)
    }

    func bashHistory(_ id: String, before: String?, limit: Int) async throws -> ComposerBashHistoryPage {
        guard Self.validHistorySession(id), (1...200).contains(limit),
              before == nil || (before.flatMap(UInt64.init) ?? 0) > 0 else { throw ClientError.invalidPayload }
        do {
            let output = try await api.getBashHistory(path: .init(sessionID: id), query: .init(before: before.flatMap(Int.init), limit: limit), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45))
            guard case let .ok(response) = output else { throw ClientError.invalidPayload }
            let page: WireBashHistoryPage = try generated(try response.body.json, as: WireBashHistoryPage.self)
            return try ComposerBashHistoryPage(page, session: id, before: before)
        } catch let error as ClientError { throw error }
          catch { throw ClientError.invalidPayload }
    }

    private static func validHistorySession(_ id: String) -> Bool {
        !id.isEmpty && id.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }
    }

    private func wireSnapshot(_ id: String) async throws -> WireSessionSnapshot {
        guard Self.validScratchpadSession(id), let version = Operations.GetSession.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else { throw ClientError.invalidPayload }
        let output = try await api.getSession(path: .init(sessionID: id), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: version))
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let snapshot: WireSessionSnapshot = try generated(try response.body.json, as: WireSessionSnapshot.self)
        guard snapshot.session.id == id, (snapshot.eventCursor ?? 0) >= 0 else { throw ClientError.invalidPayload }
        _ = try SessionProjection.snapshot(snapshot)
        return snapshot
    }

    func snapshot(_ id: String) async throws -> SessionExcerpt { try SessionProjection.snapshot(await wireSnapshot(id)) }

    func scratchpad(session id: String) async throws -> ScratchpadRecord {
        guard Self.validScratchpadSession(id) else { throw ClientError.invalidPayload }
        guard let protocolVersion = Operations.GetScratchpad.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else {
            throw ClientError.incompatible
        }
        let output = try await api.getScratchpad(
            path: .init(sessionID: id),
            headers: .init(xKitInstanceID: instance, xKitProtocolVersion: protocolVersion)
        )
        switch output {
        case .ok(let response): return try ScratchpadRecord(response.body.json)
        case .badRequest: throw ClientError.http(400)
        case .unauthorized: throw ClientError.http(401)
        case .forbidden: throw ClientError.http(403)
        case .notFound: throw ClientError.http(404)
        case .conflict(let response):
            switch try response.body.json.error {
            case .scratchpadMigrationRequired(let error): throw ScratchpadFailure.rejected(error.message)
            case .scratchpadUnsupported(let error): throw ScratchpadFailure.rejected(error.message)
            case .instanceMismatch: throw ClientError.http(409)
            }
        case .misdirectedRequest: throw ClientError.http(421)
        case .upgradeRequired: throw ClientError.http(426)
        case .internalServerError: throw ClientError.http(500)
        case .serviceUnavailable: throw ClientError.http(503)
        case .undocumented(let status, _): throw ClientError.http(status)
        }
    }

    func updateScratchpad(session id: String, content: String, expectedRevision: Int64) async throws -> ScratchpadRecord {
        guard Self.validScratchpadSession(id), expectedRevision > 0,
              ScratchpadRecord.validContent(content) else {
            throw ClientError.invalidPayload
        }
        guard let protocolVersion = Operations.UpdateScratchpad.Input.Headers.XKitProtocolVersionPayload(rawValue: kitWireVersion) else {
            throw ClientError.incompatible
        }
        let output = try await api.updateScratchpad(
            path: .init(sessionID: id),
            headers: .init(xKitInstanceID: instance, xKitProtocolVersion: protocolVersion),
            body: .json(.init(content: content, expectedRevision: String(expectedRevision)))
        )
        let result: ScratchpadRecord
        switch output {
        case .ok(let response): result = try ScratchpadRecord(response.body.json)
        case .badRequest(let response):
            switch try response.body.json.error {
            case .invalidRequest, .scratchpadInvalidContent: throw ClientError.http(400)
            }
        case .unauthorized: throw ClientError.http(401)
        case .forbidden: throw ClientError.http(403)
        case .notFound: throw ClientError.http(404)
        case .conflict(let response):
            switch try response.body.json.error {
            case .scratchpadRevisionConflict(let error):
                throw ScratchpadFailure.conflict(try ScratchpadRecord(error.details.scratchpad))
            case .scratchpadRevisionExhausted(let error): throw ScratchpadFailure.rejected(error.message)
            case .scratchpadMigrationRequired(let error): throw ScratchpadFailure.rejected(error.message)
            case .scratchpadUnsupported(let error): throw ScratchpadFailure.rejected(error.message)
            case .instanceMismatch: throw ClientError.http(409)
            }
        case .contentTooLarge(let response):
            switch try response.body.json.error {
            case .scratchpadTooLarge, .limitExceeded: throw ClientError.http(413)
            }
        case .misdirectedRequest: throw ClientError.http(421)
        case .upgradeRequired: throw ClientError.http(426)
        case .internalServerError: throw ClientError.http(500)
        case .serviceUnavailable: throw ClientError.http(503)
        case .undocumented(let status, _): throw ClientError.http(status)
        }
        guard result.content == content,
              result.revision == expectedRevision || (expectedRevision < Int64.max && result.revision == expectedRevision + 1) else {
            throw ClientError.invalidPayload
        }
        return result
    }

    private static func validScratchpadSession(_ id: String) -> Bool {
        !id.isEmpty && id.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }
    }

    private struct StreamResync: Error {}

    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        let projection = BashWatchProjection(receive: receive)
        try await withThrowingTaskGroup(of: Void.self) { group in
            group.addTask { try await self.watchEvents(id) { await projection.stream($0) } }
            group.addTask {
                while !Task.isCancelled {
                    let snapshot = try await self.snapshot(id)
                    var active: BashExecution?
                    if let executionID = snapshot.activeBashID { active = try await self.bash(id, id: executionID) }
                    var settled: [BashExecution] = []
                    for executionID in await projection.runningIDs() where executionID != snapshot.activeBashID {
                        settled.append(try await self.bash(id, id: executionID))
                    }
                    await projection.poll(snapshot, active: active, settled: settled)
                    try await Task.sleep(for: .seconds(active == nil ? 2 : 1))
                }
            }
            defer { group.cancelAll() }
            try await group.next()
        }
    }

    private func watchEvents(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        var rapidResyncs = 0
        while !Task.isCancelled {
            let started = Date()
            do {
                let generation = UUID().uuidString
                try await watchStream(id) { snapshot in
                    var snapshot = snapshot
                    snapshot.watchGeneration = generation
                    await receive(snapshot)
                }
                return
            } catch is StreamResync {
                // The server can rotate or close its event stream at run boundaries.
                // Reattach from a fresh snapshot before reporting a connection failure.
                rapidResyncs = Date().timeIntervalSince(started) > 5 ? 1 : rapidResyncs + 1
                guard rapidResyncs <= 3 else { throw ClientError.disconnected }
                try await Task.sleep(for: .milliseconds(100))
            }
        }
        try Task.checkCancellation()
    }

    func acceptedAnnotationMessage(session id: String, event: WireSessionEvent) async throws -> WireTranscriptMessage {
        guard let acceptedID = event.acceptedMessageId, let ids = event.annotationIds,
              !acceptedID.isEmpty, !ids.isEmpty else { throw ClientError.invalidPayload }
        let output = try await generatedOperation { try await api.getMessagePage(path: .init(sessionID: id), query: .init(limit: 100, role: ["user"]), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45)) }
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let page: WireMessageHistoryPage = try generated(try response.body.json, as: WireMessageHistoryPage.self)
        let messages = try page.validatedMessages(session: id, before: nil)
        guard let message = messages.first(where: { $0.id == acceptedID }),
              let notes = message.content?.flatMap({ $0.annotations ?? [] }), notes.count == ids.count,
              Set(notes.map(\.originalAnnotationId)) == Set(ids) else { throw ClientError.invalidPayload }
        return message
    }

    private func watchStream(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        var snapshot = try await wireSnapshot(id)
        var projection = try SessionEventProjection(snapshot)
        await receive(projection.session)
        var annotationGate = LiveAnnotationGate()
        let stream = snapshot.eventStreamId ?? ""
        var cursor = snapshot.eventReplayAvailable == true ? snapshot.eventReplayFrom ?? 0 : snapshot.eventCursor ?? 0
        guard let after = Int(exactly: cursor) else { throw ClientError.invalidPayload }
        let output = try await generatedOperation { try await api.streamSessionEvents(path: .init(sessionID: id), query: .init(stream: stream, after: after), headers: .init(xKitInstanceID: instance, xKitProtocolVersion: ._45)) }
        guard case let .ok(response) = output else { throw ClientError.invalidPayload }
        let body = try response.body.textEventStream
        var parser = SSEParser()
        for try await chunk in body {
            for byte in chunk {
                try Task.checkCancellation()
                guard let frame = try parser.append(byte) else { continue }
                if frame.event == "session.resync" { throw StreamResync() }
                guard frame.event == "session.events" else { throw ClientError.invalidPayload }
                let batch = try JSONDecoder().decode(WireSessionEventBatch.self, from: frame.data)
                let updated: Int64
                do { updated = try EventCursor.accept(batch, session: id, stream: stream, cursor: cursor) }
                catch ClientError.disconnected { throw StreamResync() }
                guard updated > cursor else { continue }
                var refresh = false
                var refreshSubagents = false
                var applied: [WireSessionEvent] = []
                var submissions: [WireSessionEvent] = []
                for event in batch.events ?? [] where event.sequence > cursor {
                    applied.append(event)
                    if event.kind.rawValue == "annotation.submitted" { submissions.append(event) }
                    refreshSubagents = refreshSubagents || event.kind.rawValue == "subagent.changed"
                    try projection.apply(event)
                    cursor = event.sequence
                    let newEvent = event.sequence > (snapshot.eventCursor ?? 0)
                    refresh = refresh || (newEvent
                        && ["turn.completed", "compaction.completed"].contains(event.kind.rawValue))
                }
                cursor = updated
                let publish = annotationGate.shouldPublish(applied)
                if annotationGate.releasedOrdinaryCandidate {
                    projection.clearAnnotationPreviewCandidate()
                }
                for event in submissions {
                    do {
                        let message = try await acceptedAnnotationMessage(session: id, event: event)
                        try projection.acceptAnnotationMessage(message, ids: event.annotationIds ?? [])
                    } catch is CancellationError { throw CancellationError() }
                    catch let error as URLError where error.code == .cancelled { throw error }
                    catch {
                        if Task.isCancelled { throw CancellationError() }
                        // Never publish the synthetic preview. Reconcile immediately
                        // if the accepted message cannot be read or validated.
                        projection.discardAnnotationPreview()
                        refresh = true
                    }
                }
                // Hold a possible synthetic user preview across frames. The
                // accepted message above replaces it before any publication.
                if publish { await receive(projection.session) }
                if refreshSubagents && !refresh {
                    let metadata = try await wireSnapshot(id)
                    guard metadata.eventStreamId == stream, (metadata.eventCursor ?? 0) >= cursor else { throw ClientError.disconnected }
                    try projection.updateSubagents(metadata)
                    await receive(projection.session)
                }
                if refresh {
                    snapshot = try await wireSnapshot(id)
                    guard snapshot.eventStreamId == stream, (snapshot.eventCursor ?? 0) >= cursor else { throw StreamResync() }
                    // If another turn has already started, reattach using its replay boundary.
                    guard snapshot.activeTurnId == nil else { throw StreamResync() }
                    projection = try SessionEventProjection(snapshot, terminalError: projection.session.terminalError)
                    annotationGate = LiveAnnotationGate()
                    cursor = snapshot.eventCursor ?? cursor
                    await receive(projection.session)
                }
            }
        }
        throw StreamResync()
    }
}

struct SSEParser {
    struct Frame { let event: String; let data: Data }
    private var line = Data()
    private var data = Data()
    private var event = ""
    mutating func append(_ byte: UInt8) throws -> Frame? {
        guard line.count + data.count < 2 * 1024 * 1024 else { throw ClientError.oversized }
        if byte != 10 { line.append(byte); return nil }
        defer { line.removeAll(keepingCapacity: true) }
        if line.last == 13 { line.removeLast() }
        guard let text = String(data: line, encoding: .utf8) else { throw ClientError.invalidPayload }
        if text.isEmpty {
            defer { data.removeAll(keepingCapacity: true); event = "" }
            return data.isEmpty ? nil : Frame(event: event, data: data)
        }
        if text.hasPrefix("event:") { event = String(text.dropFirst(6)).trimmingCharacters(in: .whitespaces) }
        if text.hasPrefix("data:") {
            if !data.isEmpty { data.append(10) }
            let value = text.dropFirst(5)
            data.append(contentsOf: (value.first == " " ? value.dropFirst() : value).utf8)
        }
        return nil
    }
}

struct EventCursor {
    static func accept(_ batch: WireSessionEventBatch, session: String, stream: String, cursor: Int64) throws -> Int64 {
        guard batch.resyncRequired != true, batch.streamId == stream else { throw ClientError.disconnected }
        var next = cursor
        for event in batch.events ?? [] {
            guard event.sessionId == session, event.streamId == stream, event.sequence > 0 else { throw ClientError.invalidPayload }
            if event.sequence <= next { continue }
            guard event.sequence == next + 1 else { throw ClientError.disconnected }
            next = event.sequence
        }
        return next
    }
}
