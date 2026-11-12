import Foundation
import KitClient
import Observation

/// The client the session menu and command search act through: the model
/// catalog and configuration, compaction, rename, fork, delete, changing
/// folder, reloading context, and plugin commands.
typealias SessionActionsClient = ComposerClient & SessionCompactionClient & SessionNamingClient
    & SessionForkClient & SessionDeletionClient & SessionDisposalClient & SessionDirectoryClient
    & SessionReloadClient & PluginCommandClient

/// What the session menu can do with one session, and why an item is off.
/// Model and thinking changes and compaction go through the shared
/// `ComposerConfiguration` and `SessionCompactionOperation`, so the phone
/// settles them the way the Mac does.
@MainActor @Observable
final class SessionActions {
    /// A line shown above the composer after a menu action.
    struct Notice: Equatable {
        enum Tone { case info, danger }
        let text: String
        let tone: Tone
    }

    let identity: SessionIdentity
    let configuration = ComposerConfiguration()
    let compaction = SessionCompactionOperation()
    let fork = SessionForkOperation()
    let reload = SessionReloadOperation()
    /// A folder change. One that may have reached the server is kept, so it's
    /// retried as the same change, never sent again as another.
    let directory = DirectoryChangeOperation()
    private let client: any SessionActionsClient

    private(set) var latest: SessionExcerpt?
    /// Whether the session's stream is live. Nothing changes without it.
    var live = false
    private(set) var renaming = false
    private(set) var renameError: String?
    private(set) var deleting = false
    private(set) var deleteError: String?
    /// Set by each menu action, which replaces the previous notice.
    private var noticeDismissed = true
    /// Which action's outcome the notice tells.
    private var noticeSubject = NoticeSubject.configuration
    private enum NoticeSubject { case configuration, compaction, reload, plugin }
    /// The plugin command running, by name. One runs at a time.
    private(set) var runningPlugin: String?
    /// How the last plugin command ended, when it needs saying.
    private var pluginOutcome: Notice?
    @ObservationIgnored private var pluginRun = UUID()
    /// Hands a fresh snapshot to the session's feed after a change.
    @ObservationIgnored var deliver: (@MainActor (SessionExcerpt) -> Void)?

    init(identity: SessionIdentity, client: any SessionActionsClient) {
        self.identity = identity
        self.client = client
    }

    /// Takes in each snapshot the session's stream delivers.
    func reconcile(_ snapshot: SessionExcerpt) { latest = snapshot }

    // MARK: Model and thinking

    var model: String { latest?.model ?? "" }
    var thinking: String { latest?.thinking ?? "" }

    /// The model's display name: its catalog name, or its ID without the provider.
    var modelName: String { Self.modelName(model, models: configuration.models) }

    static func modelName(_ id: String, models: [WireModelCapability]) -> String {
        models.first(where: { $0.id == id })?.name ?? id.split(separator: "/").last.map(String.init) ?? id
    }

    /// The catalog grouped by provider in the order the server lists it.
    var modelGroups: [(provider: String, models: [WireModelCapability])] {
        var groups: [(provider: String, models: [WireModelCapability])] = []
        for model in configuration.models {
            if let index = groups.firstIndex(where: { $0.provider == model.provider }) {
                groups[index].models.append(model)
            } else {
                groups.append((model.provider, [model]))
            }
        }
        return groups
    }

    /// The levels the session's model offers. Empty hides the Thinking item.
    var thinkingLevels: [String] {
        ComposerConfiguration.thinkingLevels(model: model, models: configuration.models)
    }

    /// Why the model can't change now, or nil when it can. The server takes a
    /// new model only between turns.
    var modelLockedReason: String? {
        if !live { return "Reconnect to change" }
        if configuration.changing { return "Changing…" }
        if directory.pending { return "Wait for the folder change" }
        if latest?.activeRunID != nil || latest?.activeBashID != nil { return "Changes when the turn ends" }
        if compaction.pending || latest?.activeCompactionID != nil { return "Wait for compaction" }
        if latest?.configurationRevision == nil { return "Unavailable" }
        return nil
    }

    /// Why the thinking level can't change now. It can change mid-turn; the
    /// next request uses it.
    var thinkingLockedReason: String? {
        if !live { return "Reconnect to change" }
        if configuration.changing { return "Changing…" }
        if directory.pending { return "Wait for the folder change" }
        if compaction.pending || latest?.activeCompactionID != nil { return "Wait for compaction" }
        if latest?.configurationRevision == nil { return "Unavailable" }
        return nil
    }

    /// Loads the model catalog once, or again after it failed.
    func loadModels() async {
        guard configuration.models.isEmpty else { return }
        await configuration.load(client: client)
    }

    func changeModel(_ id: String) {
        guard modelLockedReason == nil, id != model, let session = latest else { return }
        let level = ComposerConfiguration.thinkingLevel(switchingTo: id, from: thinking, models: configuration.models)
        apply(session: session, model: id, thinking: level)
    }

    func changeThinking(_ level: String) {
        guard thinkingLockedReason == nil, level != thinking, let session = latest else { return }
        apply(session: session, model: model, thinking: level)
    }

