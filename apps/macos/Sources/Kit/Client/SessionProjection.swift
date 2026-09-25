import Foundation

/// Wire values are projected explicitly into the existing native transcript UI.
enum SessionProjection {
    static func summary(_ info: WireSessionInfo, messages: [TranscriptMessage] = []) throws -> SessionExcerpt {
        guard !info.id.isEmpty, info.cwd.hasPrefix("/"), !info.model.isEmpty,
              ["off", "minimal", "low", "medium", "high", "xhigh", "max"].contains(info.thinkingLevel) else { throw ClientError.invalidPayload }
        return SessionExcerpt(id: info.id, title: info.name.flatMap { $0.isEmpty ? nil : $0 } ?? "Untitled session",
            parentSessionID: info.parentSessionId, parentSessionName: info.parentSessionName,
            sourceTitle: "Kit server", model: info.model, thinking: info.thinkingLevel,
            workspace: URL(fileURLWithPath: info.cwd).lastPathComponent, cwd: info.cwd,
            date: info.createdAt, lastActivity: info.updatedAt, messages: messages, configurationRevision: info.configurationRevision)
    }

    static func snapshot(_ snapshot: WireSessionSnapshot) throws -> SessionExcerpt {
        var session = try summary(snapshot.session, messages: transcript(snapshot.messages ?? []))
        for boundary in snapshot.pendingBoundaries ?? [] where boundary.kind == "bash" {
            guard let details = boundary.details else { throw ClientError.invalidPayload }
            let execution = try BashExecution.boundary(id: boundary.id, details: details, content: boundary.content ?? [])
            if !session.messages.contains(where: { $0.id == execution.id }) { session.messages.append(execution.message) }
        }
        session.historyCursor = try TranscriptHistoryPage.cursor(snapshot.previousMessageCursor,
            hasMore: snapshot.hasMoreMessages, firstSequence: snapshot.messages?.first?.sequence)
        session.annotations = try (snapshot.annotations ?? []).map(FileAnnotation.init)
        guard session.annotations!.count <= 128, Set(session.annotations!.map(\.id)).count == session.annotations!.count else { throw ClientError.invalidPayload }
        session.usage = SessionUsage(snapshot.usage)
        session.promptCommands = try PromptCommand.project(snapshot.promptCommands ?? [])
        session.pluginCommands = try PluginCommand.project(snapshot.pluginCommands ?? [])
        session.pluginFooter = try PluginFooterProjection.validate(snapshot.pluginFooter)
        session.contextTokens = snapshot.contextTokens
        session.contextWindow = snapshot.contextWindow
        session.subagents = try SubagentRoster(snapshot)
        session.subagentDiagnostics = snapshot.subagentDiagnostics ?? []
        session.pendingInteractions = snapshot.pendingInteractions ?? []
        session.scratchpad = try snapshot.scratchpad.map(ScratchpadRecord.init)
        session.mcpServers = try (snapshot.mcpServers ?? []).map(MCPServerStatus.init)
        guard session.mcpServers!.count <= 256, Set(session.mcpServers!.map(\.name)).count == session.mcpServers!.count else { throw ClientError.invalidPayload }
        session.mcpWarnings = snapshot.mcpWarnings ?? []
        guard session.mcpWarnings!.count <= 8, session.mcpWarnings!.allSatisfy({ $0.utf8.count <= 512 }) else { throw ClientError.invalidPayload }
        session.activeCompactionID = snapshot.activeCompaction?.id
        session.activeBashID = snapshot.activeBashExecutionId
        session.activeRunID = snapshot.activeRunId
        session.observedTurns = Array(Set((snapshot.messages ?? []).map(\.turnId)))
        session.followUps = try FollowUpState(snapshot.followUps)
        session.providerRetryAt = snapshot.providerRetry?.retryAt
        session.providerRetryCount = snapshot.providerRetry?.count
        session.terminalError = snapshot.activeRunId == nil ? snapshot.messages?.last(where: { $0.role == "assistant" })?.errorMessage : nil
        session.historyStart = snapshot.messages?.first?.sequence
        return session
    }

