import KitClient
import SwiftUI

/// Every session on every paired server, latest activity first, with
/// filters for sessions that need you and for each server.
struct SessionsView: View {
    @Environment(ServerStore.self) private var servers
    @Environment(SessionsStore.self) private var store
    @State private var filter = SessionsStore.Filter.all
    /// The open session, when one is.
    @State private var path: [SessionIdentity] = []
    @State private var newSession: NewSessionForm?
    /// The session being renamed from its row's menu.
    @State private var renaming: ListedSession?
    /// The session whose deletion is being confirmed. Kept after the alert
    /// closes so its words don't change while it fades.
    @State private var confirming: ListedSession?
    @State private var confirmingDelete = false
    /// The session being deleted, until the list no longer shows it or
    /// until its failure is seen.
    @State private var deleting: ListedSession?

    /// How often the list is fetched again while it's on screen.
    static let refreshInterval: Duration = .seconds(10)

    var body: some View {
        NavigationStack(path: $path) {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    // A lone All chip filters nothing, so it isn't shown.
                    if filterChoices.count > 1 {
                        filters
                            .padding(.top, 10)
                            .padding(.bottom, 4)
                    }
                    content
                        .padding(.top, 12)
                }
                .padding(.bottom, 24)
            }
            .background(Theme.surface)
            .navigationTitle("Sessions")
            .navigationDestination(for: SessionIdentity.self) { id in
                if let server = store.server(named: id.server) {
                    // A session opened in place of another starts afresh.
                    SessionScreen(identity: id, client: store.network.client(for: server))
                        .id(id)
                } else {
                    ContentUnavailableView("Server removed", systemImage: "server.rack",
                                           description: Text("\(id.server) is no longer paired with this iPhone."))
                }
            }
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("New session", systemImage: "plus", action: startSession)
                        .disabled(store.servers.isEmpty)
                        .accessibilityIdentifier("new-session")
                }
            }
            .sheet(item: $newSession) { form in
                NewSessionSheet(form: form) { session in
                    path = [session]
                    Task { await store.retry(session.server) }
                }
            }
            .sheet(item: $renaming) { listed in
                RenameSessionSheet(actions: listed.actions) {
                    Task { await store.retry(listed.id.server) }
                }
            }
            .alert(confirming?.confirmation.title ?? "", isPresented: $confirmingDelete, presenting: confirming) { listed in
                Button("Cancel", role: .cancel) {}
                Button(listed.confirmation.button, role: .destructive) { delete(listed) }
            } message: { listed in
                Text(listed.confirmation.message)
            }
            .alert("Couldn’t delete", isPresented: deleteFailed) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(deleting?.actions.deleteError ?? "")
            }
            .overlay { if let deleting, deleting.actions.deleteError == nil { DeletingOverlay() } }
            .refreshable { await store.refresh(saved) }
            .task(id: saved.map(\.address)) {
                while !Task.isCancelled {
                    await store.refresh(saved)
                    try? await Task.sleep(for: Self.refreshInterval)
                }
            }
            .onChange(of: filterChoices) { _, choices in
                if !choices.contains(filter) { filter = .all }
            }
        }
        // Set on the stack so pushed sessions see it.
        .environment(\.openSession, OpenSessionAction { path = [$0] })
    }

    private var saved: [SavedServer] { servers.entries.map(\.server) }

    /// Opens the new session form on the server of the latest session.
    private func startSession() {
        let network = store.network, store = store
        let places = NewSessionForm.places(servers: store.servers, rows: store.rows)
        newSession = NewSessionForm(places: places, server: store.rows.first?.server) { name in
            guard let server = store.server(named: name) else { return UnpairedServer(name: name) }
            return network.client(for: server)
        }
    }

    // MARK: Filters

    /// All; Needs you while something does; and each server when there's more than one.
    private var filterChoices: [SessionsStore.Filter] {
        var choices: [SessionsStore.Filter] = [.all]
        if store.needsYouCount > 0 { choices.append(.needsYou) }
        if store.servers.count > 1 { choices += store.servers.map(SessionsStore.Filter.server) }
        return choices
    }

    private var filters: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(filterChoices, id: \.self) { choice in
                    FilterChip(title: title(choice), count: choice == .needsYou ? store.needsYouCount : nil,
                               selected: filter == choice) {
                        filter = choice
                    }
                }
            }
            .padding(.horizontal, 20)
        }
        .scrollIndicators(.hidden)
    }

    private func title(_ filter: SessionsStore.Filter) -> String {
        switch filter {
        case .all: "All"
        case .needsYou: "Needs you"
        case .server(let name): name
        }
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        let rows = store.rows(filter)
        let problems = store.problems.filter { filter == .all || filter == .server($0.server) }
        VStack(spacing: 0) {
            ForEach(problems) { problem in
                ServerProblemBanner(problem: problem) {
                    Task { await store.retry(problem.server) }
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 4)
            }
            if rows.isEmpty {
                if store.loaded {
                    empty
                } else {
                    ProgressView()
                        .padding(.top, 120)
                        .frame(maxWidth: .infinity)
                }
            } else {
                TimelineView(.periodic(from: .now, by: 30)) { context in
                    LazyVStack(spacing: 0) {
                        ForEach(rows) { row in
                            NavigationLink(value: row.id) {
                                SessionRow(row: row, now: context.date)
                            }
                            .buttonStyle(.plain)
                            .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: 22, style: .continuous))
                            .contextMenu { menu(row) }
                        }
                    }
                }
            }
        }
    }

    // MARK: Row menu

    /// Rename and Delete, on a long press of a row. Both are off while its server can’t be reached.
    @ViewBuilder
    private func menu(_ row: SessionsStore.Row) -> some View {
        let confirmation = SessionActions.DeleteConfirmation(session: row.session.title,
                                                             temporary: row.session.isTemporary == true)
        Group {
            Button("Rename…", systemImage: "pencil") { renaming = listed(row) }
            Button(confirmation.item, systemImage: "trash", role: .destructive) {
                confirming = listed(row)
                confirmingDelete = confirming != nil
            }
        }
        .disabled(row.stale)
    }

    /// What the menu acts through for a row: the session as the list last read it.
    private func listed(_ row: SessionsStore.Row) -> ListedSession? {
        guard let server = store.server(named: row.server) else { return nil }
        let actions = SessionActions(identity: row.id, client: store.network.client(for: server))
        actions.reconcile(row.session)
        return ListedSession(actions: actions)
    }

    /// Deletes the session, keeping the progress up until the list reads its server again.
    private func delete(_ listed: ListedSession) {
        deleting = listed
        Task {
            guard await listed.actions.delete() else { return }
            await store.retry(listed.id.server)
            deleting = nil
        }
    }

    private var deleteFailed: Binding<Bool> {
        Binding(get: { deleting?.actions.deleteError != nil }, set: { if !$0 { deleting = nil } })
    }

    private var empty: some View {
        VStack(spacing: 8) {
            Image(systemName: "apple.terminal")
                .font(.system(size: 28))
                .foregroundStyle(Theme.muted)
                .padding(.bottom, 4)
            Text(filter == .needsYou ? "Nothing needs you" : "No sessions yet")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
            Text(emptyDetail)
                .font(.system(size: 15))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
        }
        .padding(.horizontal, 40)
        .padding(.top, 140)
        .frame(maxWidth: .infinity)
    }

    private var emptyDetail: String {
        switch filter {
        case .needsYou: return "Sessions that ask you something show up here."
        case .server(let name): return "Sessions from \(name) show up here, including ones you start on your Mac."
        case .all:
            let names = store.servers.formatted(.list(type: .and))
            return "Sessions from \(names) show up here, including ones you start on your Mac."
        }
    }
}