    private func apply(session: SessionExcerpt, model: String, thinking: String) {
        speak(.configuration)
        compaction.dismissFeedback()
        Task { await configuration.change(client: client, session: session, model: model, thinking: thinking, refresh: refresh) }
    }

    // MARK: Compaction

    /// Why the context can't be compacted now, or nil when it can.
    var compactionUnavailableReason: String? {
        if !live { return "Reconnect to compact" }
        if compaction.pending || latest?.activeCompactionID != nil { return "Compacting…" }
        if latest?.activeRunID != nil || latest?.activeBashID != nil { return "Available when the turn ends" }
        if configuration.changing || directory.pending { return "Wait for the current change" }
        return nil
    }

    /// Whether the context is being compacted, by this phone or elsewhere.
    var compacting: Bool { compaction.pending || latest?.activeCompactionID != nil }

    func compact() {
        guard compactionUnavailableReason == nil else { return }
        speak(.compaction)
        compaction.beginNewIfResolved()
        Task {
            await compaction.perform(session: identity.session, client: client, refresh: refresh)
        }
    }

    // MARK: Notice

    /// What the last model, thinking, or compaction change came to, when it
    /// needs saying. A manual compaction always reports how it ended: its
    /// summary is model context, so the transcript doesn't show it.
    var notice: Notice? {
        guard !noticeDismissed else { return nil }
        switch noticeSubject {
        case .configuration:
            return configuration.error.map { Notice(text: "Couldn’t change the model. \($0)", tone: .danger) }
        case .compaction: return compactionNotice
        case .reload: return reloadNotice
        case .plugin: return pluginOutcome
        }
    }

    private var compactionNotice: Notice? {
        guard compaction.showsFeedback, !compaction.pending else { return nil }
        if let error = compaction.error { return Notice(text: "Couldn’t compact. \(error)", tone: .danger) }
        if let error = compaction.refreshError { return Notice(text: "Compacted, but couldn’t refresh. \(error)", tone: .danger) }
        switch compaction.result?.compacted {
        case false?: return Notice(text: "Not enough turns to compact yet.", tone: .info)
        case true?: return Notice(text: "Context compacted.", tone: .info)
        case nil: return nil
        }
    }

    func dismissNotice() {
        noticeDismissed = true
        compaction.dismissFeedback()
    }

    /// Shows the notice for the action just started, in place of the last one.
    private func speak(_ subject: NoticeSubject) {
        noticeSubject = subject
        noticeDismissed = false
        if subject != .compaction { compaction.dismissFeedback() }
    }

    // MARK: Folder

    /// Why the session's folder can't change now, or nil when it can. The
    /// server changes it only between turns.
    var folderUnavailableReason: String? {
        if !live && directory.request == nil { return "Reconnect to change" }
        if directory.pending { return "Changing…" }
        if latest?.activeRunID != nil || latest?.activeBashID != nil { return "Available when the turn ends" }
        if compacting { return "Wait for compaction" }
        if reload.pending { return "Wait for the reload" }
        if runningPlugin != nil { return "Wait for the command" }
        if configuration.changing { return "Wait for the current change" }
        return nil
    }

    /// Moves the session to another folder, as `cd` would: a path can be
    /// absolute, start with `~`, or be relative to the current folder.
    /// Returns whether it moved; the error stays for the sheet.
    func changeFolder(to path: String) async -> Bool {
        guard folderUnavailableReason == nil else { return false }
        // A new action replaces the last one's notice.
        dismissNotice()
        return await directory.perform(path: path, session: identity.session, client: client, refresh: refresh)
    }

    // MARK: Reload

    /// Why the session's context can't be reloaded now, or nil when it can.
    var reloadUnavailableReason: String? {
        if !live { return "Reconnect to reload" }
        if reload.pending { return "Reloading…" }
        if directory.pending { return "Wait for the folder change" }
        if compacting { return "Wait for compaction" }
        if configuration.changing { return "Wait for the current change" }
        return nil
    }

    /// Reads the session's instructions, prompt commands, and agents again.
    /// A reload that may have reached the server is never repeated on its own.
    func reloadContext() {
        guard reloadUnavailableReason == nil else { return }
        speak(.reload)
        let session = identity.session, client = client
        Task { await reload.perform(session: session, client: client, refresh: refresh) }
    }

    private var reloadNotice: Notice? {
        guard !reload.pending else { return nil }
        if let error = reload.error { return Notice(text: "Couldn’t reload context. \(error)", tone: .danger) }
        guard let result = reload.result else { return nil }
        let issues = (result.warnings ?? []) + (result.diagnostics ?? []).map(\.message)
        if let first = issues.first {
            let more = issues.count > 1 ? " (+\(issues.count - 1) more)" : ""
            return Notice(text: "Reloaded context with issues: \(first)\(more)", tone: .danger)
        }
        if let error = reload.refreshError { return Notice(text: "Reloaded context, but couldn’t refresh. \(error)", tone: .danger) }
        return Notice(text: "Context reloaded.", tone: .info)
    }

    // MARK: Plugin commands

