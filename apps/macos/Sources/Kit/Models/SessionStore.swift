import AppKit
import Foundation
import Observation

@MainActor @Observable
final class SessionStore {
    @ObservationIgnored private let pluginNotifications = PluginNotificationWatch()
    var pluginCommands: [PluginCommand] { selected?.pluginCommands ?? [] }
    private(set) var pluginCommandRunning = false
    /// The plugin command the activity card shows as running.
    private(set) var runningPluginCommand: String?
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
            feedback.show(title: "/" + command.name + " is no longer available",
                          detail: "The command list changed. Choose it again from the palette.", tone: .warning)
            return
        }
        // The palette refuses these arguments before it closes.
        guard PaletteCommand(id: "", name: "", description: "", plugin: command).argumentProblem(args) == nil else { return }
        let session = selectedID, token = UUID()
        pluginCommandToken = token
        pluginCommandRunning = true
        runningPluginCommand = command.name
        let task = Task { try await client.executePluginCommand(session, input: .init(id: command.id, instance: command.instance, args: args)) }
        pluginCommandTask = task
        do {
            try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
            guard pluginCommandToken == token, selectedID == session else { return }
        } catch {
            guard pluginCommandToken == token, selectedID == session else { return }
            let detail = if let notSent = error as? MutationNotSent {
                "It wasn't sent: " + notSent.reason
            } else if (error as? PluginCommandFailure)?.unavailable == true {
                "The command list changed. Choose it again from the palette."
            } else {
                "Its effects may have partially completed. It was not retried."
            }
            feedback.show(title: "/" + command.name + " failed", detail: detail, tone: .error, persistent: true)
        }
        guard pluginCommandToken == token else { return }
        pluginCommandTask = nil
        pluginCommandRunning = false
        runningPluginCommand = nil
    }
    private func cancelPluginCommandWait() {
        pluginCommandToken = UUID()
        pluginCommandTask?.cancel()
        pluginCommandTask = nil
        pluginCommandRunning = false
        runningPluginCommand = nil
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
        if running { return "idle only" }
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
        defer { operation.dismissFeedback() }
        // A compaction that ran shows as its transcript marker.
        if detail == nil, operation.result?.compacted != false { return }
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
        if running { return "idle only" }
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

    var isTemporary: Bool { TemporarySessions.shared.contains(server: serverID, session: selectedID) }
    var scratchpadClient: (any ScratchpadClient)? { catalogClient as? any ScratchpadClient }
    /// Closes a workspace tab, saving the scratchpad first so a draft isn't lost.
    func closePane(_ pane: WorkspacePane) {
        let workspace = ui.workspace
        if pane == .scratchpad, let client = scratchpadClient {
            Task { await workspace.closeScratchpad(client: client, session: selectedID) }
        } else if let terminal = workspace.terminal(pane) {
            // A terminal running a command asks first.
            TerminalCloseConfirmation.confirm(terminal.needsConfirmClose ? [terminal] : [], scope: .tab,
                                              in: terminal.window ?? NSApp.keyWindow) { workspace.close(pane) }
        } else { workspace.close(pane) }
    }

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
    struct MCPStatus: Equatable {
        var servers: [MCPServerStatus]
        var warnings: [String]
    }
    /// Status read while the MCP view is visible; the session stream does not
    /// carry MCP changes, so other readers see the latest stream snapshot.
    private var polledMCPStatus: (session: String, status: MCPStatus)?
    var mcpStatus: MCPStatus {
        if let polled = polledMCPStatus, polled.session == selectedID { return polled.status }
        return MCPStatus(servers: selected?.mcpServers ?? [], warnings: selected?.mcpWarnings ?? [])
    }
    /// Polls MCP status for the selected session until cancelled.
    func monitorMCPStatus(interval: Duration = .seconds(2)) async {
        let client = replica.client, id = selectedID
        guard !client.isDemo, !id.isEmpty else { return }
        defer { if polledMCPStatus?.session == id { polledMCPStatus = nil } }
        while !Task.isCancelled {
            if let snapshot = try? await client.snapshot(id), !Task.isCancelled, selectedID == id {
                let status = MCPStatus(servers: snapshot.mcpServers ?? [], warnings: snapshot.mcpWarnings ?? [])
                if polledMCPStatus?.session != id || polledMCPStatus?.status != status { polledMCPStatus = (id, status) }
            }
            do { try await Task.sleep(for: interval) } catch { return }
        }
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
            feedback.show(title: "Model refresh failed", detail: error.localizedDescription, tone: .error,
                          actionTitle: "Retry") { [weak self] in Task { await self?.refreshModelCatalog() } }
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

    /// Child configuration state is keyed by durable conversation so a recreated
    /// conversation for the same agent starts without stale feedback.
    @ObservationIgnored private var subagentConfigurations: [String: [String: SubagentConfigurationOperation]] = [:]
    func subagentConfiguration(_ conversation: String) -> SubagentConfigurationOperation {
        if let value = subagentConfigurations[selectedID]?[conversation] { return value }
        let value = SubagentConfigurationOperation()
        subagentConfigurations[selectedID, default: [:]][conversation] = value
        return value
    }
    var canConfigureSubagent: Bool {
        subagentClient is any SubagentConfigurationClient && !unavailable && connectionState == .connected
    }
    /// Thinking may change while a child runs; a model change requires it to
    /// have no active or queued work. Values are read from the current roster.
    func configureSubagent(_ name: String, model: String? = nil, thinking: String? = nil) async {
        let session = selectedID
        guard canConfigureSubagent, model != nil || thinking != nil,
              let agent = selected?.subagents?.items.first(where: { $0.name == name }),
              let conversation = agent.conversationID, let generation = agent.generation, generation > 0,
              let client = subagentClient as? any SubagentConfigurationClient else { return }
        if model != nil && agent.hasOutstandingWork { return }
        let model = model == agent.model ? nil : model
        let thinking = thinking == agent.thinkingLevel ? nil : thinking
        guard model != nil || thinking != nil else { return }
        await subagentConfiguration(conversation).change(session: session, conversation: conversation,
            generation: generation, agent: name, model: model, thinking: thinking, client: client,
            outstandingWork: { [weak self] in
                self?.selected?.subagents?.items.first(where: { $0.conversationID == conversation })?.hasOutstandingWork ?? false
            }) { [weak self] in
                guard let self, self.selectedID == session else { throw CancellationError() }
                _ = try await self.replica.resynchronize()
            }
    }

    @ObservationIgnored private var subagentSends: [String: [String: SubagentSendOperation]] = [:]
    func subagentSend(_ name: String) -> SubagentSendOperation {
        if let value = subagentSends[selectedID]?[name] { return value }
        let value = SubagentSendOperation()
        subagentSends[selectedID, default: [:]][name] = value
        return value
    }
    /// Direct messaging is off: a subagent's result is delivered to the parent
    /// agent, not back to the user who messaged it.
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
    /// The session's watch has stopped and won't retry on its own, as after
    /// an error a server restart can cause.
    var watchStopped: Bool {
        guard !unavailable else { return false }
        switch connectionState {
        case .failed, .authenticationFailed: return true
        // A store is disconnected only until its watch starts or after it is released.
        case .disconnected, .unavailable, .loading, .connected, .retrying: return false
        }
    }
    var isDemo: Bool { demo != nil }
    var sessions: [SessionExcerpt] { replica.sessions }
    var refreshingSessions: Bool { replica.refreshingSessions }
    var catalogError: String? { replica.catalogError }
    var historyLoading: Bool { replica.historyLoading }
    var historyError: String? { replica.historyError }
    var hasEarlierHistory: Bool { selected?.historyCursor != nil }
    var hasLoadedHistory: Bool { replica.hasLoadedHistory }
    /// How long the reader stays at the live bottom before loaded history is released.
    @ObservationIgnored var historyReleaseDelay: Duration = .seconds(30)
    func loadHistory(beforePrepend: @escaping @MainActor () -> Void = {}) {
        replica.loadHistory(beforePrepend: beforePrepend)
    }
    /// Releases pages loaded before the recent snapshot window, along with
    /// the row interaction state that belonged to them.
    func releaseLoadedHistory() {
        guard replica.hasLoadedHistory else { return }
        replica.releaseLoadedHistory()
        ui.transcript.retainDrawers(for: replica.messages)
    }
    var selectedID: String { replica.selectedID }
    /// Selected session metadata. Its `messages` and `activity` are always
    /// empty; read ``messages`` and ``activity`` so streaming updates
    /// invalidate only the views that present them.
    var selected: SessionExcerpt? { replica.metadata }
    /// The complete selected session, including transcript rows. Views should
    /// prefer ``selected`` and ``messages``; this observes every replica change.
    var selectedSnapshot: SessionExcerpt? { replica.snapshot }
    var messages: [TranscriptMessage] {
        bashOperation.merge(into: demo?.messages ?? replica.messages)
    }
    var activity: String? { replica.activity }
    /// The running turn's streamed thinking, for the activity card.
    var model: String { demo?.model ?? selected?.model ?? "" }
    /// The active model's label when its session inputs are known to exclude images.
    var imageRejectingModel: String? {
        Self.imageRejectingModel(inputs: selected?.inputs, model: model, models: configuration.models)
    }
    static func imageRejectingModel(inputs: [String]?, model: String, models: [WireModelCapability]) -> String? {
        guard let inputs, !inputs.contains("image") else { return nil }
        return ComposerView.activeModelLabel(id: model, models: models)
    }
    /// Staged images the active model cannot receive; they block sending until removed.
    var hasRejectedImages: Bool { imageRejectingModel != nil && ui.uploads.items.contains(where: \.isImage) }
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
    /// Authorization requests whose page has opened by itself.
    @ObservationIgnored private var openedAuthorizations: Set<String> = []
    var interactions: [InteractionFlow] { unavailable ? [] : demo?.interactions ?? liveInteractions[selectedID] ?? [] }
    /// Keeps an answered request docked briefly so a plugin's next one replaces it.
    let interactionHold = InteractionHold()
    /// The request in the composer's place: the oldest pending one, or the
    /// answered one still held.
    var dockedInteraction: InteractionFlow? {
        interactions.first ?? (unavailable ? nil : interactionHold.flow)
    }
    private func reconcileInteractions(_ session: SessionExcerpt) {
        let previous = liveInteractions[session.id] ?? []
        liveInteractions[session.id] = (session.pendingInteractions ?? []).sorted { $0.createdAt < $1.createdAt }.compactMap { request in
            previous.first { $0.request?.id == request.id } ?? (try? InteractionFlow(request: request))
        }
        guard session.id == selectedID else { return }
        if liveInteractions[session.id]?.isEmpty == false { interactionHold.clear(); return }
        // A held request returns focus to the composer when its hold ends.
        if !previous.isEmpty, interactionHold.flow == nil { ui.composerFocus += 1 }
    }

    init(fixture: Fixture, client: (any SessionClient)? = nil, sessionID: String? = nil) {
        let client = client ?? FixtureClient(fixture: fixture)
        replica = SessionReplica(sessions: fixture.sessions, client: client)
        if let sessionID, !sessionID.isEmpty { replica.select(sessionID) }
        ui = SessionUIState(demo: client.isDemo)
        demo = client.isDemo ? DemoSessionState() : nil
        ui.workspace.feedback = feedback(for: replica.selectedID)
        demo?.select(selectedSnapshot)
        operationsBySession[replica.selectedID] = SessionOperations()
        configurations[replica.selectedID] = ComposerConfiguration()
        if let cwd = replica.snapshot?.cwd { knownDirectories[replica.selectedID] = cwd }
        replica.onReceive = { [weak self] session in
            self?.feedback(for: session.id).observe(session, manualCompaction: self?.compactions[session.id]?.pending == true) {
                [weak self] in
                guard let self, self.selectedID == session.id else { return }
                self.ui.subagentsPresented = true
                self.ui.palette = true
            }
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
        interactionHold.clear()
        cancelPluginCommandWait()
        pluginNotifications.stop()
        feedback.clearTransientPluginNotifications()
        ui.switchSession(from: SessionIdentity(server: replica.client.serverID, session: selectedID),
                         to: SessionIdentity(server: replica.client.serverID, session: id), demo: isDemo)
        ui.workspace.feedback = feedback(for: id)
        if operationsBySession[id] == nil { operationsBySession[id] = SessionOperations() }
        if configurations[id] == nil { configurations[id] = ComposerConfiguration() }
        replica.select(id)
        demo?.select(selectedSnapshot)
        applyAcknowledgedDraft()
        attach()
    }

    var forkClient: (any SessionForkClient)? { replica.client as? any SessionForkClient }
    var forkUnavailableReason: String? {
        if isDemo || forkClient == nil { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected { return "Connect to fork" }
        if isTemporary { return "Temporary sessions cannot be forked" }
        // A fork copies the latest settled boundary, so active turns and
        // compaction do not block it; a reload replaces the session runtime.
        if reloadOperation.pending { return "Wait for session reload" }
        return nil
    }

    /// Whether `/new` can create a session from this one. When it can't,
    /// the command opens the launcher instead.
    var canCreateSibling: Bool {
        !isDemo && !unavailable && replica.client is any SessionCreationClient
            && selected?.cwd?.isEmpty == false && !model.isEmpty
    }
    private(set) var siblingCreationPending = false
    var newSessionUnavailableReason: String? {
        siblingCreationPending ? "Session creation is already in progress" : nil
    }
    /// Creates a session in this session's working directory with its model
    /// and thinking level, as the TUI's `/new` does. A failure is reported
    /// as feedback, whose Retry creates it again and then calls `open`, and
    /// returns nil.
    func createSibling(then open: (@MainActor (SessionExcerpt) -> Void)? = nil) async -> SessionExcerpt? {
        guard canCreateSibling, !siblingCreationPending,
              let client = replica.client as? any SessionCreationClient,
              let cwd = selected?.cwd else { return nil }
        let model = model, thinking = thinking
        siblingCreationPending = true
        defer { siblingCreationPending = false }
        do {
            let created = try await client.createSession(WireCreateSessionInput(id: nil, cwd: cwd, name: nil, model: model,
                thinkingLevel: thinking.isEmpty ? nil : thinking, temporary: false))
            open?(created)
            return created
        } catch {
            feedback.show(title: "New session failed", detail: error.localizedDescription, tone: .error,
                          actionTitle: "Retry") { [weak self] in Task { _ = await self?.createSibling(then: open) } }
            return nil
        }
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
        applyForkFirstTurnFailure()
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
    /// Reads the session through `client`, the same server under a new name,
    /// keeping the tab's drafts, terminals, and layout, and reattaches.
    func rebind(to client: any SessionClient) {
        let old = serverID
        detach()
        replica.rebind(to: client)
        ui.serverRenamed(from: old, to: client.serverID)
        attach()
    }

    /// Ends the session tab's work, including its browser pages and shells.
    func stop() {
        demo?.stop()
        ui.closeLiveTabs()
    }

    /// Whether the tab's server runs on this Mac, as its connection last
    /// verified; nil until it does. The window sets it.
    @ObservationIgnored var serverIsThisMac: @MainActor () -> Bool? = { nil }

    /// Whether this tab can use features that work on this Mac's files and
    /// programs, such as terminals.
    var thisMacGate: ThisMacGate { ThisMacGate(thisMac: isDemo ? false : serverIsThisMac()) }

    /// Whether the tab offers a terminal, available or dimmed: it shows a session.
    var offersTerminal: Bool { !isDemo && !unavailable && selected != nil }

    /// Why a terminal can't open, such as "needs this Mac", while the tab
    /// offers one.
    var terminalUnavailableReason: String? { offersTerminal ? thisMacGate.reason : "Session unavailable" }

    /// Where a new terminal starts: the session's working directory. Nil
    /// unless the server runs on this Mac, since a terminal runs here.
    var terminalDirectory: String? {
        guard offersTerminal, thisMacGate == .available, let session = selected else { return nil }
        return session.cwd ?? session.workspace
    }

    /// Terminals in this session tab whose commands are still running.
    var busyTerminals: [TerminalTab] { ui.workspaces.flatMap(\.busyTerminals) }
    func replay() { ui.notice = ""; demo?.replay() }
    func previewModel(_ value: String) { demo?.model = value }
    func previewThinking(_ value: String) { demo?.thinking = value }
    func previewInteractions(_ flows: [InteractionFlow]) { demo?.interactions = flows }

    /// Opens an authorization page beside the conversation. The redirect
    /// it ends at answers the request, so nothing is pasted, even when
    /// another session is shown by then. Returns false when the request has
    /// no redirect Kit can receive.
    func openAuthorization(_ flow: InteractionFlow) -> Bool {
        guard let url = flow.authorizationURL, let redirect = flow.authorizationRedirect else { return false }
        let session = selectedID
        ui.workspace.openAuthorization(url, redirect: redirect, beside: .conversation) { [weak self, weak flow] received in
            // A request no longer pending lets the page load the redirect.
            guard let self, let flow, self.liveInteractions[session]?.contains(where: { $0 === flow }) == true else { return false }
            // Otherwise the redirect is the answer: sent now, or left in the
            // dock's field when it can't be.
            if flow.receiveRedirect(received) { self.respond(to: flow, in: session, cancelled: false) }
            return true
        }
        return true
    }

    /// Opens a docked authorization's page the first time it's shown, so
    /// the user needn't click Open. Closing the page doesn't reopen it.
    func openAuthorizationOnce(_ flow: InteractionFlow) {
        guard !isDemo, !flow.submitting, let id = flow.request?.id, flow.authorizationRedirect != nil,
              interactions.first === flow, !openedAuthorizations.contains(id), openAuthorization(flow) else { return }
        openedAuthorizations.insert(id)
    }

    func finishInteraction(cancelled: Bool) {
        if !isDemo {
            if let flow = interactions.first { respond(to: flow, in: selectedID, cancelled: cancelled) }
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
    /// Sends a request's answer for session `id`. Returns false when there
    /// is nothing to send or no client to send it with.
    @discardableResult
    private func respond(to flow: InteractionFlow, in id: String, cancelled: Bool) -> Bool {
        guard let client = composerClient, !flow.submitting, let input = flow.response(cancelled: cancelled) else { return false }
        flow.submitting = true; flow.error = nil
        Task { [weak self] in
            do {
                try await client.respond(id, input: input)
                guard let self, self.selectedID == id else { flow.submitting = false; return }
                // Held before resynchronizing, so the dock never empties
                // while a plugin's next request is on its way. It stays in
                // its sending state until something replaces it.
                self.interactionHold.hold(flow) { [weak self] in
                    guard let self, self.selectedID == id else { return }
                    if self.interactions.isEmpty { self.ui.composerFocus += 1 }
                }
                _ = try? await self.replica.resynchronize()
            } catch {
                flow.error = error.localizedDescription
                flow.submitting = false
                if let self, self.selectedID == id { _ = try? await self.replica.resynchronize() }
            }
        }
        return true
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

    var palettePromptUnavailableReason: String? {
        if !(catalogClient is any PromptCommandClient) { return "Unavailable for this connection" }
        if unavailable { return "Session unavailable" }
        if connectionState != .connected { return "Connect to run" }
        // During a turn a prompt command joins the follow-up queue.
        if operations.sending || operations.uncertain || operations.queuePending { return "Wait for the current message" }
        if directoryChange.pending || reloadOperation.pending || compactionOperation.pending || configuration.changing {
            return "Wait for the current change"
        }
        return nil
    }

    func runPalettePrompt(_ command: PromptCommand, args: String) {
        guard let client = catalogClient as? any PromptCommandClient,
              palettePromptUnavailableReason == nil else { return }
        guard selected?.promptCommands?.contains(where: { $0.name == command.name && $0.location == command.location }) == true else {
            feedback.show(title: "/" + command.name + " is no longer available",
                          detail: "The command list changed. Choose it again from the palette.", tone: .warning)
            return
        }
        // The palette refuses these arguments before it closes.
        guard PaletteCommand(id: "", name: "", description: "", prompt: command).argumentProblem(args) == nil else { return }
        let id = selectedID, operation = operations
        let invocation = "/" + command.name + (args.isEmpty ? "" : " " + args)
        operation.submitCommand(client: client, session: id, input: .init(name: command.name, args: args),
            draft: .init(text: invocation, notes: [:], attachmentIDs: []),
            uncertain: { [weak self] in
                guard let self, self.selectedID == id else { return }
                self.recoverSubmission()
            }, acknowledged: {
                // This command did not originate in the composer; preserve its draft and attachments.
                operation.consumeAcknowledgedDraft()
            })
    }

    private func submitLive() {
        guard let client = mutationClient, connectionState == .connected else { return }
        guard !operations.queuePending, !operations.sending, !operations.uncertain else { return }
        guard ui.uploads.ready, !configuration.changing, !directoryChange.pending, !reloadOperation.pending, !compactionOperation.pending else { return }
        if hasRejectedImages, let model = imageRejectingModel {
            operations.reject("Remove images before sending. \(model) doesn’t accept images.")
            return
        }
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
        if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, attachmentIDs.isEmpty, annotationIDs.isEmpty {
            // As in the TUI, Return in an empty composer sends the queue to the running turn.
            if running, (operations.queue?.count ?? 0) > 0 { changeQueue(.promote) }
            return
        }
        let id = selectedID
        if let command = PromptCommand.invocation(ui.draft) {
            guard (selected?.promptCommands ?? []).contains(where: { $0.name == command.name }) else {
                operations.reject("Unknown prompt command: /" + command.name); return
            }
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

    /// Presents a fork's first message that could not start like any failed
    /// prompt submission: it returns to the composer with the failure shown.
    func applyForkFirstTurnFailure() {
        guard !isDemo, let failure = ForkFirstTurnFailures.shared.take(SessionIdentity(server: serverID, session: selectedID)) else { return }
        if ui.draft.isEmpty { ui.draft = failure.message }
        operations.fail(failure.error)
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
        operations.abort(client: client, session: selectedID, turn: run)
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
        // An opened file takes focus in its tile; a mention returns to the composer.
        if ui.filePickerIntent != "open" { ui.composerFocus += 1 }
    }
}
