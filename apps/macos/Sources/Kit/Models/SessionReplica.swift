import Foundation
import Observation

/// Read-only replica of server data. Only client responses replace its contents.
@MainActor @Observable
final class SessionReplica {
    var onReceive: (@MainActor (SessionExcerpt) -> Void)?
    let client: any SessionClient
    private(set) var sessions: [SessionExcerpt]
    private(set) var selectedID: String
    private(set) var snapshot: SessionExcerpt?
    private(set) var connectionState: SessionConnectionState = .disconnected
    var connectionStatus: String { connectionState.label }
    private(set) var unavailable = false
    private(set) var refreshingSessions = false
    private(set) var catalogError: String?
    private var titlesReceivedDuringRefresh: [String: String] = [:]
    private(set) var historyLoading = false
    private(set) var historyError: String?
    private var historyPrefix: [TranscriptMessage] = []
    private var historyBoundary: Int64?
    private var historyCursor: String?
    private var resetHistoryOnReceive = false
    private var historyGeneration = UUID()
    @ObservationIgnored private var historyTask: Task<Void, Never>?
    @ObservationIgnored private var watchTask: Task<Void, Never>?
    private var attachmentGeneration = UUID()
    private var titleRevision = 0
    @ObservationIgnored private var vcsTask: Task<Void, Never>?
    private var vcsRefreshPending = false
    private var vcsActivity: VCSActivity?

    private struct VCSActivity: Equatable {
        let cwd: String?
        let watch: String?
        let run: String?
        let bash: String?
        let message: String?
        let tools: [String]
        init(_ snapshot: SessionExcerpt) {
            watch = snapshot.watchGeneration
            cwd = snapshot.cwd; run = snapshot.activeRunID; bash = snapshot.activeBashID
            message = snapshot.messages.last?.id
            tools = snapshot.messages.suffix(2).flatMap(\.tools).map {
                $0.id + ":" + ($0.status ?? "") + ($0.failed ? ":failed" : "")
            }
        }
    }

    init(sessions: [SessionExcerpt], client: any SessionClient) {
        self.sessions = sessions
        self.client = client
        selectedID = sessions.first?.id ?? ""
        snapshot = sessions.first
        historyCursor = snapshot?.historyCursor
    }

    deinit {
        watchTask?.cancel()
        historyTask?.cancel()
        vcsTask?.cancel()
    }

    func select(_ id: String) {
        workspaceFiles = []
        detach()
        unavailable = false
        selectedID = id
        snapshot = sessions.first { $0.id == id }
        historyPrefix = []
        historyBoundary = nil
        historyCursor = snapshot?.historyCursor
        historyError = nil
    }

    func refreshSessions() async {
        guard !refreshingSessions else { return }
        refreshingSessions = true
        catalogError = nil
        titlesReceivedDuringRefresh = [:]
        defer {
            refreshingSessions = false
            titlesReceivedDuringRefresh = [:]
        }
        do {
            var catalog = try await TemporarySessions.shared.catalog(client: client)
            catalog.removeAll { deletedIDs.contains($0.id) }
            try Task.checkCancellation()
            // A rename delivered while the request was in flight is newer than
            // the catalog response. Keep its title without retaining deleted rows.
            for index in catalog.indices {
                if let title = titlesReceivedDuringRefresh[catalog[index].id] {
                    catalog[index].title = title
                }
            }
            sessions = catalog
            // Catalog summaries must never replace the active transcript or UI.
            if let summary = catalog.first(where: { $0.id == selectedID }) {
                snapshot?.title = summary.title
                if unavailable { attach() }
            } else if !selectedID.isEmpty, !client.isDemo {
                markUnavailable()
            }
        } catch is CancellationError {
            // A dismissed catalog does not produce failure feedback.
        } catch {
            catalogError = error.localizedDescription
        }
    }

    private(set) var workspaceFiles: [String] = []

    func refreshWorkspace() async throws {
        guard let composer = client as? any ComposerClient else { throw ClientError.invalidPayload }
        let id = selectedID
        let fresh = try await resynchronize()
        async let files = composer.files(id)
        let paths = try await files
        guard selectedID == id, snapshot?.cwd == fresh.cwd else { throw CancellationError() }
        workspaceFiles = paths
        requestVCSRefresh()
    }