/// A listed session acted on from its row's menu.
struct ListedSession: Identifiable {
    let id: SessionIdentity
    let actions: SessionActions
    let confirmation: SessionActions.DeleteConfirmation

    @MainActor init(actions: SessionActions) {
        id = actions.identity
        self.actions = actions
        confirmation = actions.deleteConfirmation
    }
}

/// A filter in the row under the title.
struct FilterChip: View {
    let title: String
    var count: Int?
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Text(title)
                    .font(.system(size: 15, weight: selected ? .semibold : .medium))
                    .foregroundStyle(selected ? Theme.surface : Theme.text)
                if let count, count > 0 {
                    Text("\(count)")
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(Theme.surface)
                        .padding(.horizontal, 6)
                        .frame(minWidth: 20, minHeight: 20)
                        .background(Theme.warning, in: Capsule())
                }
            }
            .padding(.leading, 14)
            .padding(.trailing, count ?? 0 > 0 ? 12 : 14)
            .frame(height: 34)
            .background(selected ? Theme.text : Theme.raised, in: Capsule())
            .overlay {
                if !selected { Capsule().strokeBorder(Theme.border, lineWidth: 0.5) }
            }
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A server whose sessions can't be listed now.
struct ServerProblemBanner: View {
    let problem: SessionsStore.Problem
    let retry: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            ServerStateIndicator(state: problem.state)
            VStack(alignment: .leading, spacing: 1) {
                Text(title)
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.text)
                Text(detail)
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
            }
            Spacer(minLength: 0)
            if case .unreachable = problem.state {
                Button("Retry", action: retry)
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.accent)
            }
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 12)
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.cardRadius))
        .overlay { RoundedRectangle(cornerRadius: Theme.cardRadius).strokeBorder(Theme.border, lineWidth: 0.5) }
        .accessibilityElement(children: .combine)
    }

    private var title: String {
        if case .unreachable = problem.state { return "Can’t reach \(problem.server)" }
        return problem.server
    }

    private var detail: String {
        if case .unreachable = problem.state {
            guard let listedAt = problem.listedAt else { return "Its sessions show once it answers." }
            return "Its sessions show their state from \(listedAt.formatted(date: .omitted, time: .shortened))."
        }
        return ServerStatePresentation.summary(problem.state)
    }
}