    /// Why a plugin command can't run now, or nil when one can.
    var pluginUnavailableReason: String? {
        if !live { return "Reconnect to run" }
        if runningPlugin != nil { return "A command is running" }
        if directory.pending { return "Wait for the folder change" }
        return nil
    }

    /// Runs a plugin command from this session's catalog. Its effects are
    /// the plugin's, so a failure is reported and never retried.
    func runPlugin(_ command: PluginCommand, args: String) {
        guard pluginUnavailableReason == nil else { return }
        guard (latest?.pluginCommands ?? []).contains(where: { $0.id == command.id && $0.instance == command.instance }) else {
            speak(.plugin)
            pluginOutcome = Notice(text: "\(command.localID) is no longer available. Choose it again.", tone: .danger)
            return
        }
        speak(.plugin)
        pluginOutcome = nil
        runningPlugin = command.localID
        let run = UUID(), session = identity.session, client = client
        pluginRun = run
        Task {
            var outcome: Notice?
            do {
                try await client.executePluginCommand(session, input: .init(id: command.id, instance: command.instance, args: args))
            } catch let failure as PluginCommandFailure where failure.unavailable {
                outcome = Notice(text: "\(command.localID) is no longer available. Choose it again.", tone: .danger)
            } catch {
                outcome = Notice(text: "\(command.localID) failed. \(PluginCommandFailure.detail(for: error))", tone: .danger)
            }
            guard pluginRun == run else { return }
            runningPlugin = nil
            pluginOutcome = outcome
        }
    }

    // MARK: Rename, fork, delete

    var title: String { latest?.title ?? "" }

    /// Whether this session lives only in the server's memory. Deleting it disposes it.
    var temporary: Bool { latest?.isTemporary == true }

    /// Renames the session. Returns whether it worked; the error stays for the sheet.
    func rename(to name: String) async -> Bool {
        guard !renaming else { return false }
        guard let name = SessionName.normalized(name) else {
            renameError = "Use a name without control characters, up to 256 bytes."
            return false
        }
        renaming = true; renameError = nil
        defer { renaming = false }
        do {
            let renamed = try await client.renameSession(identity.session, name: name)
            if renamed.id == identity.session { try? await refresh() }
            return true
        } catch {
            renameError = error.localizedDescription
            try? await refresh()
            return false
        }
    }

    func clearRenameError() { renameError = nil }

    /// Why the session can't be forked now, or nil when it can.
    var forkUnavailableReason: String? {
        if !live { return "Reconnect to fork" }
        if temporary { return "Temporary sessions can’t be forked" }
        return nil
    }

    /// Forks the session. A first message that couldn't start returns to the
    /// fork's composer when it opens.
    func fork(name: String, message: String) async -> SessionIdentity? {
        guard forkUnavailableReason == nil,
              let forked = await fork.submit(client: client, source: identity.session, name: name, message: message) else { return nil }
        let child = SessionIdentity(server: identity.server, session: forked.session.id)
        if let error = forked.firstTurnError {
            ForkFirstTurnFailures.shared.record(
                .init(message: message.trimmingCharacters(in: .whitespacesAndNewlines), error: error), for: child)
        }
        return child
    }

    /// What asks before deleting: the menu item, the alert's title, message,
    /// and button. A temporary session is disposed instead.
    struct DeleteConfirmation: Equatable {
        let item: String
        let title: String
        let message: String
        let button: String

        init(item: String, title: String, message: String, button: String) {
            self.item = item
            self.title = title
            self.message = message
            self.button = button
        }

        init(session title: String, temporary: Bool) {
            let name = "“\(title.isEmpty ? "This session" : title)”"
            self = temporary
                ? DeleteConfirmation(
                    item: "Dispose…", title: "Dispose temporary session?",
                    message: "\(name) will be removed from the server’s memory and any active work will be cancelled. This can’t be undone.",
                    button: "Dispose session")
                : DeleteConfirmation(
                    item: "Delete…", title: "Delete session?",
                    message: "\(name) and its saved history will be permanently deleted. This can’t be undone.",
                    button: "Delete session")
        }
    }

    var deleteConfirmation: DeleteConfirmation { DeleteConfirmation(session: title, temporary: temporary) }

    /// Deletes the session, or disposes it when it is temporary. A lost
    /// answer is settled by reading the session, never by deleting again.
    func delete() async -> Bool {
        guard !deleting else { return false }
        deleting = true; deleteError = nil
        defer { deleting = false }
        do {
            if temporary {
                do { try await client.disposeSession(identity.session) }
                catch ClientError.http(404) {}
            } else {
                try await SessionDeletion.perform(identity.session, client: client)
            }
            return true
        } catch {
            if case ClientError.http(409) = error {
                deleteError = "This session is in use and can’t be deleted. Stop its work and try again."
            } else {
                deleteError = error.localizedDescription
            }
            return false
        }
    }

    func clearDeleteError() { deleteError = nil }

    // MARK: Refresh

    /// Reads the session again and hands it to the feed.
    private func refresh() async throws {
        let snapshot = try await client.snapshot(identity.session)
        reconcile(snapshot)
        deliver?(snapshot)
    }
}
