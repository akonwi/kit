import Foundation

/// Ordered live presentation layered on the last authoritative snapshot.
/// Pending prose is buffered by message and content index, never shown piecemeal.
struct SessionEventProjection {
    private(set) var session: SessionExcerpt
    private var text: [String: [Int: String]] = [:]
    private var thinking: [String: [Int: String]] = [:]
    private var userTurns: Set<String>
    private var lastTurn: String?
    private let persistedMessages: Set<String>
    private let persistedTools: Set<String>
    private var liveBytes = 0
    private var activeAssistant: String?
    private var activeRunID: String?
    private var pendingInteractions: Set<String> = []
    private var thinkingTools: [String: String] = [:]

    init(_ snapshot: WireSessionSnapshot, terminalError: String? = nil) throws {
        try self.init(session: SessionProjection.snapshot(snapshot), source: snapshot.messages ?? [],
            activeRunID: snapshot.activeRunId, replayAvailable: snapshot.eventReplayAvailable == true,
            pendingInteractions: Set((snapshot.pendingInteractions ?? []).filter { $0.runId == snapshot.activeRunId }.map(\.id)))
        if snapshot.activeRunId == nil && session.terminalError == nil { session.terminalError = terminalError }
    }

    init(session: SessionExcerpt, source: [WireTranscriptMessage], activeRunID: String? = nil,
         replayAvailable: Bool = true, pendingInteractions: Set<String> = []) throws {
        self.session = session
        self.activeRunID = activeRunID
        self.pendingInteractions = pendingInteractions
        self.session.tabStatus = Self.status(runID: activeRunID, pending: pendingInteractions)
        self.session.activity = session.activeCompactionID != nil ? "Compacting context…" : (activeRunID == nil ? nil : "Working…")
        userTurns = Set(source.filter { $0.role == "user" }.map(\.turnId))
        let pending = source.filter {
            $0.role == "assistant" && $0.turnId == activeRunID && ($0.stopReason ?? "").isEmpty
        }
        activeAssistant = pending.last?.id
        let pendingIDs = Set(pending.map(\.id))
        persistedMessages = Set(source.map(\.id)).subtracting(pendingIDs)
        self.session.messages.removeAll { pendingIDs.contains($0.id) && $0.role == "assistant" }
        if !replayAvailable {
            for message in pending {
                for (index, block) in (message.content ?? []).enumerated() {
                    if block.kind.rawValue == "text" { text[message.id, default: [:]][index] = block.text ?? "" }
                    if block.kind.rawValue == "thinking" { thinking[message.id, default: [:]][index] = block.text ?? "" }
                }
            }
        }
        persistedTools = Set(source.filter { $0.role == "tool" }.compactMap(\.toolCallId))
        lastTurn = source.last?.turnId
    }

    mutating func updateSubagents(_ snapshot: WireSessionSnapshot) throws {
        guard snapshot.session.id == session.id else { throw ClientError.invalidPayload }
        session.subagents = try SubagentRoster(snapshot)
    }

    mutating func apply(_ event: WireSessionEvent) throws {
        guard event.sessionId == session.id else { throw ClientError.invalidPayload }
        try apply(TranscriptEvent(event))
    }

