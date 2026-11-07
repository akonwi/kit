import KitClient
import SwiftUI

/// The session's shared scratchpad as a plain Markdown editor. It saves a
/// moment after typing stops. When the agent changes the shared copy under
/// unsaved edits, the draft stays and a notice offers to review the two.
struct ScratchpadScreen: View {
    let pad: ScratchpadState
    let client: ServerClient
    let feed: SessionFeed
    @Environment(\.scenePhase) private var scenePhase

    private var session: String { feed.identity.session }

    var body: some View {
        content
            .background(Theme.surface)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(.hidden, for: .tabBar)
            .toolbar {
                ToolbarItem(placement: .principal) { header }
                    .sharedBackgroundVisibility(.hidden)
            }
            .navigationDestination(isPresented: Binding(
                get: { pad.reviewing }, set: { if !$0 { pad.keepEditing() } })) {
                ScratchpadReviewScreen(pad: pad, client: client, session: session)
            }
            .task { await pad.load(client: client, session: session) }
            // Save what's typed before the app leaves the screen or the foreground.
            .onChange(of: scenePhase) { _, phase in
                if phase != .active { save() }
            }
            .onDisappear { save() }
    }

    private func save() {
        guard pad.dirty, pad.status != .conflict else { return }
        Task { await pad.save(client: client, session: session) }
    }

    // MARK: Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text("Scratchpad")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
            Text(subtitle)
                .font(.system(size: 12))
                .foregroundStyle(subtitleColor)
                .lineLimit(1)
                .accessibilityIdentifier("scratchpad-status")
        }
        .frame(minWidth: 0, idealWidth: 10_000, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private var subtitle: String {
        switch pad.status {
        case .loading: "Loading…"
        case .saving: "Saving…"
        case .conflict: "Conflict"
        case .failed(let reason): reason
        case .saved, .unsaved: ScratchpadFormat.sharing(owner: pad.authoritative?.owner, session: session,
                                                         title: feed.session?.title)
        }
    }

    private var subtitleColor: Color {
        switch pad.status {
        case .conflict: Theme.warning
        case .failed: Theme.danger
        default: Theme.muted
        }
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if pad.authoritative == nil {
            if case .failed = pad.status {
                ContentUnavailableView {
                    Label("Couldn’t open the scratchpad", systemImage: "exclamationmark.triangle")
                } actions: {
                    Button("Try Again") { Task { await pad.load(client: client, session: session) } }
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        } else {
            VStack(spacing: 0) {
                if pad.status == .conflict { conflictNotice.padding(.horizontal, 20).padding(.top, 4) }
                ScratchpadEditor(text: pad.draft, version: pad.documentVersion) { pad.edit($0) }
                    .overlay(alignment: .topLeading) {
                        if pad.draft.isEmpty {
                            Text("Notes you and the agent share")
                                .font(ScratchpadEditor.placeholderFont)
                                .foregroundStyle(Theme.muted)
                                .padding(.horizontal, ScratchpadEditor.inset.left)
                                .padding(.top, ScratchpadEditor.inset.top)
                                .allowsHitTesting(false)
                        }
                    }
            }
        }
    }

    private var conflictNotice: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 14))
                    .foregroundStyle(Theme.warning)
                    .padding(.top, 2)
                Text("Shared scratchpad changed. Your draft is preserved.")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.text)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            Button { pad.review() } label: {
                Text("Review changes")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(Theme.surface)
                    .padding(.horizontal, 14)
                    .frame(height: 32)
                    .background(Theme.text, in: Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("scratchpad-review-changes")
        }
        .padding(.horizontal, 14).padding(.vertical, 12)
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.cardRadius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.cardRadius, style: .continuous)
            .strokeBorder(Theme.border, lineWidth: 0.5))
    }
}

