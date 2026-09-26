import Foundation
import Observation

@MainActor @Observable
final class SessionStore {
    @ObservationIgnored private let pluginNotifications = PluginNotificationWatch()
    var pluginCommands: [PluginCommand] { selected?.pluginCommands ?? [] }
    private(set) var pluginCommandRunning = false
    @ObservationIgnored private var pluginCommandTask: Task<Void, Error>?
    @ObservationIgnored private var pluginCommandToken = UUID()
    var pluginCommandUnavailableReason: String? {
        if !(catalogClient is any PluginCommandClient) { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected { return "Connect to execute" }
        if pluginCommandRunning { return "Command running" }
        return nil
    }
    func runPluginCommand(_ command: PluginCommand, args: String) async {
        guard let client = catalogClient as? any PluginCommandClient else { return }
        guard pluginCommandUnavailableReason == nil else { return }
        guard pluginCommands.contains(where: { $0.id == command.id && $0.instance == command.instance }) else {
            feedback.show(title: "Plugin command changed", detail: "Reselect it from the refreshed catalog.", tone: .warning)
            return
        }
        guard args.utf8.count <= 64 * 1024, !args.contains("\0") else {
            feedback.show(title: "Invalid plugin command arguments", detail: "Arguments must fit within 64 KiB and contain no NUL.", tone: .error)
            return
        }
        let session = selectedID, token = UUID()
        pluginCommandToken = token
        pluginCommandRunning = true
        let task = Task { try await client.executePluginCommand(session, input: .init(id: command.id, instance: command.instance, args: args)) }
        pluginCommandTask = task
        let key = "plugin-command-progress"
        feedback.clear(key: key)
        feedback.show(key: key, title: "Running /" + command.name, detail: "Session reload interrupts this command.", persistent: true)
        do {
            try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
            guard pluginCommandToken == token, selectedID == session else { return }
            feedback.clear(key: key)
        } catch {
            guard pluginCommandToken == token, selectedID == session else { return }
            feedback.clear(key: key)
            let detail = (error as? PluginCommandFailure)?.errorDescription
                ?? "The command did not complete. Effects may have partially completed; it was not retried."
            feedback.show(title: "Plugin command failed", detail: detail, tone: .error, persistent: true)
        }
        guard pluginCommandToken == token else { return }
        pluginCommandTask = nil
        pluginCommandRunning = false
    }
    private func cancelPluginCommandWait() {
        pluginCommandToken = UUID()
        pluginCommandTask?.cancel()
        pluginCommandTask = nil
        pluginCommandRunning = false
        feedback.clear(key: "plugin-command-progress")
    }

    @ObservationIgnored private var feedbackBySession: [String: SessionFeedback] = [:]
    var feedback: SessionFeedback { feedback(for: selectedID) }
    private func feedback(for id: String) -> SessionFeedback {
        if let value = feedbackBySession[id] { return value }
        let value = SessionFeedback(); feedbackBySession[id] = value; return value
    }

    @ObservationIgnored private var compactions: [String: SessionCompactionOperation] = [:]
    var compactionOperation: SessionCompactionOperation {
        if let value = compactions[selectedID] { return value }
        let value = SessionCompactionOperation()
        compactions[selectedID] = value
        return value
    }
    var compactionUnavailableReason: String? {
        if !(catalogClient is any SessionCompactionClient) { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected { return "Connect to compact" }
        if running { return "Wait for the current turn" }
        if compactionOperation.pending || selected?.activeCompactionID != nil { return "Compaction in progress" }
        if directoryChange.pending || reloadOperation.pending || configuration.changing { return "Wait for the current change" }
        return nil
    }
    func compactSession() async {
        guard compactionUnavailableReason == nil, let client = catalogClient as? any SessionCompactionClient else { return }
        let id = selectedID
        let operation = compactionOperation
        feedback(for: id).clear(key: "compaction")
        await operation.perform(session: id, client: client) { [weak self] in
            guard let self, self.selectedID == id else { throw CancellationError() }
            _ = try await self.replica.resynchronize()
        }
        let detail = operation.error ?? operation.refreshError
        let feedback = feedback(for: id)
        let retry: (@MainActor () -> Void)?
        if detail != nil {
            retry = { [weak self] in
                guard let self, self.selectedID == id else { return }
                Task { await self.compactSession() }
            }
        } else { retry = nil }
        feedback.show(key: "compaction", title: operation.title, detail: detail ?? "", tone: detail == nil ? .info : .error,
                      persistent: detail != nil, actionTitle: detail == nil ? nil : (operation.result == nil ? "Retry" : "Refresh session"),
                      action: retry)
        operation.dismissFeedback()
    }

    @ObservationIgnored private var reloads: [String: SessionReloadOperation] = [:]
    var reloadOperation: SessionReloadOperation {
        if let value = reloads[selectedID] { return value }
        let value = SessionReloadOperation()
        reloads[selectedID] = value
        return value
    }
    var reloadUnavailableReason: String? {
        if !(catalogClient is any SessionReloadClient) { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected { return "Connect to reload" }
        if directoryChange.pending || compactionOperation.pending || configuration.changing { return "Wait for the current change" }
        if compactionOperation.pending { return "Wait for compaction" }
        if reloadOperation.pending { return "Reloading session context" }
        return nil
    }
    func reloadSession(for id: String, refreshOnly: Bool = false) async {
        guard selectedID == id, let client = catalogClient as? any SessionReloadClient else { return }
        let operation = reloadOperation
        guard !operation.pending, refreshOnly || reloadUnavailableReason == nil else { return }
        let feedback = feedback(for: id)
        feedback.clear(key: "session-reload")
        let refresh: @MainActor () async throws -> Void = { [weak self] in
            guard let self, self.selectedID == id else { throw CancellationError() }
            _ = try await self.replica.resynchronize()
        }
        if refreshOnly { await operation.refreshOnly(refresh) }
        else { await operation.perform(session: id, client: client, refresh: refresh) }
        // Cancellation does not undo an acknowledged server mutation. Always
        // publish its recorded result (or recovery error) to the owning session.
        guard let summary = SessionReloadFeedback.from(operation) else { return }
        let action: (@MainActor () -> Void)? = switch summary.recovery {
        case .refresh: { [weak self] in Task { await self?.reloadSession(for: id, refreshOnly: true) } }
        case .reload: { [weak self] in Task { await self?.reloadSession(for: id) } }
        case nil: nil
        }
        let actionTitle: String? = switch summary.recovery {
        case .refresh: "Refresh session"
        case .reload: "Reload again"
        case nil: nil
        }
        feedback.show(key: "session-reload", title: summary.title, detail: summary.detail,
                      tone: summary.tone, persistent: summary.persistent,
                      actionTitle: actionTitle, action: action, reloadReport: summary.report)
    }

    @ObservationIgnored private var knownDirectories: [String: String] = [:]
    @ObservationIgnored private var directories: [String: DirectoryChangeOperation] = [:]
    var directoryChange: DirectoryChangeOperation {
        if let value = directories[selectedID] { return value }
        let value = DirectoryChangeOperation()
        directories[selectedID] = value
        return value
    }
    var workspaceFiles: [String] { replica.workspaceFiles }
    var directoryUnavailableReason: String? {
        if !(catalogClient is any SessionDirectoryClient) { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected && directoryChange.request == nil { return "Connect to change directory" }
        if compactionOperation.pending { return "Wait for compaction" }
        if reloadOperation.pending { return "Wait for session reload" }
        if running { return "Wait for the current turn" }
        return nil
    }
    func changeDirectory(_ path: String) async -> Bool {
        guard directoryUnavailableReason == nil, let client = catalogClient as? any SessionDirectoryClient else { return false }
        let id = selectedID
        return await directoryChange.perform(path: path, session: id, client: client) { [weak self] in
            guard let self, self.selectedID == id else { throw CancellationError() }
            try await self.replica.refreshWorkspace()
        }
    }

    func syncBashFeedback() {
        let operation = bashOperation, id = selectedID
        guard let error = operation.error else { feedback.clear(key: "shell-error"); return }
        guard !operation.pending else { return }
        let retry: (@MainActor () -> Void)?
        if operation.unresolved {
            retry = { [weak self] in
                guard let self, self.selectedID == id else { return }
                self.retryBash()
            }
        } else { retry = nil }
        feedback.show(key: "shell-error", title: "Shell command needs attention", detail: error,
                      tone: .error, persistent: true,
                      actionTitle: operation.unresolved ? "Retry shell command" : nil, action: retry)
    }

    var isTemporary: Bool { TemporarySessions.shared.contains(server: serverID, session: selectedID) }
    var scratchpadClient: (any ScratchpadClient)? { catalogClient as? any ScratchpadClient }
    var canOpenScratchpad: Bool { (isDemo || scratchpadClient != nil) && !isTemporary && !unavailable }
    @ObservationIgnored private var annotationStates: [String: AnnotationState] = [:]
    var annotationState: AnnotationState {
        if let value = annotationStates[selectedID] { return value }
        let value = AnnotationState()
        value.observe(selected?.annotations ?? [])
        annotationStates[selectedID] = value
        return value
    }
    var catalogClient: any SessionClient { replica.client }
    func sessionDeleted(_ identity: SessionIdentity) { replica.sessionDeleted(identity) }
    private let replica: SessionReplica
    private let demo: DemoSessionState?
    let ui: SessionUIState
    @ObservationIgnored private var operationsBySession: [String: SessionOperations] = [:]
    private let emptyOperations = SessionOperations()
    var operations: SessionOperations { operationsBySession[selectedID] ?? emptyOperations }
    @ObservationIgnored private var shells: [String: BashOperation] = [:]
    var bashOperation: BashOperation {
        if let value = shells[selectedID] { return value }
        let value = BashOperation(); shells[selectedID] = value; return value
    }
    var bashClient: (any BashClient)? { replica.client as? any BashClient }
    var activeBashID: String? { bashOperation.active?.id ?? selected?.activeBashID }
    var shellMode: BashDraft? { BashDraft(ui.draft) }
    func sendBash() {
        guard let client = bashClient, connectionState == .connected, !unavailable,
              activeBashID == nil, !reloadOperation.pending, !compactionOperation.pending,
              !directoryChange.pending else { return }
        let id = selectedID, operation = bashOperation, draft = ui.draft, anchor = messages.last?.id
        Task { [weak self] in
            await operation.submit(draft, session: id, client: client, after: anchor) { text in
                guard let self, self.selectedID == id, self.ui.draft == text else { return }
                self.ui.draft = ""
            }
        }
    }
    func retryBash() {
        guard let client = bashClient else { return }
        let session = selectedID, operation = bashOperation
        Task { [weak self] in
            await operation.retry(session: session, client: client) { text in
                if let self, self.selectedID == session, self.ui.draft == text { self.ui.draft = "" }
            }
        }
    }
    func stopBash() {
        guard let client = bashClient, let id = activeBashID else { return }
        let session = selectedID, operation = bashOperation
        Task { await operation.abort(session: session, id: id, client: client) }
    }
    func monitorBash() async {
        guard let client = bashClient else { return }
        let session = selectedID, operation = bashOperation
        while !Task.isCancelled {
            await operation.refresh(session: session, client: client)
            if operation.unresolved {
                await operation.resolve(session: session, client: client) { text in
                    if self.selectedID == session, self.ui.draft == text { self.ui.draft = "" }
                }
            }
            do { try await Task.sleep(for: .seconds(1)) } catch { return }
        }
    }
    var mutationClient: (any SessionMutationClient)? { replica.client as? any SessionMutationClient }
    var attachmentClient: (any AttachmentClient)? { replica.client as? any AttachmentClient }
    var composerClient: (any ComposerClient)? { replica.client as? any ComposerClient }
    var composerHistoryClient: (any ComposerHistoryClient)? { replica.client as? any ComposerHistoryClient }
    @ObservationIgnored private var configurations: [String: ComposerConfiguration] = [:]
    private let emptyConfiguration = ComposerConfiguration()
    private(set) var modelCatalogRefreshPending = false
    var configuration: ComposerConfiguration { configurations[selectedID] ?? emptyConfiguration }
    var thinkingLevels: [String] {
        configuration.models.first(where: { $0.id == model })?.thinkingLevels?.map(\.rawValue) ?? [thinking]
    }
    func loadComposer() async {
        guard let client = composerClient else { return }
        await configuration.load(client: client)
    }
    func refreshModelCatalog() async {
        guard !modelCatalogRefreshPending,
              let client = catalogClient as? any ModelCatalogRefreshClient else { return }
        modelCatalogRefreshPending = true
        defer { modelCatalogRefreshPending = false }
        do {
            let models = try await client.refreshModels()
            emptyConfiguration.replaceCatalog(models)
            for configuration in configurations.values { configuration.replaceCatalog(models) }
            feedback.show(title: "Model catalog refreshed", detail: "\(models.count) models available")
        } catch is CancellationError {
        } catch {
            feedback.show(title: "Model refresh failed", detail: error.localizedDescription, tone: .error)
        }
    }
    func configure(model: String? = nil, thinking: String? = nil) async {
        guard !unavailable, let client = composerClient, let selected else { return }
        let target = model ?? self.model
        let levels = configuration.models.first(where: { $0.id == target })?.thinkingLevels?.map(\.rawValue) ?? []
        let level = thinking ?? (levels.contains(self.thinking) ? self.thinking : levels.first ?? "off")
        await configuration.change(client: client, session: selected, model: target, thinking: level) { [weak self] in
            guard let self, self.selectedID == selected.id else { throw CancellationError() }
            _ = try await self.replica.resynchronize()
        }
    }
    var unavailable: Bool { replica.unavailable }
    var running: Bool { !unavailable && selected?.activeRunID != nil }

    @ObservationIgnored private var subagentDismissals: [String: SubagentDismissalOperation] = [:]
    var subagentDismissal: SubagentDismissalOperation {
        if let value = subagentDismissals[selectedID] { return value }
        let value = SubagentDismissalOperation()
        subagentDismissals[selectedID] = value
        return value
    }
    var canDismissSubagent: Bool {
        subagentClient is any SubagentDismissalClient && !unavailable && connectionState == .connected && !subagentDismissal.pending
    }
    func dismissSubagent(_ agent: SubagentRoster.Item, session: String) async {
        guard selectedID == session, canDismissSubagent,
              let conversation = agent.conversationID, let generation = agent.generation,
              let client = subagentClient as? any SubagentDismissalClient else { return }
        let workspace = ui.workspace
        let dismissed = await subagentDismissal.dismiss(session: session, conversation: conversation,
            generation: generation, client: client) { [weak self] in
                guard let self, self.selectedID == session else { throw CancellationError() }
                _ = try await self.replica.resynchronize()
            }
        guard dismissed else { return }
        subagentSends[session]?[agent.name] = nil
        // A newly created conversation for the same name must keep its pane.
        if selectedID != session || selected?.subagents?.items.first(where: { $0.name == agent.name })?.conversationID == nil ||
            selected?.subagents?.items.first(where: { $0.name == agent.name })?.conversationID == conversation {
            workspace.close(.agent(agent.name))
        }
    }

    @ObservationIgnored private var subagentSends: [String: [String: SubagentSendOperation]] = [:]
    func subagentSend(_ name: String) -> SubagentSendOperation {
        if let value = subagentSends[selectedID]?[name] { return value }
        let value = SubagentSendOperation()
        subagentSends[selectedID, default: [:]][name] = value
        return value
    }
    // Direct human-to-subagent messaging is intentionally deferred.
    static let subagentMessagingEnabled = false
    var canSendSubagent: Bool {
        Self.subagentMessagingEnabled && subagentClient is any SubagentMessagingClient && !isTemporary && !unavailable && connectionState == .connected
    }
    func sendToSubagent(_ name: String, conversation: String?) async {
        guard canSendSubagent, let client = subagentClient as? any SubagentMessagingClient else { return }
        let id = selectedID
        await subagentSend(name).send(session: id, agent: name, conversation: conversation, client: client) { [weak self] in
            guard let self, self.selectedID == id else { throw CancellationError() }
            _ = try await self.replica.resynchronize()
        }
    }

    var subagentClient: (any SubagentClient)? { replica.client as? any SubagentClient }
    var serverID: String { replica.client.serverID }
    var isDemo: Bool { demo != nil }
    var sessions: [SessionExcerpt] { replica.sessions }
    var refreshingSessions: Bool { replica.refreshingSessions }
    var catalogError: String? { replica.catalogError }
    var historyLoading: Bool { replica.historyLoading }
    var historyError: String? { replica.historyError }
    var hasEarlierHistory: Bool { selected?.historyCursor != nil }
    func loadHistory(beforePrepend: @escaping @MainActor () -> Void = {}) {
        replica.loadHistory(beforePrepend: beforePrepend)
    }
    var selectedID: String { replica.selectedID }
    var selected: SessionExcerpt? { replica.snapshot }
    var messages: [TranscriptMessage] {
        bashOperation.merge(into: demo?.messages ?? selected?.messages ?? [])
    }
    var model: String { demo?.model ?? selected?.model ?? "" }
    var thinking: String { demo?.thinking ?? selected?.thinking ?? "" }
    var connectionState: SessionConnectionState { replica.connectionState }
    var connectionStatus: String { replica.connectionStatus }
    var replaying: Bool { demo?.replaying ?? false }
    var notice: String { ui.notice.isEmpty ? demo?.notice ?? "" : ui.notice }
    var tabStatus: SessionTabStatus {
        if isDemo { return approval ? .awaitingResponse : replaying ? .running : .idle }
        guard connectionState == .connected else { return .idle }
        if activeBashID != nil { return .running }
        return selected?.tabStatus ?? .idle
    }
    var approval: Bool { !interactions.isEmpty }
    private var liveInteractions: [String: [InteractionFlow]] = [:]
    var interactions: [InteractionFlow] { unavailable ? [] : demo?.interactions ?? liveInteractions[selectedID] ?? [] }
    private func reconcileInteractions(_ session: SessionExcerpt) {
        let previous = liveInteractions[session.id] ?? []
        liveInteractions[session.id] = (session.pendingInteractions ?? []).sorted { $0.createdAt < $1.createdAt }.compactMap { request in
            previous.first { $0.request?.id == request.id } ?? (try? InteractionFlow(request: request))
        }
        if session.id == selectedID, !previous.isEmpty, liveInteractions[session.id]?.isEmpty == true { ui.composerFocus += 1 }
    }

    init(fixture: Fixture, client: (any SessionClient)? = nil, sessionID: String? = nil) {
        let client = client ?? FixtureClient(fixture: fixture)
        replica = SessionReplica(sessions: fixture.sessions, client: client)
        if let sessionID, !sessionID.isEmpty { replica.select(sessionID) }
        ui = SessionUIState(demo: client.isDemo)
        demo = client.isDemo ? DemoSessionState() : nil
        demo?.select(replica.snapshot)
        operationsBySession[replica.selectedID] = SessionOperations()
        configurations[replica.selectedID] = ComposerConfiguration()
        if let cwd = replica.snapshot?.cwd { knownDirectories[replica.selectedID] = cwd }
        replica.onReceive = { [weak self] session in
            self?.feedback(for: session.id).observe(session, manualCompaction: self?.compactions[session.id]?.pending == true)
            if let self, !self.isDemo {
                SessionAttention.shared.observe(session, server: self.serverID)
            }
            if let self, let cwd = session.cwd {
                if let previous = self.knownDirectories[session.id], previous != cwd, self.selectedID == session.id {
                    self.ui.workspace.invalidateDirectory()
                }
                self.knownDirectories[session.id] = cwd
                if self.selectedID == session.id { self.ui.workspace.filePreviews.observe(session) }
            }
            for row in session.messages { if let bash = row.bash { self?.shells[session.id]?.record(bash) } }
            self?.annotationStates[session.id]?.observe(session.annotations ?? [])
            if let self, session.id == self.selectedID, let scratchpad = session.scratchpad {
                self.ui.workspace.scratchpadState.observe(scratchpad)
            }
            self?.operationsBySession[session.id]?.reconcile(session)
            self?.reconcileInteractions(session)
        }
    }

    func select(_ id: String) {
        guard id != selectedID else { return }
        cancelPluginCommandWait()
        pluginNotifications.stop()
        feedback.clearTransientPluginNotifications()
        ui.switchSession(from: SessionIdentity(server: replica.client.serverID, session: selectedID),
                         to: SessionIdentity(server: replica.client.serverID, session: id), demo: isDemo)
        if operationsBySession[id] == nil { operationsBySession[id] = SessionOperations() }
        if configurations[id] == nil { configurations[id] = ComposerConfiguration() }
        replica.select(id)
        demo?.select(selected)
        applyAcknowledgedDraft()
        attach()
    }

    var forkClient: (any SessionForkClient)? { replica.client as? any SessionForkClient }
    var forkUnavailableReason: String? {
        if isDemo || forkClient == nil { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected { return "Connect to fork" }
        if isTemporary { return "Temporary sessions cannot be forked" }
        if compactionOperation.pending { return "Wait for compaction" }
        if reloadOperation.pending { return "Wait for session reload" }
        if running { return "Wait for the current turn" }
        return nil
    }

    var renameUnavailableReason: String? {
        if isDemo || !(replica.client is any SessionNamingClient) { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected { return "Connect to rename" }
        return nil
    }
    func renameSession(_ name: String) async throws { try await replica.rename(name) }

    func refreshSessions() async { await replica.refreshSessions() }
    func attach() {
        let id = selectedID
        pluginNotifications.start(session: id, client: catalogClient as? any PluginNotificationClient) { [weak self] notification in
            guard let self, self.selectedID == id, !self.unavailable else { return }
            self.feedback.showPluginNotification(notification)
        }
        if operations.uncertain { recoverSubmission() }
        else { replica.attach() }
    }
    func detach() {
        cancelPluginCommandWait()
        pluginNotifications.stop()
        feedback.clearTransientPluginNotifications()
        replica.detach()
    }
    func stop() { demo?.stop() }
    func replay() { ui.notice = ""; demo?.replay() }
    func previewModel(_ value: String) { demo?.model = value }
    func previewThinking(_ value: String) { demo?.thinking = value }
    func previewInteractions(_ flows: [InteractionFlow]) { demo?.interactions = flows }

    func finishInteraction(cancelled: Bool) {
        if !isDemo {
            guard let client = composerClient, let flow = interactions.first, !flow.submitting,
                  let input = flow.response(cancelled: cancelled) else { return }
            let id = selectedID
            flow.submitting = true; flow.error = nil
            Task { [weak self] in
                do {
                    try await client.respond(id, input: input)
                    if let self, self.selectedID == id { _ = try await self.replica.resynchronize() }
                } catch {
                    flow.error = error.localizedDescription
                    if let self, self.selectedID == id { _ = try? await self.replica.resynchronize() }
                }
                flow.submitting = false
            }
            return
        }
        guard let demo, let flow = demo.interactions.first else { return }
        demo.interactions.removeFirst()
        ui.notice = cancelled ? "Request cancelled" : "Response recorded locally"
        if !cancelled {
            demo.append(TranscriptMessage(id: UUID().uuidString, role: "user", text: flow.summary, tools: []))
        }
        if demo.interactions.isEmpty { ui.composerFocus += 1 }
    }
    func send() {
        if !isDemo, shellMode != nil { sendBash(); return }
        guard let demo else { submitLive(); return }
        guard !ui.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !ui.workspace.notes.isEmpty || !ui.attachments.isEmpty else { return }
        stop()
        let suffix = ui.attachments.isEmpty ? "" : "\n\nAttachments: " + ui.attachments.joined(separator: ", ")
        let review = ui.workspace.notes.isEmpty ? "" : "\n\nReview notes:\n" + ui.workspace.notes.sorted(by: { $0.key < $1.key }).map { "- \($0.key): \($0.value)" }.joined(separator: "\n")
        demo.append(TranscriptMessage(id: UUID().uuidString, role: "user", text: ui.draft + suffix + review, tools: []))
        demo.append(TranscriptMessage(id: UUID().uuidString, role: "preview", text: "Prompt added to this preview. No model request was sent.", tools: []))
        ui.draft = ""
        ui.attachments = []
        ui.attachmentPreviews = [:]
        ui.workspace.notes = [:]
        ui.notice = "Local preview only"
    }

    private func submitLive() {
        guard let client = mutationClient, connectionState == .connected else { return }
        guard !operations.queuePending, !operations.sending, !operations.uncertain else { return }
        guard ui.uploads.ready, !configuration.changing, !directoryChange.pending, !reloadOperation.pending, !compactionOperation.pending else { return }
        guard ui.attachments.isEmpty else {
            operations.reject("Remove unavailable local attachments before sending.")
            return
        }
        let notes = ui.workspace.notes
        let review = notes.isEmpty ? "" : "\n\nReview notes:\n" + notes.sorted(by: { $0.key < $1.key }).map { "- \($0.key): \($0.value)" }.joined(separator: "\n")
        let attachmentIDs = ComposerAttachments.uniqueIDs(ui.serverAttachmentIDs + ui.uploads.readyIDs)
        guard attachmentIDs.count <= 8 else { operations.reject("A message can contain up to eight attachments."); return }
        let annotations = annotationState.records
        guard !annotationState.pending, !annotationState.uncertain, annotationState.editor == nil else {
            operations.reject("Finish the annotation change before sending."); return
        }
        guard annotations.count <= 64 else { operations.reject("A message can contain up to 64 annotations."); return }
        guard !annotations.contains(where: \.stale) else { operations.reject("Source changed. Replace or delete stale annotations before sending."); return }
        let annotationIDs = annotations.map(\.id)
        let text = ui.draft + review
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !attachmentIDs.isEmpty || !annotationIDs.isEmpty else { return }
        let id = selectedID
        if let command = PromptCommand.invocation(ui.draft) {
            guard (selected?.promptCommands ?? []).contains(where: { $0.name == command.name }) else {
                operations.reject("Unknown prompt command: /" + command.name); return
            }
            guard !running else { operations.reject("Wait for the current turn before running a prompt command."); return }
            guard attachmentIDs.isEmpty, notes.isEmpty, annotationIDs.isEmpty else {
                operations.reject("Prompt commands do not accept attachments or annotations."); return
            }
            guard let commandClient = catalogClient as? any PromptCommandClient else {
                operations.reject("Prompt commands are unavailable for this connection."); return
            }
            operations.submitCommand(client: commandClient, session: id,
                input: .init(name: command.name, args: command.args),
                draft: .init(text: ui.draft, notes: notes, attachmentIDs: attachmentIDs),
                uncertain: { [weak self] in
                    guard let self, self.selectedID == id else { return }
                    self.recoverSubmission()
                }, acknowledged: { [weak self] in
                    guard let self, self.selectedID == id else { return }
                    self.applyAcknowledgedDraft()
                })
            return
        }
        operations.submit(client: client, session: id,
            input: WirePromptInput(text: text, attachmentIds: attachmentIDs, annotationIds: annotationIDs),
            draft: .init(text: ui.draft, notes: notes, attachmentIDs: attachmentIDs),
            uncertain: { [weak self] in
                guard let self, self.selectedID == id else { return }
                self.recoverSubmission()
            }) { [weak self] in
                guard let self, self.selectedID == id else { return }
                self.applyAcknowledgedDraft()
            }
    }

    func recoverSubmission() {
        let id = selectedID
        operations.recover { [weak self] in
            guard let self, self.selectedID == id else { throw CancellationError() }
            return try await self.replica.resynchronize()
        }
    }

    private func applyAcknowledgedDraft() {
        guard let draft = operations.acknowledgedDraft else { return }
        if ui.draft == draft.text { ui.draft = "" }
        if ui.workspace.notes == draft.notes { ui.workspace.notes = [:] }
        ui.serverAttachmentIDs.removeAll { draft.attachmentIDs.contains($0) }
        ui.uploads.acknowledge(draft.attachmentIDs)
        operations.consumeAcknowledgedDraft()
    }

    func abortRun() {
        guard !unavailable, let client = mutationClient, let run = selected?.activeRunID else { return }
        operations.abort(client: client, session: selectedID, run: run)
    }

    func changeQueue(_ action: SessionOperations.QueueAction) {
        guard !unavailable, let client = mutationClient else { return }
        let id = selectedID
        operations.changeQueue(action, client: client, session: id) { [weak self] messages in
            guard let self else { return }
            let text = messages.map(\.text).joined(separator: "\n\n")
            let ids = messages.flatMap { $0.attachmentIds ?? [] }
            if self.selectedID == id {
                self.ui.draft = [text, self.ui.draft].filter { !$0.isEmpty }.joined(separator: "\n\n")
                self.ui.serverAttachmentIDs = ComposerAttachments.uniqueIDs(self.ui.serverAttachmentIDs + ids)
                self.ui.composerFocus += 1
            } else {
                self.ui.restoreQueueDraft(session: SessionIdentity(server: self.serverID, session: id), text: text, attachmentIDs: ids)
            }
        }
    }

    func showFiles(intent: String = "mention") {
        ui.filePickerIntent = intent
        ui.mentionQuery = ""
        ui.filePicker = true
    }

    func chooseFile(_ path: String) {
        if ui.filePickerIntent == "open" { ui.workspace.open(.file(path)) }
        else {
            // Picker insertion replaces a trailing @ token. Cursor-aware inline
            // completion is handled by the native composer.
            if let range = ui.draft.range(of: #"(^|\s)@[^\s]*$"#, options: .regularExpression) {
                let prefix = ui.draft[range].first?.isWhitespace == true ? " " : ""
                ui.draft.replaceSubrange(range, with: prefix + "@" + path + " ")
            } else { ui.draft += (ui.draft.isEmpty || ui.draft.last?.isWhitespace == true ? "" : " ") + "@" + path + " " }
        }
        ui.filePicker = false
        ui.composerFocus += 1
    }
}