    /// Coalesce invalidations without starving updates during an active run.
    private func requestVCSRefresh() {
        guard let client = client as? any SessionVCSClient, let cwd = snapshot?.cwd else { return }
        vcsRefreshPending = true
        guard vcsTask == nil else { return }
        let id = selectedID, generation = attachmentGeneration
        vcsTask = Task { [weak self] in
            while !Task.isCancelled {
                do { try await Task.sleep(for: .milliseconds(200)) } catch { return }
                guard let self, self.attachmentGeneration == generation else { return }
                self.vcsRefreshPending = false
                let result = try? await client.vcs(id)
                guard !Task.isCancelled, self.attachmentGeneration == generation,
                      self.selectedID == id else { return }
                if self.snapshot?.cwd == cwd {
                    let status = result?.sessionId == id && result?.cwd == cwd ? result?.status : nil
                    self.snapshot?.gitHead = status?.head.name ?? status?.head.oid.map { String($0.prefix(8)) }
                    self.snapshot?.gitHeadKind = status?.head.kind.rawValue
                    self.snapshot?.gitDirty = status?.dirty
                    if let snapshot = self.snapshot { self.onReceive?(snapshot) }
                }
                if !self.vcsRefreshPending {
                    self.vcsTask = nil
                    return
                }
                // A directory change invalidates the captured cwd as well as its result.
                if self.snapshot?.cwd != cwd {
                    self.vcsTask = nil
                    self.requestVCSRefresh()
                    return
                }
            }
        }
    }

    func rename(_ name: String) async throws {
        guard let client = client as? any SessionNamingClient, let name = SessionName.normalized(name),
              !unavailable, connectionState == .connected else { throw MutationNotSent(reason: "Connect to the session before renaming it.") }
        let id = selectedID
        let generation = attachmentGeneration
        let revision = titleRevision
        do {
            let renamed = try await client.renameSession(id, name: name)
            guard renamed.id == id else { throw ClientError.invalidPayload }
            // An event received during the request is newer than our starting title.
            // Never replace that event with a potentially delayed acknowledgement.
            guard generation == attachmentGeneration, selectedID == id, revision == titleRevision else { return }
            if let index = sessions.firstIndex(where: { $0.id == id }) { sessions[index].title = renamed.title }
            snapshot?.title = renamed.title
            titleRevision += 1
            if refreshingSessions { titlesReceivedDuringRefresh[id] = renamed.title }
            if let snapshot { onReceive?(snapshot) }
        } catch {
            guard generation == attachmentGeneration, selectedID == id else { throw error }
            let original = error
            // A lost acknowledgement may still have committed the rename.
            if let fresh = try? await resynchronize(), fresh.title == name { return }
            throw original
        }
    }

    func loadHistory(beforePrepend: @escaping @MainActor () -> Void = {}) {
        guard !unavailable, !historyLoading, let cursor = snapshot?.historyCursor,
              let pager = client as? any TranscriptPagingClient else { return }
        let id = selectedID
        let generation = historyGeneration
        historyLoading = true
        historyError = nil
        historyTask = Task { [weak self] in
            do {
                let page = try await pager.history(id, before: cursor)
                guard !Task.isCancelled, let self, self.historyGeneration == generation,
                      self.selectedID == id, self.snapshot?.historyCursor == cursor else { return }
                guard page.sessionID == id else { throw ClientError.invalidPayload }
                let existing = Set(self.snapshot?.messages.map(\.id) ?? [])
                guard page.messages.allSatisfy({ !existing.contains($0.id) }) else { throw ClientError.invalidPayload }
                beforePrepend()
                self.historyPrefix = page.messages + self.historyPrefix
                self.snapshot?.messages.insert(contentsOf: page.messages, at: 0)
                self.historyCursor = page.previousCursor
                self.snapshot?.historyCursor = page.previousCursor
                self.historyLoading = false
                self.historyTask = nil
            } catch {
                guard !Task.isCancelled, let self, self.historyGeneration == generation else { return }
                self.historyLoading = false
                self.historyTask = nil
                if Self.isMissing(error) {
                    self.markUnavailable()
                } else if case ClientError.http(409) = error {
                    // The server no longer recognizes this history boundary.
                    // Reattach to an authoritative snapshot instead of mixing pages.
                    self.historyPrefix = []
                    self.historyBoundary = nil
                    self.resetHistoryOnReceive = true
                    self.historyError = "History changed on the server. Reloading the recent transcript."
                    self.attach()
                } else {
                    self.historyError = error.localizedDescription
                }
            }
        }
    }

    private func cancelHistory() {
        historyGeneration = UUID()
        historyTask?.cancel(); historyTask = nil
        historyLoading = false
    }

    func detach() {
        vcsTask?.cancel(); vcsTask = nil
        vcsRefreshPending = false
        vcsActivity = nil
        cancelHistory()
        attachmentGeneration = UUID()
        watchTask?.cancel(); watchTask = nil
        connectionState = .disconnected
    }