/// The shared copy compared with the draft, and what to keep.
struct ScratchpadReviewScreen: View {
    let pad: ScratchpadState
    let client: ServerClient
    let session: String
    @State private var replacing = false

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(Array(ScratchpadFormat.reviewLines(pad.reviewText).enumerated()), id: \.offset) { _, line in
                    reviewLine(line)
                }
            }
            .padding(.top, 8)
            .padding(.bottom, 160)
        }
        .background(Theme.surface)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(.hidden, for: .tabBar)
        .toolbar {
            ToolbarItem(placement: .principal) { header }
                .sharedBackgroundVisibility(.hidden)
        }
        .safeAreaBar(edge: .bottom, spacing: 0) { actions }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text("Scratchpad changes")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
            Text("Shared copy compared with your draft")
                .font(.system(size: 12))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
        }
        .frame(minWidth: 0, idealWidth: 10_000, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private func reviewLine(_ line: ScratchpadFormat.ReviewLine) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(line.marker)
            Text(line.text.isEmpty ? " " : line.text)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 13, design: .monospaced))
        .lineSpacing(6)
        .foregroundStyle(Theme.text)
        .padding(.horizontal, 20).padding(.vertical, 3)
        .background(line.kind == .added ? Theme.diffAdded : line.kind == .removed ? Theme.diffRemoved : .clear)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(line.accessibilityLabel)
    }

    private var actions: some View {
        VStack(spacing: 8) {
            Button { pad.keepEditing() } label: {
                Text("Keep editing")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(Theme.surface)
                    .frame(maxWidth: .infinity).frame(height: 48)
                    .background(Theme.text, in: Capsule())
            }
            .accessibilityIdentifier("scratchpad-keep-editing")
            HStack(spacing: 8) {
                Button { pad.useShared() } label: {
                    // Narrower than Replace, whose title is longer.
                    Text("Use shared")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.text)
                        .fixedSize()
                        .padding(.horizontal, 32).frame(height: 44)
                        .background(Theme.glassButton, in: Capsule())
                }
                .accessibilityIdentifier("scratchpad-use-shared")
                Button {
                    replacing = true
                    Task {
                        await pad.replaceReviewed(client: client, session: session)
                        replacing = false
                    }
                } label: {
                    Text("Replace shared with mine")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.danger)
                        .lineLimit(1).minimumScaleFactor(0.85)
                        .frame(maxWidth: .infinity).frame(height: 44)
                        .background(Theme.glassButton, in: Capsule())
                }
                .accessibilityIdentifier("scratchpad-replace-shared")
            }
            .disabled(replacing)
        }
        .buttonStyle(.plain)
        .padding(12)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 28, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }
}

/// Text the scratchpad screens show, apart from how they draw it.
enum ScratchpadFormat {
    /// Who else sees the pad: every fork of the session it belongs to.
    static func sharing(owner: String?, session: String, title: String?) -> String {
        guard let owner, owner != session else {
            guard let title, !title.isEmpty else { return "Shared with this session’s forks" }
            return "Shared with forks of \(title)"
        }
        return "Shared with the session this was forked from"
    }

    struct ReviewLine: Equatable {
        enum Kind: Equatable { case same, added, removed }
        let kind: Kind
        let text: String

        var marker: String {
            switch kind {
            case .same: " "
            case .added: "+"
            case .removed: "-"
            }
        }

        var accessibilityLabel: String {
            switch kind {
            case .same: text
            case .added: "Only in your draft: \(text)"
            case .removed: "Only in the shared copy: \(text)"
            }
        }
    }

    /// The lines of `ScratchpadState.reviewText` past its header: each the
    /// same in both, only in the draft, or only in the shared copy.
    static func reviewLines(_ unified: String) -> [ReviewLine] {
        unified.components(separatedBy: "\n").drop { !$0.hasPrefix("@@") }.dropFirst().map { line in
            let kind: ReviewLine.Kind = switch line.first {
            case "+": .added
            case "-": .removed
            default: .same
            }
            return ReviewLine(kind: kind, text: String(line.dropFirst()))
        }
    }
}
