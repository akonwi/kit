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
    private let client: ServerClient
    @State private var position = ScrollPosition(edge: .bottom)
    /// Whether the reader is at the end, so new output scrolls into view.
    @State private var following = true
    /// Whether the reader is scrolling the transcript.
    @State private var scrolling = false

    init(identity: SessionIdentity, client: ServerClient) {
        let feed = SessionFeed(identity: identity, client: client)
        let composer = SessionComposer(session: identity.session, client: client)
        let actions = SessionActions(identity: identity, client: client)
        let interactions = SessionInteractions(session: identity.session, client: client)
        feed.onSnapshot = { [weak composer, weak actions, weak interactions] in
            composer?.reconcile($0)
            actions?.reconcile($0)
            interactions?.reconcile($0)
        }
        actions.deliver = { [weak feed] in feed?.receive($0) }
        feed.onConnection = { [weak composer, weak actions, weak interactions] connection in
            composer?.live = connection == .live
            actions?.live = connection == .live
            interactions?.live = connection == .live
        }
        composer.modelName = { [weak actions] in actions?.modelName ?? "" }
        if let failure = ForkFirstTurnFailures.shared.take(identity) { composer.restore(failedFork: failure) }
        _feed = State(initialValue: feed)
        _composer = State(initialValue: composer)
        _actions = State(initialValue: actions)
        _interactions = State(initialValue: interactions)
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
                                    confirmDelete: { confirmingDelete = true })
                    }
                }
            }
            .safeAreaBar(edge: .bottom, spacing: 0) {
                if !feed.unavailable {
                    ComposerBar(composer: composer, actions: actions, interactions: interactions, feed: feed,
                                revealComment: { note in
                                    diff.reveal(note)
                                    showingChanges = true
                                }) { feed.reconnect() }
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
            .environment(\.selectText) { selecting = SelectedText(text: $0) }
            .environment(\.transcriptAttachments, attachments)
            // Whether comments' code changed is known when they're read.
            .task(id: "\(feed.session != nil)|\(feed.session?.watchGeneration ?? "")") {
                guard feed.session != nil else { return }
                try? await composer.annotations.refresh(client: client, session: feed.identity.session)
            }
            .alert(deleteTitle, isPresented: $confirmingDelete) {
                Button("Cancel", role: .cancel) {}
                Button(actions.temporary ? "Dispose session" : "Delete session", role: .destructive) { delete() }
            } message: {
                Text(deleteMessage)
            }
            .alert("Couldn’t delete", isPresented: deleteFailed) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actions.deleteError ?? "")
            }
            .overlay { if actions.deleting { deletingOverlay } }
            .navigationDestination(isPresented: $showingChanges) {
                ChangesScreen(diff: diff, client: client, feed: feed, composer: composer,
                              actions: actions, interactions: interactions)
            }
            .task(id: scenePhase == .active) {
                guard scenePhase == .active else { return }
                await feed.view()
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

    private var deleteTitle: String { actions.temporary ? "Dispose temporary session?" : "Delete session?" }

    private var deleteMessage: String {
        let title = "“\(feed.session?.title ?? "This session")”"
        return actions.temporary
            ? "\(title) will be removed from the server’s memory and any active work will be cancelled. This can’t be undone."
            : "\(title) and its saved history will be permanently deleted. This can’t be undone."
    }

    private var deleteFailed: Binding<Bool> {
        Binding(get: { actions.deleteError != nil }, set: { if !$0 { actions.clearDeleteError() } })
    }

    private var deletingOverlay: some View {
        ProgressView()
            .controlSize(.large)
            .padding(24)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(.black.opacity(0.08))
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
            transcript(session)
        } else if case .unavailable(let reason) = feed.connection {
            ContentUnavailableView("Can’t show this session", systemImage: "exclamationmark.triangle", description: Text(reason))
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func transcript(_ session: SessionExcerpt) -> some View {
        let live = ToolGroupActivity.liveGroups(in: session.messages, active: session.activeRunID != nil)
        return ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                if feed.hasEarlier || feed.historyError != nil { earlier }
                ForEach(session.messages) { message in
                    TranscriptRow(message: message, live: live.contains(message.id))
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 8)
            .padding(.bottom, 16)
        }
        .scrollPosition($position)
        // Open at the end, but start a short transcript at the top.
        .defaultScrollAnchor(.bottom, for: .initialOffset)
        .defaultScrollAnchor(.top, for: .alignment)
        // At the end, a row that grows as it loads, such as an image, keeps
        // the end in place. Without it, a taller bar can leave the lazy stack
        // laying itself out without end.
        .defaultScrollAnchor(following ? .bottom : nil, for: .sizeChanges)
        // Only the reader's own scrolling stops or resumes following. The
        // screen's own scrolls aren't animated, so any scroll phase, including
        // a tap on the status bar, is the reader's.
        .onScrollPhaseChange { old, phase, context in
            scrolling = phase.isScrolling
            if old.isScrolling || phase.isScrolling {
                following = Self.atEnd(context.geometry)
            }
        }
        .onScrollGeometryChange(for: EndGeometry.self) { geometry in
            EndGeometry(inset: geometry.contentInsets.bottom, atEnd: Self.atEnd(geometry))
        } action: { old, new in
            if scrolling {
                following = new.atEnd
            } else if following, new.inset > old.inset {
                // A taller bar, such as the dock or a status line, hides the
                // end without the reader scrolling; keep it in view.
                position.scrollTo(edge: .bottom)
            }
        }
        .onChange(of: Self.tail(session)) {
            if following { position.scrollTo(edge: .bottom) }
        }
        // Sending shows the message, wherever the reader was.
        .onChange(of: composer.sends) {
            following = true
            position.scrollTo(edge: .bottom)
        }
    }

    /// How far the transcript is from its end, and how much the bar covers.
    private struct EndGeometry: Equatable {
        let inset: CGFloat
        let atEnd: Bool
    }

    /// Whether the end is in view. The visible rect runs under the bar and
    /// the keyboard, so their inset counts as part of the content.
    private static func atEnd(_ geometry: ScrollGeometry) -> Bool {
        geometry.contentSize.height + geometry.contentInsets.bottom - geometry.visibleRect.maxY < 48
    }

    /// What changes when output arrives at the end of the transcript.
    private static func tail(_ session: SessionExcerpt) -> [Int] {
        guard let last = session.messages.last else { return [] }
        return [session.messages.count, last.text.count, last.tools.count, last.tools.filter(\.running).count]
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
            guard session.activeRunID != nil || session.activity != nil else { return nil }
            if let tool = session.messages.last(where: { $0.role == "tools" })?.tools.last(where: \.running) {
                let presentation = ToolPresentation(tool)
                label = tool.bashCommand != nil ? "Running" : presentation.title
                detail = presentation.chip
                started = tool.startedAt
            } else {
                label = session.activity ?? LiveActivity.working
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
