import Foundation

/// Wire values are projected explicitly into the existing native transcript UI.
enum SessionProjection {
    static func summary(_ info: WireSessionInfo, messages: [TranscriptMessage] = []) throws -> SessionExcerpt {
        guard !info.id.isEmpty, info.cwd.hasPrefix("/"), !info.model.isEmpty,
              ["off", "minimal", "low", "medium", "high", "xhigh", "max"].contains(info.thinkingLevel),
              let configurationRevision = UInt64(exactly: info.configurationRevision) else { throw ClientError.invalidPayload }
        var session = SessionExcerpt(id: info.id, title: info.name.flatMap { $0.isEmpty ? nil : $0 } ?? "Untitled session",
            parentSessionID: info.parentSessionId, parentSessionName: info.parentSessionName,
            sourceTitle: "Kit server", model: info.model, thinking: info.thinkingLevel,
            workspace: URL(fileURLWithPath: info.cwd).lastPathComponent, cwd: info.cwd,
            date: info.createdAt, lastActivity: info.updatedAt, messages: messages,
            configurationRevision: configurationRevision)
        session.inputs = info.inputs
        return session
    }

    /// `carried` holds turn-end rows the live stream showed; see `transcript`.
    static func snapshot(_ snapshot: WireSessionSnapshot, carried: [TranscriptMessage] = []) throws -> SessionExcerpt {
        // A snapshot holds every result of the calls it holds, so a settled
        // turn's call without one never ran.
        // Boundaries that have not joined a completed turn follow the turns as
        // records without a turn or history sequence.
        let history = (snapshot.messages ?? []).filter { !$0.turnId.isEmpty }
        var session = try summary(snapshot.session, messages: transcript(snapshot.messages ?? [],
            settled: { $0 != snapshot.activeTurnId }, finished: { $0 != snapshot.activeTurnId }, carried: carried))
        session.historyCursor = try TranscriptHistoryPage.cursor(snapshot.previousMessageCursor,
            hasMore: snapshot.hasMoreMessages, firstSequence: history.first?.sequence)
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
        session.activeRunID = snapshot.activeTurnId
        session.observedTurns = Set(history.map(\.turnId)).sorted()
        session.followUps = try FollowUpState(snapshot.followUps)
        session.providerRetryAt = snapshot.providerRetry?.retryAt
        session.providerRetryCount = snapshot.providerRetry?.count
        let lastTurn = history.last?.turnId
        session.terminalError = snapshot.activeTurnId == nil
            ? history.last(where: { $0.role == "assistant" && $0.turnId == lastTurn })?.errorMessage : nil
        session.historyStart = history.first?.sequence
        return session
    }

    /// Projects stored messages into transcript rows. `settled` names turns
    /// whose calls without a result in `source` never ran; history pages pass
    /// none, because a call's result may lie in a later page.
    ///
    /// `finished` names turns that are over, which close with a row when they
    /// didn't complete. A turn may store errors from attempts it retried, so
    /// only its last reply's error says how it ended. `carried` holds rows the
    /// live stream already showed for a turn; they win over the stored reason
    /// so a refresh doesn't relabel them.
    static func transcript(_ source: [WireTranscriptMessage],
                           settled: (String) -> Bool = { _ in false },
                           finished: (String) -> Bool = { _ in false },
                           carried: [TranscriptMessage] = []) throws -> [TranscriptMessage] {
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
        var lastIndex: [String: Int] = [:]
        var lastReply: [String: WireTranscriptMessage] = [:]
        for (index, message) in source.enumerated() where !message.turnId.isEmpty {
            lastIndex[message.turnId] = index
            if message.role == "assistant" { lastReply[message.turnId] = message }
        }
        var carriedByTurn: [String: TranscriptMessage] = [:]
        for row in carried where row.id.hasPrefix(turnEndPrefix) {
            carriedByTurn[String(row.id.dropFirst(turnEndPrefix.count))] = row
        }
        func flush() {
            guard let first = pending.first else { return }
            messages.append(TranscriptMessage(id: "tools-" + first.id, role: "tools", text: "", tools: pending))
            pending.removeAll()
        }
        /// Closes a finished turn after its last stored record.
        func endTurn(_ turn: String) {
            guard finished(turn) else { return }
            let row: TranscriptMessage
            if let live = carriedByTurn.removeValue(forKey: turn) {
                row = live
            } else if let reply = lastReply[turn],
                      let error = reply.errorMessage?.trimmingCharacters(in: .whitespacesAndNewlines), !error.isEmpty {
                row = turnEndRow(turn: turn, end: reply.stopReason == "aborted" ? .stopped : .failed, message: error)
            } else { return }
            flush()
            messages.append(row)
        }
        for (index, message) in source.enumerated() {
            defer { if !message.turnId.isEmpty, lastIndex[message.turnId] == index { endTurn(message.turnId) } }
            // Results enrich the original call; they do not create transcript rows.
            if message.role == "tool", let id = message.toolCallId, calls.contains(id) { continue }
            if turnID != message.turnId { flush(); turnID = message.turnId }
            // Context the user should notice becomes a marker; context meant
            // only for the model stays in server history without a row.
            if message.role == "context", !["bash", "plugin_message"].contains(message.boundaryKind) {
                flush()
                if let row = contextRow(message) { messages.append(row) }
                continue
            }
            if message.role == "context", message.boundaryKind == "plugin_message" {
                guard let plugin = message.boundarySource, !plugin.isEmpty else { throw ClientError.invalidPayload }
                flush()
                messages.append(TranscriptMessage(id: message.id, role: "plugin", text: visibleText(message.content ?? []), tools: [],
                    plugin: PluginMessageOrigin(pluginID: plugin, turnID: message.turnId)))
                continue
            }
            if message.role == "tool", let id = message.toolCallId {
                pending.append(ToolActivity(id: id, name: message.toolName ?? "Tool", summary: message.toolName ?? "Tool",
                    output: visibleText(message.content ?? []), arguments: nil, failed: message.isError ?? false, attachments: attachments(message.content ?? []),
                    details: json(message.details)))
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
                            thinking: thinking.isEmpty ? nil : thinking, attachments: attachments(result?.content ?? []),
                            details: json(result?.details), notRun: result == nil && settled(message.turnId) ? true : nil))
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
        // A live row whose turn stored nothing still closes the transcript.
        for (turn, row) in carriedByTurn.sorted(by: { $0.key < $1.key }) where finished(turn) { messages.append(row) }
        return messages
    }

    /// Structured JSON as compact text, or nil when absent.
    static func json(_ value: WireJSON?) -> String? {
        guard let value, case .object = value,
              let data = try? JSONEncoder().encode(value) else { return nil }
        return String(data: data, encoding: .utf8)
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
            // A prompt command reads, and is recalled, as the invocation the
            // user typed; its expansion is model input only.
            case "promptCommand": return block.promptCommandInvocation
            default: return nil
            }
        }.filter { !$0.isEmpty }.joined(separator: "\n\n")
    }
}