    mutating func apply(_ event: TranscriptEvent) throws {
        liveBytes += (event.delta?.utf8.count ?? 0) + (event.text?.utf8.count ?? 0)
            + (event.thinking?.utf8.count ?? 0) + (event.arguments?.utf8.count ?? 0)
            + (event.content ?? []).reduce(0) { $0 + ($1.text?.utf8.count ?? 0) }
        guard liveBytes <= 32 * 1024 * 1024 else { throw ClientError.oversized }
        switch event.kind {
        case "annotation.created", "annotation.updated":
            guard let value = event.annotation else { throw ClientError.invalidPayload }
            let annotation = try FileAnnotation(value, session: session.id)
            var records = session.annotations ?? []
            records.removeAll { $0.id == annotation.id }
            records.append(annotation)
            guard records.count <= 128 else { throw ClientError.invalidPayload }
            session.annotations = records.sorted { $0.id < $1.id }
        case "annotation.deleted":
            guard let id = event.annotationId, id > 0 else { throw ClientError.invalidPayload }
            session.annotations?.removeAll { $0.id == id }
        case "annotation.submitted":
            guard let ids = event.annotationIds, ids.count <= 64, Set(ids).count == ids.count else { throw ClientError.invalidPayload }
            session.annotations?.removeAll { ids.contains($0.id) }
        case "run.started":
            session.activeCompactionID = nil
            if activeRunID != event.runId { pendingInteractions.removeAll(); session.pendingInteractions = [] }
            activeRunID = event.runId
            session.terminalError = nil
            session.activity = "Working…"
        case "interaction.requested":
            guard let interaction = event.interaction else { throw ClientError.invalidPayload }
            if interaction.runId == activeRunID {
                pendingInteractions.insert(interaction.id)
                var requests = session.pendingInteractions ?? []
                requests.removeAll { $0.id == interaction.id }
                requests.append(interaction)
                session.pendingInteractions = requests
            }
        case "interaction.resolved":
            guard let id = event.interactionId else { throw ClientError.invalidPayload }
            pendingInteractions.remove(id)
            session.pendingInteractions?.removeAll { $0.id == id }
        case "message.user":
            let annotations = try (event.content ?? []).flatMap { $0.annotations ?? [] }.map(FileAnnotation.init)
            let value = event.text ?? SessionProjection.visibleText(event.content ?? [])
            if userTurns.insert(event.turnId).inserted {
                session.messages.append(TranscriptMessage(id: "live-user-" + event.turnId, role: "user", text: value, tools: [], attachments: SessionProjection.attachments(event.content ?? []), annotations: annotations))
            }
            session.observedTurns = Array(Set((session.observedTurns ?? []) + [event.turnId]))
            lastTurn = event.turnId
        case "assistant.started", "assistant.text.delta", "assistant.thinking.delta":
            guard let id = event.messageId, !id.isEmpty else { throw ClientError.invalidPayload }
            activeAssistant = id
            guard !persistedMessages.contains(id) else { return }
            if event.kind == "assistant.started" {
                text[id] = event.text.map { [-1: $0] } ?? [:]
                thinking[id] = event.thinking.map { [-1: $0] } ?? [:]
            } else {
                guard let delta = event.delta, (event.contentIndex ?? 0) >= 0 else { throw ClientError.invalidPayload }
                if event.kind == "assistant.text.delta" {
                    text[id, default: [:]][event.contentIndex ?? 0, default: ""] += delta
                } else {
                    thinking[id, default: [:]][event.contentIndex ?? 0, default: ""] += delta
                }
            }
            if event.kind == "assistant.text.delta" { session.activity = "Working…" }
            else {
                let value = joined(thinking[id])
                session.activity = value.split(separator: "\n").last.map(String.init) ?? "Thinking…"
                if let toolID = thinkingTools[id],
                   let row = session.messages.firstIndex(where: { $0.tools.contains { $0.id == toolID } }),
                   let tool = session.messages[row].tools.firstIndex(where: { $0.id == toolID }) {
                    session.messages[row].tools[tool].thinking = value
                }
            }
        case "assistant.completed":
            guard let id = event.messageId, !id.isEmpty else { throw ClientError.invalidPayload }
            let value = event.text.flatMap { $0.isEmpty ? nil : $0 } ?? joined(text[id])
            if !persistedMessages.contains(id), !value.isEmpty {
                session.messages.append(TranscriptMessage(id: id, role: "assistant", text: value, tools: []))
            }
            if let finalThinking = event.thinking {
                thinking[id] = [-1: finalThinking]
                if let toolID = thinkingTools[id],
                   let row = session.messages.firstIndex(where: { $0.tools.contains { $0.id == toolID } }),
                   let tool = session.messages[row].tools.firstIndex(where: { $0.id == toolID }) {
                    session.messages[row].tools[tool].thinking = finalThinking
                }
            }
            text[id] = nil
            session.activity = "Working…"
            lastTurn = event.turnId
        case "tool.planned", "tool.started", "tool.updated", "tool.completed":
            guard let id = event.toolCallId, !id.isEmpty, let name = event.toolName, !name.isEmpty else { throw ClientError.invalidPayload }
            guard !persistedTools.contains(id) else { return }
            session.activity = "Working…"
            var row = session.messages.firstIndex { $0.tools.contains { $0.id == id } }
            if row == nil {
                if lastTurn == event.turnId, session.messages.last?.role == "tools" { row = session.messages.count - 1 }
                else {
                    session.messages.append(TranscriptMessage(id: "tools-" + id, role: "tools", text: "", tools: []))
                    row = session.messages.count - 1
                }
            }
            let index = row!
            let old = session.messages[index].tools.first { $0.id == id }
            let output = SessionProjection.visibleText(event.content ?? [])
            let completed = event.kind == "tool.completed"
            let assistant = event.messageId ?? activeAssistant
            var evidence = old?.thinking
            if let assistant, thinkingTools[assistant] == nil {
                let value = joined(thinking[assistant])
                if !value.isEmpty { evidence = value; thinkingTools[assistant] = id }
            }
            let value = ToolActivity(id: id, name: name, summary: name,
                output: completed ? output : (old?.output ?? "") + output,
                arguments: event.arguments ?? old?.arguments, failed: event.isError ?? old?.failed ?? false,
                status: completed ? "Completed" : event.kind == "tool.planned" ? "Planned" : "Running…",
                thinking: evidence,
                attachments: event.content == nil ? old?.attachments : SessionProjection.attachments(event.content ?? []),
                contentTruncated: event.contentTruncated ?? old?.contentTruncated)
            if let toolIndex = session.messages[index].tools.firstIndex(where: { $0.id == id }) {
                session.messages[index].tools[toolIndex] = value
            } else { session.messages[index].tools.append(value) }
            lastTurn = event.turnId
        case "provider.retry.scheduled":
            session.providerRetryAt = event.providerRetry?.retryAt
            session.providerRetryCount = event.providerRetry?.count
        case "provider.retry.started":
            session.providerRetryAt = nil
        case "run.finished":
            session.activeCompactionID = nil
            session.terminalError = event.errorMessage
            session.providerRetryAt = nil
            session.providerRetryCount = nil
            activeRunID = nil
            pendingInteractions.removeAll()
            session.pendingInteractions = []
            session.activity = nil
            text.removeAll(); thinking.removeAll(); thinkingTools.removeAll(); activeAssistant = nil
        case "compaction.started":
            session.activeCompactionID = event.compactionID
            session.activity = "Compacting context…"
        case "compaction.failed", "compaction.completed":
            if session.activeCompactionID == event.compactionID {
                session.activeCompactionID = nil
                session.activity = activeRunID == nil ? nil : "Working…"
            }
        case "usage.updated":
            guard let usage = event.usage else { throw ClientError.invalidPayload }
            session.usage = SessionUsage(usage)
        case "context.updated":
            session.contextTokens = event.contextTokens
            session.contextWindow = event.contextWindow
        case "session.renamed": session.title = event.sessionName ?? "Untitled session"
        default: break
        }
        session.activeRunID = activeRunID
        session.tabStatus = Self.status(runID: activeRunID, pending: pendingInteractions)
    }

    private static func status(runID: String?, pending: Set<String>) -> SessionTabStatus {
        guard runID != nil else { return .idle }
        return pending.isEmpty ? .running : .awaitingResponse
    }

    private func joined(_ blocks: [Int: String]?) -> String {
        (blocks ?? [:]).sorted { $0.key < $1.key }.map(\.value).joined()
    }
}