    /// Replace the visible replica from one authoritative snapshot before
    /// reattaching SSE. Generation checks keep late responses out of other sessions.
    func resynchronize() async throws -> SessionExcerpt {
        detach()
        let generation = attachmentGeneration
        let id = selectedID
        connectionState = .loading
        do {
            let fresh = try await client.snapshot(id)
            try Task.checkCancellation()
            guard attachmentGeneration == generation, selectedID == id else { throw CancellationError() }
            guard fresh.id == id, fresh.followUps != nil else { throw ClientError.invalidPayload }
            receive(fresh, generation: generation)
            attach()
            return fresh
        } catch {
            if attachmentGeneration == generation {
                if Self.isMissing(error) { markUnavailable() }
                else { connectionState = .failed(error.localizedDescription) }
            }
            throw error
        }
    }

    func attach() {
        guard !client.isDemo, !selectedID.isEmpty else { return }
        detach()
        let client = client
        let identity = SessionIdentity(server: client.serverID, session: selectedID)
        let generation = attachmentGeneration
        watchTask = Task { [weak self] in
            var delay = 1
            while !Task.isCancelled {
                self?.connectionState = .loading
                do {
                    try await client.watch(identity.session) { [weak self] snapshot in
                        await self?.receive(snapshot, generation: generation)
                    }
                    return
                } catch {
                    guard !Task.isCancelled, let self, self.attachmentGeneration == generation else { return }
                    if Self.isMissing(error) { self.markUnavailable(); return }
                    if self.connectionState == .connected { delay = 1 }
                    self.connectionState = .failed(error.localizedDescription)
                    if case ClientError.http(401) = error { self.connectionState = .authenticationFailed; return }
                    if case ClientError.http(403) = error { self.connectionState = .authenticationFailed; return }
                    if case ClientError.invalidPayload = error { return }
                    if case ClientError.oversized = error { return }
                    if case ClientError.incompatible = error { return }
                    if error is DecodingError { return }
                    self.connectionState = .retrying(message: error.localizedDescription, delay: delay)
                }
                do { try await Task.sleep(for: .seconds(delay)) } catch { return }
                delay = min(delay * 2, 15)
            }
        }
    }

    private static func isMissing(_ error: Error) -> Bool {
        switch error {
        case ClientError.http(404), ClientError.missingSession: true
        default: false
        }
    }

    private var deletedIDs: Set<String> = []

    func sessionDeleted(_ identity: SessionIdentity) {
        guard identity.server == client.serverID else { return }
        deletedIDs.insert(identity.session)
        sessions.removeAll { $0.id == identity.session }
        if selectedID == identity.session { markUnavailable() }
    }

    private func markUnavailable() {
        detach()
        unavailable = true
        connectionState = .unavailable
    }

    private func receive(_ snapshot: SessionExcerpt, generation: UUID) {
        guard attachmentGeneration == generation, snapshot.id == selectedID else { return }
        if self.snapshot?.title != snapshot.title { titleRevision += 1 }
        if refreshingSessions, self.snapshot?.title != snapshot.title {
            titlesReceivedDuringRefresh[snapshot.id] = snapshot.title
        }
        if let index = sessions.firstIndex(where: { $0.id == snapshot.id }) { sessions[index] = snapshot }
        let activity = VCSActivity(snapshot)
        let refreshVCS = connectionState != .connected || vcsActivity != activity
        vcsActivity = activity
        var merged = snapshot
        if self.snapshot?.cwd == snapshot.cwd {
            merged.gitHead = self.snapshot?.gitHead
            merged.gitDirty = self.snapshot?.gitDirty
            merged.gitHeadKind = self.snapshot?.gitHeadKind
        } else { workspaceFiles = [] }
        if resetHistoryOnReceive || historyBoundary != snapshot.historyStart {
            cancelHistory()
            // A fresh bounded snapshot can move the recent-history boundary.
            // Retain only the prefix connected to it by a stable row identity.
            if !resetHistoryOnReceive, snapshot.historyCursor != nil, let first = snapshot.messages.first,
               let index = self.snapshot?.messages.firstIndex(where: { $0.id == first.id }) {
                historyPrefix = Array(self.snapshot!.messages.prefix(index))
            } else {
                historyPrefix = []
                historyCursor = snapshot.historyCursor
            }
            resetHistoryOnReceive = false
            historyBoundary = snapshot.historyStart
            historyError = nil
        }
        if !historyPrefix.isEmpty {
            merged.messages = historyPrefix + snapshot.messages
        }
        merged.historyCursor = historyCursor
        self.snapshot = merged
        unavailable = false
        onReceive?(merged)
        connectionState = .connected
        if refreshVCS { requestVCSRefresh() }
    }

}