/// One session: a lane for what it needs, its title, a preview, and where it runs.
struct SessionRow: View {
    let row: SessionsStore.Row
    let now: Date

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            // The lane keeps its width when it has no mark, so every title
            // starts at the same edge.
            Color.clear
                .frame(width: 20, height: 22)
                .overlay { SessionLane(row: row) }
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(row.session.title)
                        .font(.system(size: 17, weight: row.stands ? .semibold : .regular))
                        .foregroundStyle(row.stale && row.status != .idle ? Theme.muted : Theme.text)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if let changed = row.changed {
                        Text(SessionFormat.age(changed, now: now))
                            .font(.system(size: 15))
                            .foregroundStyle(Theme.muted)
                    }
                }
                if let preview = row.preview {
                    Text(preview.text(stale: row.stale))
                        .font(.system(size: 15))
                        .foregroundStyle(previewColor(preview))
                        .lineLimit(1)
                }
                Text("\(row.server) · \(SessionFormat.directory(row.session.cwd ?? row.session.workspace))")
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
            }
            .padding(.bottom, 11)
            .overlay(alignment: .bottom) {
                Rectangle().fill(Theme.border).frame(height: 0.5)
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 11)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityLabel)
    }

    private func previewColor(_ preview: SessionPreview) -> Color {
        if case .asking = preview, !row.stale { return Theme.warning }
        return Theme.muted
    }

    private var accessibilityLabel: String {
        var parts = [row.session.title]
        if let status = statusDescription { parts.append(status) }
        if let preview = row.preview { parts.append(preview.text(stale: row.stale)) }
        parts.append(row.server)
        return parts.joined(separator: ", ")
    }

    private var statusDescription: String? {
        if row.stale && row.status != .idle { return "Last seen " + (row.status == .running ? "working" : "waiting") }
        switch row.status {
        case .awaitingResponse: return "Needs you"
        case .running: return "Working"
        case .idle: return row.unseen ? "Finished" : nil
        }
    }
}

/// The mark before a session's title: a question, work in progress, a
/// finished change you haven't seen, or a stale state from an unreachable server.
struct SessionLane: View {
    let row: SessionsStore.Row

    var body: some View {
        Group {
            if row.stale && row.status != .idle {
                Circle().strokeBorder(Theme.strongBorder, lineWidth: 1.5)
                    .frame(width: 10, height: 10)
            } else {
                switch row.status {
                case .awaitingResponse:
                    Text("?")
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(Theme.surface)
                        .frame(width: 18, height: 18)
                        .background(Theme.warning, in: Circle())
                case .running:
                    LaneSpinner()
                case .idle:
                    if row.unseen {
                        Image(systemName: "checkmark")
                            .font(.system(size: 13, weight: .bold))
                            .foregroundStyle(Theme.success)
                    }
                }
            }
        }
        .accessibilityHidden(true)
    }
}

/// A small ring with a turning accent arc.
struct LaneSpinner: View {
    var tint = Theme.accent

    var body: some View {
        TimelineView(.animation) { context in
            let turn = context.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 1)
            ZStack {
                Circle().stroke(Theme.border, lineWidth: 2)
                Circle().trim(from: 0, to: 0.25)
                    .stroke(tint, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                    .rotationEffect(.degrees(turn * 360 - 90))
            }
            .frame(width: 13, height: 13)
        }
    }
}

/// Opens a session in place of the one shown, such as a fork just made.
struct OpenSessionAction {
    let open: @MainActor (SessionIdentity) -> Void
    @MainActor func callAsFunction(_ id: SessionIdentity) { open(id) }
}

extension EnvironmentValues {
    @Entry var openSession = OpenSessionAction { _ in }
}
