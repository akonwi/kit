import KitClient
import SwiftUI

/// A session's changes as one scroll of unified diffs, a section per file,
/// with the composer below so a follow-up can go straight back to the agent.
struct ChangesScreen: View {
    let diff: DiffState
    let client: ServerClient
    let feed: SessionFeed
    let composer: SessionComposer
    let actions: SessionActions
    let interactions: SessionInteractions
    @Environment(\.scenePhase) private var scenePhase
    @State private var choosingTarget = false

    private var session: String { feed.identity.session }

    var body: some View {
        content
            .background(Theme.surface)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(.hidden, for: .tabBar)
            .toolbar {
                ToolbarItem(placement: .principal) { header }
                    .sharedBackgroundVisibility(.hidden)
                ToolbarItem(placement: .topBarTrailing) { targetPill }
                ToolbarSpacer(.fixed, placement: .topBarTrailing)
                ToolbarItem(placement: .topBarTrailing) { viewMenu }
            }
            .safeAreaBar(edge: .bottom, spacing: 0) {
                if !feed.unavailable {
                    ComposerBar(composer: composer, actions: actions, interactions: interactions, feed: feed) { feed.reconnect() }
                }
            }
            .sheet(isPresented: $choosingTarget) {
                CompareSheet(diff: diff) { target in
                    choosingTarget = false
                    Task { await diff.select(target, client: client, session: session) }
                }
                .task { await diff.refreshCatalog(client: client, session: session) }
            }
            .task(id: scenePhase == .active) {
                guard scenePhase == .active else { return }
                await feed.view()
            }
            .task {
                if diff.catalog == nil && diff.document == nil { await diff.refresh(client: client, session: session) }
            }
            // Fetch files ahead of the reader so they arrive at their size.
            .task(id: "\(diff.observation?.target.id ?? "")|\(diff.observation?.revision ?? "")|\(diff.files.count)") {
                await diff.prefetch(client: client, session: session)
            }
            // Check the target for changes while shown, as the Mac does.
            .task(id: scenePhase == .active) {
                guard scenePhase == .active else { return }
                while !Task.isCancelled {
                    do { try await Task.sleep(for: DiffState.pollInterval) } catch { return }
                    await diff.poll(client: client, session: session) { true }
                }
            }
    }

    // MARK: Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text("Changes")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
            Text(feed.session?.title ?? "")
                .font(.system(size: 12))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
        }
        .frame(minWidth: 0, idealWidth: 10_000, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private var targetPill: some View {
        Button { choosingTarget = true } label: {
            HStack(spacing: 4) {
                Text(ChangesFormat.pill(diff.target))
                    .font(.system(size: 15, weight: .semibold))
                    .lineLimit(1)
                Image(systemName: "chevron.down")
                    .font(.system(size: 11, weight: .bold))
            }
            .foregroundStyle(Theme.text)
            .padding(.horizontal, 6)
        }
        .accessibilityLabel("Compare: \(ChangesFormat.pill(diff.target))")
        .accessibilityIdentifier("changes-target")
    }

    private var viewMenu: some View {
        Menu {
            Toggle("Wrap lines", systemImage: "text.word.spacing", isOn: Binding(
                get: { diff.wrapLines }, set: { diff.wrapLines = $0 }))
            Button("Refresh", systemImage: "arrow.clockwise") {
                Task { await diff.refresh(client: client, session: session) }
            }
            .disabled(diff.loading)
        } label: {
            Image(systemName: "ellipsis")
                .foregroundStyle(Theme.text)
                .accessibilityLabel("Changes options")
        }
        .accessibilityIdentifier("changes-menu")
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if diff.files.isEmpty {
            emptyState
        } else {
            document
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if let error = diff.error {
            ContentUnavailableView {
                Label(error, systemImage: "exclamationmark.triangle")
            } description: {
                if let detail = diff.errorDetail { Text(detail) }
            } actions: {
                Button("Try Again") { Task { await diff.refresh(client: client, session: session) } }
            }
        } else if diff.loading || diff.catalog == nil {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ContentUnavailableView(ChangesFormat.empty(diff), systemImage: "checkmark.circle")
        }
    }

    private var document: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0, pinnedViews: [.sectionHeaders]) {
                summary
                if let notice = diff.notice { DiffNote(text: notice) }
                if let error = diff.error { DiffNote(text: error, warning: true) }
                ForEach(diff.files, id: \.path) { file in
                    let collapsed = diff.collapsed.contains(file.path)
                    Section {
                        if !collapsed {
                            DiffFileBody(file: file, diff: diff, client: client, session: session)
                        }
                    } header: {
                        DiffFileHeader(file: file, collapsed: collapsed) {
                            if collapsed { diff.collapsed.remove(file.path) } else { diff.collapsed.insert(file.path) }
                        }
                    }
                }
                if diff.fileCursor != nil {
                    DiffNote(text: "Loading more files…", loading: true)
                        .task(id: "\(diff.fileCursor ?? "")\(diff.loading)") {
                            guard !diff.loading, let target = diff.target else { return }
                            await diff.select(target, client: client, session: session, more: true)
                        }
                }
            }
            .padding(.bottom, 16)
        }
    }

    /// `4 files changed +48 −12` above the first file.
    private var summary: some View {
        HStack(spacing: 6) {
            Text("\(ChangesFormat.files(diff.files.count, more: diff.fileCursor != nil)) changed")
                .foregroundStyle(Theme.muted)
            if let additions = diff.additions { Text("+\(additions)").foregroundStyle(Theme.success) }
            if let deletions = diff.deletions { Text("−\(deletions)").foregroundStyle(Theme.danger) }
        }
        .font(.system(size: 13))
        .padding(.horizontal, 20)
        .padding(.vertical, 8)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("changes-summary")
    }
}

/// A quiet line in the diff: why a file has no text, or what is loading.
struct DiffNote: View {
    let text: String
    var loading = false
    var warning = false

    var body: some View {
        HStack(spacing: 8) {
            if loading { ProgressView().controlSize(.small) }
            Text(text)
                .font(.system(size: 13))
                .foregroundStyle(warning ? Theme.warning : Theme.muted)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
