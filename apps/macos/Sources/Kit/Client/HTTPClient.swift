import Foundation
import CryptoKit

/// Refuse redirects rather than forwarding a bearer token to another endpoint.
private final class NoRedirects: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

final class HTTPClient: DiffClient, AnnotationClient, WorkspaceFileClient, SubagentDismissalClient, SubagentMessagingClient, BashClient, TranscriptPagingClient, SubagentStreamingClient, SessionMutationClient, SessionCreationClient, SessionNamingClient, SessionDirectoryClient, SessionReloadClient, SessionCompactionClient, PromptCommandClient, SessionDeletionClient, SessionDisposalClient, SessionForkClient, ComposerClient, AttachmentClient {
    let serverID: String
    let isDemo = false
    let endpoint: URL
    private let token: String
    private let instance: String
    private let session: URLSession

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
        session = URLSession(configuration: configuration, delegate: NoRedirects(), delegateQueue: nil)
    }

    // URLSession retains its connections until explicitly invalidated, including
    // abandoned SSE requests. LocalClient creates a transport per operation.
    deinit { session.invalidateAndCancel() }

    static func local() async throws -> HTTPClient {
        // Discovery is read-only. Starting a daemon remains owned by Kit's CLI.
        let root = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".kit-v2/run")
        struct Registry: Decodable { let registryVersion: Int; let protocolVersion: Int; let url: String; let instanceId: String; let pid: Int }
        let registry: Registry
        let token: String
        do {
            registry = try JSONDecoder().decode(Registry.self, from: Data(contentsOf: root.appendingPathComponent("server.json")))
            token = try String(contentsOf: root.appendingPathComponent("server.token"), encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
        } catch {
            let failure = error as NSError
            if failure.domain == NSCocoaErrorDomain && failure.code == NSFileReadNoSuchFileError {
                throw ClientError.noDaemon
            }
            let cause = failure.userInfo[NSUnderlyingErrorKey] as? NSError ?? failure
            throw ClientError.discovery(cause.localizedDescription)
        }
        guard registry.registryVersion == 1, registry.protocolVersion == kitWireVersion else { throw ClientError.incompatible }
        guard let url = URL(string: registry.url), registry.pid > 0 else { throw ClientError.invalidEndpoint }
        let client = try HTTPClient(endpoint: url, token: token, instance: registry.instanceId, serverID: "local-v2")
        struct Health: Decodable { let instanceId: String; let protocolVersion: Int; let pid: Int; let databaseReady: Bool }
        let health: Health = try await client.get("v1/health")
        guard health.instanceId == registry.instanceId, health.pid == registry.pid,
              health.protocolVersion == kitWireVersion, health.databaseReady else { throw ClientError.incompatible }
        return client
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

    private func check(_ response: URLResponse) throws {
        guard let response = response as? HTTPURLResponse else { throw ClientError.invalidPayload }
        guard response.statusCode == 200 else { throw ClientError.http(response.statusCode) }
    }

    private func get<T: Decodable>(_ path: String, query: [URLQueryItem] = []) async throws -> T {
        let (bytes, response) = try await session.bytes(for: request(path, query: query))
        defer { bytes.task.cancel() }
        return try await withTaskCancellationHandler {
            try check(response)
            var data = Data()
            for try await byte in bytes {
                guard data.count < 32 * 1024 * 1024 else { throw ClientError.oversized }
                data.append(byte)
            }
            return try JSONDecoder().decode(T.self, from: data)
        } onCancel: {
            bytes.task.cancel()
        }
    }

    private func childTranscript(session id: String, conversation: String) async throws -> WireSubagentTranscript {
        guard [id, conversation].allSatisfy({ !$0.isEmpty && $0.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" } }) else { throw ClientError.invalidPayload }
        let result: WireSubagentTranscript = try await get("v1/sessions/" + id + "/subagents/" + conversation + "/transcript")
        guard result.conversationId == conversation else { throw ClientError.invalidPayload }
        return result
    }

    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] {
        var projection = SubagentStreamProjection()
        return try projection.project(await childTranscript(session: session, conversation: conversation)).messages
    }

    func watchSubagent(session id: String, conversation: String,
                       receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws {
        var projection = SubagentStreamProjection()
        var transcript = try await childTranscript(session: id, conversation: conversation)
        var lastHistoryRead = Date.distantPast
        var resets = 0
        while !Task.isCancelled {
            let oldCursor = projection.cursor
            let oldStream = projection.stream
            let page: WireSubagentLiveEventPage
            do {
                page = try await get("v1/sessions/" + id + "/subagents/" + conversation + "/events",
                    query: [URLQueryItem(name: "stream", value: projection.stream),
                            URLQueryItem(name: "after", value: String(projection.cursor))])
            } catch ClientError.http(404) {
                // Journals are runtime-local; a retained conversation may have no journal after restart.
                transcript = try await childTranscript(session: id, conversation: conversation)
                projection = SubagentStreamProjection()
                await receive(try projection.project(transcript))
                try await Task.sleep(for: .seconds(1))
                continue
            }
            guard try projection.accept(page) else {
                resets += 1
                guard resets <= 3 else { throw ClientError.invalidPayload }
                transcript = try await childTranscript(session: id, conversation: conversation)
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
                transcript = try await childTranscript(session: id, conversation: conversation)
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

    private func mutate<Input: Encodable, Output: Decodable>(_ id: String, suffix: String, input: Input) async throws -> Output {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else {
            throw MutationNotSent(reason: "The session identity is invalid.")
        }
        return try await post("v1/sessions/" + id + "/" + suffix, input: input)
    }

    private func post<Input: Encodable, Output: Decodable>(_ path: String, input: Input, status: [Int] = [200, 202]) async throws -> Output {
        var request = try request(path)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(input)
        return try await send(request, status: status)
    }

    private func send<Output: Decodable>(_ request: URLRequest, status: [Int]) async throws -> Output {
        let (bytes, response) = try await session.bytes(for: request)
        defer { bytes.task.cancel() }
        return try await withTaskCancellationHandler {
            guard let response = response as? HTTPURLResponse else { throw ClientError.invalidPayload }
            guard status.contains(response.statusCode) else { throw ClientError.http(response.statusCode) }
            var data = Data()
            for try await byte in bytes {
                guard data.count < 32 * 1024 * 1024 else { throw ClientError.oversized }
                data.append(byte)
            }
            return try JSONDecoder().decode(Output.self, from: data)
        } onCancel: { bytes.task.cancel() }
    }

    private func diffRequest<Input: Encodable, Output: Decodable>(_ id: String, suffix: String, input: Input) async throws -> Output {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" || $0 == "-" }) else { throw ClientError.invalidPayload }
        var request = try request("v1/sessions/" + id + "/diff/" + suffix)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(input)
        let (bytes, response) = try await session.bytes(for: request)
        defer { bytes.task.cancel() }
        return try await withTaskCancellationHandler {
            var data = Data()
            for try await byte in bytes {
                guard data.count < 512 * 1024 else { throw ClientError.oversized }
                data.append(byte)
            }
            guard let response = response as? HTTPURLResponse else { throw ClientError.invalidPayload }
            guard response.statusCode == 200 else {
                if let value = try? JSONDecoder().decode(DiffErrorEnvelope.self, from: data) {
                    throw DiffReadError(code: value.error.code.rawValue)
                }
                throw ClientError.http(response.statusCode)
            }
            return try JSONDecoder().decode(Output.self, from: data)
        } onCancel: { bytes.task.cancel() }
    }

    func diffTargets(_ id: String) async throws -> WireDiffTargetCatalog {
        let workspace: WireWorkspaceRef = try await get("v1/sessions/" + id + "/workspace")
        guard workspace.sessionId == id, workspace.state.rawValue == "ready" else { throw WorkspaceFileError.unavailable }
        let result: WireDiffTargetCatalog = try await diffRequest(id, suffix: "targets",
            input: WireListDiffTargetsInput(workspaceId: workspace.workspaceId))
        guard result.sessionId == id, result.workspaceId == workspace.workspaceId,
              let targets = result.targets, targets.count <= 42,
              Set(targets.map(\.targetId)).count == targets.count,
              targets.allSatisfy({ $0.targetId.hasPrefix("difftarget_") && $0.reference.hasPrefix("difftargetref_") && $0.reference.utf8.count <= 2048 && ["working_tree", "branch", "commit"].contains($0.kind) }) else { throw ClientError.invalidPayload }
        return result
    }

    func observeDiff(_ id: String, input: WireObserveDiffInput) async throws -> WireWorkingTreePage {
        let page: WireWorkingTreePage = try await diffRequest(id, suffix: "observations", input: input)
        try DiffValidation.observation(page.observation, session: id, target: input.expectedTargetId, revision: input.expectedTargetRevision)
        guard page.observation.target.workspaceId == input.workspaceId, let files = page.files, files.count <= 200,
              Set(files.map(\.path)).count == files.count else { throw ClientError.invalidPayload }
        try files.forEach(DiffValidation.file)
        try DiffValidation.cursor(page.nextCursor)
        return page
    }

    func readDiff(_ id: String, input: WireReadFileDiffInput) async throws -> WireFileDiffPage {
        let page: WireFileDiffPage = try await diffRequest(id, suffix: "files/read", input: input)
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
        let path = try annotationPath(id)
        var result: [FileAnnotation] = []
        var cursor: String?
        var seen = Set<String>()
        repeat {
            var query = [URLQueryItem(name: "pageSize", value: "100")]
            if let cursor { query.append(.init(name: "cursor", value: cursor)) }
            let page: WireAnnotationPage = try await get(path, query: query)
            guard page.sessionId == id, let entries = page.entries, entries.count <= 100 else { throw ClientError.invalidPayload }
            for entry in entries {
                let value = try FileAnnotation(entry, session: id)
                guard value.id > (result.last?.id ?? 0), result.count < 128 else { throw ClientError.invalidPayload }
                result.append(value)
            }
            cursor = page.nextCursor.flatMap { $0.isEmpty ? nil : $0 }
            if let cursor {
                guard !entries.isEmpty, cursor.utf8.count <= 256, seen.insert(cursor).inserted, seen.count <= 2 else { throw ClientError.invalidPayload }
            }
        } while cursor != nil
        return result
    }

    func createAnnotation(_ id: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation {
        guard FileAnnotation.validBody(input.body) else { throw MutationNotSent(reason: "Enter a comment of at most 16 KiB.") }
        let record: WireAnnotation = try await post(annotationPath(id), input: input, status: [201])
        return try FileAnnotation(record, session: id)
    }

    func updateAnnotation(_ id: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation {
        guard input.annotationId > 0, FileAnnotation.validBody(input.body) else { throw MutationNotSent(reason: "Enter a comment of at most 16 KiB.") }
        var request = try request(annotationPath(id))
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(input)
        let record: WireAnnotation = try await send(request, status: [200])
        guard record.id == input.annotationId else { throw ClientError.invalidPayload }
        return try FileAnnotation(record, session: id)
    }

    func deleteAnnotation(_ id: String, id annotationID: UInt64) async throws {
        guard annotationID > 0 else { throw ClientError.invalidPayload }
        var request = try request(annotationPath(id))
        request.httpMethod = "DELETE"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(WireDeleteAnnotationInput(annotationId: annotationID))
        let (bytes, response) = try await session.bytes(for: request)
        defer { bytes.task.cancel() }
        guard let response = response as? HTTPURLResponse else { throw ClientError.invalidPayload }
        guard response.statusCode == 204 else { throw ClientError.http(response.statusCode) }
    }

    func deleteSession(_ id: String) async throws {
        try await removeSession(id, temporary: false)
    }

    func disposeSession(_ id: String) async throws {
        try await removeSession(id, temporary: true)
    }

    private func removeSession(_ id: String, temporary: Bool) async throws {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else {
            throw MutationNotSent(reason: "The session identity is invalid.")
        }
        var request = try request("v1/sessions/" + id + (temporary ? "/dispose" : ""))
        request.httpMethod = temporary ? "POST" : "DELETE"
        let (bytes, response) = try await session.bytes(for: request)
        defer { bytes.task.cancel() }
        guard let response = response as? HTTPURLResponse else { throw ClientError.invalidPayload }
        guard response.statusCode == 204 else { throw ClientError.http(response.statusCode) }
    }

    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> SessionExcerpt {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else {
            throw MutationNotSent(reason: "The source session identity is invalid.")
        }
        let record: WireSessionInfo = try await post("v1/sessions/" + id + "/forks", input: input, status: [201])
        guard record.id == input.id, record.parentSessionId == id else { throw ClientError.invalidPayload }
        return try SessionProjection.summary(record)
    }

    func renameSession(_ id: String, name: String) async throws -> SessionExcerpt {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              let name = SessionName.normalized(name) else { throw MutationNotSent(reason: "Enter a valid session name of at most 256 bytes.") }
        var request = try request("v1/sessions/" + id)
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(WireRenameSessionInput(name: name))
        let record: WireSessionInfo = try await send(request, status: [200])
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
        let result: WireSubagentOperationResult = try await post("v1/sessions/" + session + "/subagents", input: input, status: [200])
        guard result.dismissed == true else { throw ClientError.invalidPayload }
    }

    func sendSubagent(_ session: String, agent: String, conversation: String?, message: String) async throws -> SubagentReceipt {
        guard !session.isEmpty, session.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" || $0 == "-" }),
              !agent.isEmpty, agent.utf8.count <= 128, !agent.contains(where: { $0.isWhitespace || $0 == "/" || $0 == "\\" }),
              !message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              message.utf8.count <= 128 * 1024, !message.contains("\0") else {
            throw MutationNotSent(reason: "Invalid subagent message.")
        }
        let input = WireSubagentOperationInput(action: .init(rawValue: conversation == nil ? "start" : "message")!,
            agent: conversation == nil ? agent : nil, message: message, conversationId: conversation,
            taskId: nil, timeoutMs: nil, generation: nil)
        let result: WireSubagentOperationResult = try await post("v1/sessions/" + session + "/subagents", input: input, status: [202])
        return try SubagentReceipt(result, agent: agent, conversation: conversation)
    }

    func startBash(_ session: String, input: WireBashExecutionInput) async throws -> BashExecution {
        guard input.executionId.hasPrefix("bash_"), !input.command.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              input.command.utf8.count <= 64 * 1024, !input.command.contains("\0") else { throw MutationNotSent(reason: "Invalid shell command.") }
        let wire: WireBashExecution = try await post(bashPath(session), input: input, status: [202])
        guard wire.id == input.executionId, wire.command == input.command,
              (wire.excludeFromContext == true) == (input.excludeFromContext == true) else { throw ClientError.invalidPayload }
        return try BashExecution(wire, session: session)
    }
    func bash(_ session: String, id: String) async throws -> BashExecution {
        let wire: WireBashExecution = try await get(bashPath(session, id))
        guard wire.id == id else { throw ClientError.invalidPayload }
        return try BashExecution(wire, session: session)
    }
    func abortBash(_ session: String, id: String) async throws {
        struct Reply: Decodable { let aborting: Bool }
        let reply: Reply = try await post(bashPath(session, id) + "/abort", input: [String: String](), status: [202])
        guard reply.aborting else { throw ClientError.invalidPayload }
    }

    func runPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WireRunReservation {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              PromptCommand.validName(input.name), (input.args?.utf8.count ?? 0) <= 128 * 1024,
              !(input.args ?? "").contains("\0") else { throw MutationNotSent(reason: "Invalid prompt command or arguments.") }
        let result: WireRunReservation = try await post("v1/sessions/" + id + "/prompt-commands", input: input, status: [202])
        guard result.sessionId == id, !result.runId.isEmpty, result.runId == result.turnId else { throw ClientError.invalidPayload }
        return result
    }

    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              !input.operationId.isEmpty, input.operationId.utf8.count <= 256 else { throw ClientError.invalidPayload }
        var request = try request("v1/sessions/" + id + "/compact")
        request.httpMethod = "POST"
        request.timeoutInterval = 120
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(input)
        let result: WireCompactSessionResult = try await send(request, status: [200])
        guard result.operationId == input.operationId, result.eventStreamId.hasPrefix("stream_"),
              !result.compacted || !(result.checkpointId ?? "").isEmpty else { throw ClientError.invalidPayload }
        return result
    }

    func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw ClientError.invalidPayload }
        let result: WireReloadSessionResult = try await post("v1/sessions/" + id + "/reload", input: [String: String](), status: [200])
        guard result.sessionId == id, !result.eventStreamId.isEmpty,
              (1...512).contains(result.sources?.count ?? 0),
              (result.diagnostics?.count ?? 0) <= 256, (result.warnings?.count ?? 0) <= 8,
              (result.sources ?? []).allSatisfy({ !$0.id.isEmpty && !$0.sectionId.isEmpty }),
              (result.diagnostics ?? []).allSatisfy({ ["info", "warning"].contains($0.severity) && !$0.code.isEmpty && !$0.message.isEmpty })
        else { throw ClientError.invalidPayload }
        return result
    }

    func changeDirectory(_ id: String, input: WireChangeCWDInput) async throws -> SessionExcerpt {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              !input.path.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw ClientError.invalidPayload }
        let result: WireChangeWorkspaceCWDResult = try await post("v1/sessions/" + id + "/cwd", input: input, status: [200])
        guard result.session.id == id, result.workspace.sessionId == id,
              result.workspace.cwd == result.session.cwd else { throw ClientError.invalidPayload }
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
        let workspace: WireWorkspaceRef = try await get("v1/sessions/" + id + "/workspace")
        guard workspace.sessionId == id, workspace.state.rawValue == "ready" else { throw WorkspaceFileError.unavailable }
        do {
            let result: WireWorkspaceFileRead = try await post("v1/sessions/" + id + "/workspace/files/read",
                input: WireReadWorkspaceFileInput(workspaceId: workspace.workspaceId, path: path, expectedFileRevision: expectedRevision))
            guard result.sessionId == id, result.path == path,
                  result.workspace.workspaceId == workspace.workspaceId, result.workspace.cwd == workspace.cwd,
                  result.returnedBytes == result.content.utf8.count,
                  result.returnedBytes <= workspace.limits.maxPreviewBytes else { throw ClientError.invalidPayload }
            return result
        } catch ClientError.http(let code) {
            throw WorkspaceFileError.response(code)
        }
    }

    func vcs(_ id: String) async throws -> WireSessionVCSStatus {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw ClientError.invalidPayload }
        let result: WireSessionVCSStatus = try await get("v1/sessions/" + id + "/vcs")
        guard result.sessionId == id, result.cwd.hasPrefix("/") else { throw ClientError.invalidPayload }
        return result
    }

    func models() async throws -> [WireModelCapability] {
        let catalog: WireModelCatalog = try await get("v1/models")
        let models = catalog.models ?? []
        guard Set(models.map(\.id)).count == models.count,
              models.allSatisfy({ $0.id.contains("/") && !$0.name.isEmpty && !($0.thinkingLevels ?? []).isEmpty }) else { throw ClientError.invalidPayload }
        return models.filter(\.available)
    }

    func configure(_ id: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult {
        let result: WireConfigureSessionResult = try await mutate(id, suffix: "configure", input: input)
        guard result.session.id == id, result.session.model == input.model,
              result.session.configurationRevision > input.expectedRevision,
              !result.eventStreamId.isEmpty else { throw ClientError.invalidPayload }
        _ = try SessionProjection.summary(result.session)
        return result
    }

    func files(_ id: String) async throws -> [String] { try await files(id, refresh: false) }

    func files(_ id: String, refresh: Bool) async throws -> [String] {
        try await fileIndex(id, refresh: refresh).paths
    }

    func fileIndex(_ id: String, refresh: Bool) async throws -> FileIndex {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw ClientError.invalidPayload }
        let index: WireSessionFileIndex = try await get("v1/sessions/" + id + "/files", query: refresh ? [URLQueryItem(name: "refresh", value: "true")] : [])
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
        let info: WireAttachmentInfo = try await send(request, status: [201])
        guard info.sessionId == id, info.id.hasPrefix("attachment_"), info.size == data.count,
              !info.filename.isEmpty, info.sha256.count == 64 else { throw ClientError.invalidPayload }
        return info
    }

    func resolveAttachments(_ id: String, ids: [String]) async throws -> WireAttachmentResolution {
        let result: WireAttachmentResolution = try await mutate(id, suffix: "attachments/resolve",
            input: WireAttachmentResolutionInput(attachmentIds: ids))
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
        let result: [String: Bool] = try await mutate(id, suffix: "interactions/" + input.requestId + "/response", input: input)
        guard result["settled"] == true else { throw ClientError.invalidPayload }
    }

    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        let record: WireSessionInfo = try await post("v1/sessions", input: input, status: [201])
        guard input.id == nil || input.id == record.id else { throw ClientError.invalidPayload }
        return try SessionProjection.summary(record)
    }

    func followUps(_ id: String) async throws -> FollowUpState { try await FollowUpState(wireSnapshot(id).followUps) }

    func submit(_ id: String, input: WirePromptInput) async throws -> WirePromptSubmission {
        let result: WirePromptSubmission = try await mutate(id, suffix: "submissions", input: input)
        _ = try FollowUpState(result.queue)
        guard result.queued ? (result.reservation == nil && result.queue.count > 0) : result.reservation?.sessionId == id,
              result.queued || (!(result.reservation?.runId ?? "").isEmpty && !(result.reservation?.turnId ?? "").isEmpty) else { throw ClientError.invalidPayload }
        return result
    }
    func restoreFollowUps(_ id: String) async throws -> WireRestoreFollowUpsResult {
        let result: WireRestoreFollowUpsResult = try await mutate(id, suffix: "follow-ups/restore", input: [String: String]())
        _ = try FollowUpState(result.queue)
        guard result.queue.count == 0, (result.messages?.count ?? 0) <= 64,
              (result.messages ?? []).allSatisfy({ !$0.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !($0.attachmentIds ?? []).isEmpty }) else { throw ClientError.invalidPayload }
        return result
    }
    func promoteFollowUps(_ id: String) async throws -> WirePromoteFollowUpsResult {
        let result: WirePromoteFollowUpsResult = try await mutate(id, suffix: "follow-ups/promote", input: [String: String]())
        _ = try FollowUpState(result.queue)
        guard result.promoted >= 0, result.promoted <= 64, result.queue.count == 0 else { throw ClientError.invalidPayload }
        return result
    }
    func abort(_ id: String, run: String) async throws {
        guard !run.isEmpty, run.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw MutationNotSent(reason: "The run identity is invalid.") }
        struct Response: Decodable { let aborting: Bool }
        let result: Response = try await mutate(id, suffix: "runs/" + run + "/abort", input: [String: String]())
        guard result.aborting else { throw ClientError.invalidPayload }
    }

    func sessions() async throws -> [SessionExcerpt] {
        struct List: Decodable { let sessions: [WireSessionInfo]? }
        let list: List = try await get("v1/sessions")
        return try (list.sessions ?? []).map { try SessionProjection.summary($0) }
    }

    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }),
              let cursor = UInt64(before), cursor > 0 else { throw ClientError.invalidPayload }
        let page: WireTranscriptPage = try await get("v1/sessions/" + id + "/messages",
                                                   query: [URLQueryItem(name: "before", value: before)])
        return try TranscriptHistoryPage(page, sessionID: id, before: before)
    }

    private func wireSnapshot(_ id: String) async throws -> WireSessionSnapshot {
        guard !id.isEmpty, id.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }) else { throw ClientError.invalidPayload }
        let snapshot: WireSessionSnapshot = try await get("v1/sessions/" + id)
        guard snapshot.session.id == id, (snapshot.eventCursor ?? 0) >= 0 else { throw ClientError.invalidPayload }
        _ = try SessionProjection.snapshot(snapshot)
        return snapshot
    }

    func snapshot(_ id: String) async throws -> SessionExcerpt { try SessionProjection.snapshot(await wireSnapshot(id)) }

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

    private func watchStream(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        var snapshot = try await wireSnapshot(id)
        var projection = try SessionEventProjection(snapshot)
        await receive(projection.session)
        let stream = snapshot.eventStreamId ?? ""
        var cursor = snapshot.eventReplayAvailable == true ? snapshot.eventReplayFrom ?? 0 : snapshot.eventCursor ?? 0
        let query = [URLQueryItem(name: "stream", value: stream), URLQueryItem(name: "after", value: String(cursor))]
        let (bytes, response) = try await session.bytes(for: request("v1/sessions/\(id)/events/stream", query: query))
        defer { bytes.task.cancel() }
        try await withTaskCancellationHandler {
            try check(response)
            guard response.mimeType == "text/event-stream" else { throw ClientError.invalidPayload }
            var parser = SSEParser()
            for try await byte in bytes {
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
                for event in batch.events ?? [] where event.sequence > cursor {
                    refreshSubagents = refreshSubagents || event.kind.rawValue == "subagent.changed"
                    try projection.apply(event)
                    cursor = event.sequence
                    refresh = refresh || (event.sequence > (snapshot.eventCursor ?? 0)
                        && ["run.finished", "compaction.completed", "annotation.submitted"].contains(event.kind.rawValue))
                }
                cursor = updated
                await receive(projection.session)
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
                    guard snapshot.activeRunId == nil else { throw StreamResync() }
                    projection = try SessionEventProjection(snapshot, terminalError: projection.session.terminalError)
                    cursor = snapshot.eventCursor ?? cursor
                    await receive(projection.session)
                }
            }
            throw StreamResync()
        } onCancel: {
            // AsyncBytes may be waiting on an idle stream with no next byte to
            // observe Task cancellation. Close the request immediately.
            bytes.task.cancel()
        }
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
