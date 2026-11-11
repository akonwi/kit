import KitClient
import KitTranscript
import SwiftUI

/// One session's live transcript and its composer.
struct SessionScreen: View {
    /// What the session menu opens over the screen.
    enum Sheet: String, Identifiable {
        case rename, fork, details
        var id: String { rawValue }
    }

    @Environment(SessionsStore.self) private var sessions
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openSession) private var openSession
    @State private var feed: SessionFeed
    @State private var composer: SessionComposer
    @State private var actions: SessionActions
    @State private var interactions: SessionInteractions
    @State private var attachments: TranscriptAttachmentStore
    @State private var sheet: Sheet?
    @State private var confirmingDelete = false
    @State private var selecting: SelectedText?
    /// The session's changes: counted for the menu, shown by Changes.
    @State private var diff = DiffState()
    @State private var showingChanges = false
    /// The session's files, listed for Open File.
    @State private var files = WorkspaceFiles()
    @State private var openingFile = false
    /// The file shown over the session.
    @State private var openedFile: OpenedFile?
    /// The session's shared scratchpad, kept current from its snapshots.
    @State private var scratchpad: ScratchpadState
    @State private var showingScratchpad = false
    /// Which agent's transcript shows: Main's or a subagent's.
    @State private var agents: SubagentFocus
    /// The session's branch, pull request, and checks, for the strip.
    @State private var repository: SessionRepository
    @State private var showingChecks = false
    private let client: ServerClient

    init(identity: SessionIdentity, client: ServerClient) {
        let feed = SessionFeed(identity: identity, client: client)
        let composer = SessionComposer(session: identity.session, client: client)
        let actions = SessionActions(identity: identity, client: client)
        let interactions = SessionInteractions(session: identity.session, client: client)
        let scratchpad = ScratchpadState(demo: false)
        scratchpad.bind(client: client, session: identity.session)
        let agents = SubagentFocus(session: identity.session, client: client)
        feed.onSnapshot = { [weak composer, weak actions, weak interactions, weak scratchpad, weak agents] in
            composer?.reconcile($0)
            actions?.reconcile($0)
            interactions?.reconcile($0)
            agents?.reconcile($0)
            if let record = $0.scratchpad { scratchpad?.observe(record) }
        }
        actions.deliver = { [weak feed] in feed?.receive($0) }
        agents.refresh = { [weak feed] in
            let snapshot = try await client.snapshot(identity.session)
            feed?.receive(snapshot)
        }
        feed.onConnection = { [weak composer, weak actions, weak interactions, weak agents] connection in
            composer?.live = connection == .live
            actions?.live = connection == .live
            interactions?.live = connection == .live
            agents?.live = connection == .live
        }
        composer.modelName = { [weak actions] in actions?.modelName ?? "" }
        if let failure = ForkFirstTurnFailures.shared.take(identity) { composer.restore(failedFork: failure) }
        _feed = State(initialValue: feed)
        _composer = State(initialValue: composer)
        _actions = State(initialValue: actions)
        _interactions = State(initialValue: interactions)
        _scratchpad = State(initialValue: scratchpad)
        _agents = State(initialValue: agents)
        _repository = State(initialValue: SessionRepository(session: identity.session, client: client))
        _attachments = State(initialValue: TranscriptAttachmentStore(client: client, session: identity.session))
        self.client = client
    }

    var body: some View {
        content
            .background(Theme.surface)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(.hidden, for: .tabBar)
            .toolbar {
                ToolbarItem(placement: .principal) { header }
                    .sharedBackgroundVisibility(.hidden)
                if let session = feed.session {
                    ToolbarItem(placement: .topBarTrailing) {
                        SessionMenu(session: session, actions: actions, diff: diff, present: { sheet = $0 },
                                    openChanges: { showingChanges = true },
                                    openFile: { openingFile = true },
                                    openScratchpad: { showingScratchpad = true },
                                    confirmDelete: { confirmingDelete = true })
                    }
                }
            }
            .safeAreaBar(edge: .top, spacing: 0) {
                if feed.session != nil {
                    VStack(spacing: 0) {
                        if let status = repository.status {
                            RepositoryStrip(status: status) { showingChecks = true }
                        }
                        if !agents.strip.isEmpty {
                            AgentStrip(focus: agents, mainNeedsYou: interactions.docked != nil)
                        }
                    }
                }
            }
            .safeAreaBar(edge: .bottom, spacing: 0) {
                if feed.unavailable {
                    EmptyView()
                } else if let agent = agents.focused {
                    AgentBar(agent: agent, focus: agents, actions: actions, conversation: agents.conversation(agent))
                } else {
                    ComposerBar(composer: composer, actions: actions, interactions: interactions, feed: feed,
                                revealComment: open, revealable: { _ in true }) { feed.reconnect() }
                }
            }
            .sheet(item: $sheet) { sheet in
                switch sheet {
                case .rename:
                    RenameSessionSheet(actions: actions) { refreshList() }
                case .fork:
                    ForkSessionSheet(actions: actions) { child in
                        refreshList()
                        openSession(child)
                    }
                case .details:
                    if let session = feed.session {
                        SessionDetailsSheet(session: session, server: feed.identity.server)
                    }
                }
            }
            .sheet(item: $selecting) { SelectTextSheet(text: $0.text) }
            .sheet(isPresented: $showingChecks) { ChecksSheet(repository: repository) }
            .sheet(isPresented: $openingFile) {
                OpenFileSheet(files: files, client: client, session: feed.identity.session) { file in
                    openingFile = false
                    openedFile = OpenedFile(path: file)
                }
            }
            .environment(\.selectText) { selecting = SelectedText(text: $0) }
            .environment(\.transcriptAttachments, attachments)
            .environment(\.openComment, open)
            // Whether comments' code changed is known when they're read.
            .task(id: "\(feed.session != nil)|\(feed.session?.watchGeneration ?? "")") {
                guard feed.session != nil else { return }
                try? await composer.annotations.refresh(client: client, session: feed.identity.session)
            }
            .alert(actions.deleteConfirmation.title, isPresented: $confirmingDelete) {
                Button("Cancel", role: .cancel) {}
                Button(actions.deleteConfirmation.button, role: .destructive) { delete() }
            } message: {
                Text(actions.deleteConfirmation.message)
            }
            .alert("Couldn’t delete", isPresented: deleteFailed) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actions.deleteError ?? "")
            }
            .overlay { if actions.deleting { DeletingOverlay() } }
            .navigationDestination(isPresented: $showingChanges) {
                ChangesScreen(diff: diff, client: client, feed: feed, composer: composer,
                              actions: actions, interactions: interactions)
            }
            .navigationDestination(isPresented: $showingScratchpad) {
                ScratchpadScreen(pad: scratchpad, client: client, feed: feed)
            }
            .navigationDestination(item: $openedFile) { file in
                FileScreen(path: file.path, line: file.line, client: client, feed: feed, composer: composer,
                           actions: actions, interactions: interactions)
            }
            .task(id: scenePhase == .active) {
                guard scenePhase == .active else { return }
                await feed.view()
            }
            .task(id: scenePhase == .active) {
                guard scenePhase == .active else { return }
                await repository.watch()
            }
            // Count the changes for the menu once, and again when a turn ends.
            .task {
                if diff.catalog == nil && diff.document == nil { await diff.refresh(client: client, session: feed.identity.session) }
            }
            .onChange(of: feed.session?.activeRunID == nil) { _, idle in
                guard idle else { return }
                Task { await diff.poll(client: client, session: feed.identity.session) { true } }
            }
            .onChange(of: feed.session?.lastActivity) { markSeen() }
            .onDisappear { markSeen() }
    }

    // MARK: Delete

    /// Shows a comment where it was written: Changes for a comment on a
    /// diff, its file at its lines for the rest.
    private func open(_ note: FileAnnotation) {
        if note.isDiff {
            diff.reveal(note)
            showingChanges = true
        } else {
            openedFile = OpenedFile(path: note.path, line: note.startLine)
        }
    }

    private var deleteFailed: Binding<Bool> {
        Binding(get: { actions.deleteError != nil }, set: { if !$0 { actions.clearDeleteError() } })
    }

    private func delete() {
        Task {
            guard await actions.delete() else { return }
            refreshList()
            dismiss()
        }
    }

    /// Lists the session's server again so the list shows a rename, fork, or delete.
    private func refreshList() {
        let server = feed.identity.server
        Task { await sessions.retry(server) }
    }

    private func markSeen() {
        sessions.markSeen(feed.identity, changed: feed.session.flatMap { SessionFormat.date($0.lastActivity ?? $0.date) })
    }

    // MARK: Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(feed.session?.title ?? listed?.session.title ?? "Session")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
                .lineLimit(1)
            Text("\(feed.identity.server) · \(Self.shortDirectory(feed.session?.cwd ?? listed?.session.cwd ?? ""))")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
        }
        // The toolbar centers its ideal size; a large one fills the space, so
        // a short title starts at the left as a long one does.
        .frame(minWidth: 0, idealWidth: 10_000, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private var listed: SessionsStore.Row? { sessions.rows.first { $0.id == feed.identity } }

    /// A working directory short enough for the header: `~/…/agent/kit`.
    static func shortDirectory(_ path: String) -> String {
        let full = SessionFormat.directory(path)
        let parts = full.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count > 3 else { return full }
        return ([parts[0], "…"] + parts.suffix(2)).joined(separator: "/")
    }

    // MARK: Transcript

    @ViewBuilder
    private var content: some View {
        if let session = feed.session {
            // Each transcript shown stays put while another is in view, so
            // coming back finds it where the reader left it.
            ZStack {
                transcript(session)
                    .shown(agents.focus == nil)
                ForEach(agents.visited, id: \.self) { name in
                    if let agent = agents.strip.agent(named: name), let conversation = agents.conversation(agent) {
                        SubagentTranscript(agent: agent, conversation: conversation, focus: agents,
                                           visible: agents.focus == name)
                            .id(agent.conversationID)
                            .shown(agents.focus == name)
                    }
                }
            }
            .environment(\.agentLinks, AgentLinks(names: Set(agents.strip.agents.map(\.name))) { agents.show($0) })
        } else if case .unavailable(let reason) = feed.connection {
            ContentUnavailableView("Can’t show this session", systemImage: "exclamationmark.triangle", description: Text(reason))
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func transcript(_ session: SessionExcerpt) -> some View {
        TranscriptList(messages: session.messages, active: session.activeRunID != nil, sends: composer.sends) {
            if feed.hasEarlier || feed.historyError != nil { earlier.transcriptRow() }
        }
        .accessibilityIdentifier("transcript")
    }

    private var earlier: some View {
        VStack(spacing: 6) {
            if feed.historyLoading {
                ProgressView()
            } else if feed.hasEarlier {
                Button("Show earlier messages") { Task { await feed.loadEarlier() } }
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(Theme.accent)
            }
            if let error = feed.historyError {
                Text(error).font(.system(size: 13)).foregroundStyle(Theme.muted).multilineTextAlignment(.center)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
    }
}

/// What the session is doing while a turn runs: the running tool call with
/// how long it has run, or the model's latest thought.
struct ActivityLine: View {
    struct Content: Equatable {
        let label: String
        var detail: String?
        var started: Date?

        init?(_ session: SessionExcerpt) {
            self.init(messages: session.messages, activity: session.activity, running: session.activeRunID != nil)
        }

        /// What an agent is doing: its running tool call, or its activity.
        init?(messages: [TranscriptMessage], activity: String?, running: Bool) {
            guard running || activity != nil else { return nil }
            if let tool = messages.last(where: { $0.role == "tools" })?.tools.last(where: \.running) {
                let presentation = ToolPresentation(tool)
                label = tool.bashCommand != nil ? "Running" : presentation.title
                detail = presentation.chip
                started = tool.startedAt
            } else {
                label = activity ?? LiveActivity.working
            }
        }
    }

    let content: Content

    var body: some View {
        HStack(spacing: 8) {
            LaneSpinner()
            Text(content.label)
                .font(.system(size: 14))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
            if let detail = content.detail {
                Text(detail)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
            if let started = content.started {
                TimelineView(.periodic(from: started, by: 1)) { context in
                    Text(Self.duration(context.date.timeIntervalSince(started)))
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(Theme.muted)
                }
            }
        }
        .frame(height: 30)
        .accessibilityElement(children: .combine)
    }

    static func duration(_ seconds: TimeInterval) -> String {
        let whole = max(0, Int(seconds))
        return "\(whole / 60):" + String(format: "%02d", whole % 60)
    }
}

private extension View {
    /// Shows a transcript, or keeps it in place, hidden, while another shows.
    func shown(_ shown: Bool) -> some View {
        opacity(shown ? 1 : 0)
            .allowsHitTesting(shown)
            .accessibilityHidden(!shown)
    }
}

/// Covers a screen while a session is deleted.
struct DeletingOverlay: View {
    var body: some View {
        ProgressView()
            .controlSize(.large)
            .padding(24)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(.black.opacity(0.08))
    }
}