    static func transcript(_ source: [WireTranscriptMessage]) throws -> [TranscriptMessage] {
        var messages: [TranscriptMessage] = []
        var seen = Set<String>()
        var calls = Set<String>()
        var results: [String: WireTranscriptMessage] = [:]
        for message in source {
            guard !message.id.isEmpty, seen.insert(message.id).inserted,
                  ["user", "assistant", "tool", "context"].contains(message.role) else { throw ClientError.invalidPayload }
            for block in message.content ?? [] where block.kind.rawValue == "toolCall" {
                guard let id = block.toolCallId, block.toolName != nil else { throw ClientError.invalidPayload }
                calls.insert(id)
            }
            if message.role == "tool", let id = message.toolCallId { results[id] = message }
        }
        var pending: [ToolActivity] = []
        var turnID: String?
        func flush() {
            guard let first = pending.first else { return }
            messages.append(TranscriptMessage(id: "tools-" + first.id, role: "tools", text: "", tools: pending))
            pending.removeAll()
        }
        for message in source {
            // Results enrich the original call; they do not create transcript rows.
            if message.role == "tool", let id = message.toolCallId, calls.contains(id) { continue }
            if turnID != message.turnId { flush(); turnID = message.turnId }
            // Agent-to-agent context remains in server history, but is not a user-facing row.
            if message.role == "context", let kind = message.boundaryKind,
               ["subagent_result", "peer_query", "peer_result"].contains(kind) {
                flush()
                continue
            }
            if message.role == "tool", let id = message.toolCallId {
                pending.append(ToolActivity(id: id, name: message.toolName ?? "Tool", summary: message.toolName ?? "Tool",
                    output: visibleText(message.content ?? []), arguments: nil, failed: message.isError ?? false, attachments: attachments(message.content ?? [])))
            } else if let bash = try BashExecution.boundary(message) {
                flush()
                messages.append(bash.message)
            } else {
                if message.role != "assistant" { flush() }
                var visible: [WireTranscriptContent] = []
                var segment = 0
                var thinking = ""
                func flushText() throws {
                    let text = visibleText(visible)
                    let media = attachments(visible)
                    let annotations = try visible.flatMap { $0.annotations ?? [] }.map(FileAnnotation.init)
                    visible.removeAll()
                    guard !text.isEmpty || !media.isEmpty || !annotations.isEmpty else { return }
                    flush()
                    messages.append(TranscriptMessage(id: segment == 0 ? message.id : "\(message.id)-text-\(segment)",
                        role: message.role, text: text, tools: [], attachments: media, annotations: annotations))
                    segment += 1
                }
                for block in message.content ?? [] {
                    if block.kind.rawValue == "toolCall", let id = block.toolCallId, let name = block.toolName {
                        try flushText()
                        let result = results[id]
                        pending.append(ToolActivity(id: id, name: name, summary: name,
                            output: visibleText(result?.content ?? []), arguments: block.arguments, failed: result?.isError ?? false,
                            thinking: thinking.isEmpty ? nil : thinking, attachments: attachments(result?.content ?? [])))
                        thinking = ""
                    } else if block.kind.rawValue == "thinking" {
                        thinking += block.text ?? ""
                    } else {
                        visible.append(block)
                    }
                }
                try flushText()
            }
        }
        flush()
        return messages
    }

    static func attachments(_ content: [WireTranscriptContent]) -> [TranscriptAttachment] {
        content.filter { ["image", "file"].contains($0.kind.rawValue) }.map {
            TranscriptAttachment(id: $0.attachmentId, filename: $0.filename ?? "Attachment",
                                 mediaType: $0.mediaType, isImage: $0.kind.rawValue == "image")
        }
    }

    static func visibleText(_ content: [WireTranscriptContent]) -> String {
        content.compactMap { block -> String? in
            switch block.kind.rawValue {
            case "text": return block.text
            default: return nil
            }
        }.filter { !$0.isEmpty }.joined(separator: "\n\n")
    }
}
