import KitClient
import SwiftUI

/// What Changes compares: uncommitted changes, this branch against its
/// base, or a recent commit, from the server's list, with search.
struct CompareSheet: View {
    let diff: DiffState
    let choose: (WireDiffTargetEntry) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    private var targets: [WireDiffTargetEntry] { Self.filter(diff.catalog?.targets ?? [], query: query) }

    var body: some View {
        let targets = targets
        let commits = targets.filter { $0.kind == "commit" }
        NavigationStack {
            List {
                if targets.isEmpty {
                    Text(diff.catalog == nil ? "Loading…" : "No matching changes")
                        .foregroundStyle(Theme.muted)
                }
                let others = targets.filter { $0.kind != "commit" }
                if !others.isEmpty {
                    Section { ForEach(others, id: \.targetId, content: row) }
                }
                if !commits.isEmpty {
                    Section("Recent commits") { ForEach(commits, id: \.targetId, content: row) }
                }
            }
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search commits and branches")
            .navigationTitle("Compare")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark", role: .close) { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .accessibilityIdentifier("compare")
    }

    private func row(_ target: WireDiffTargetEntry) -> some View {
        let selected = target.targetId == diff.target?.targetId
        return Button { choose(target) } label: {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(ChangesFormat.title(target))
                        .foregroundStyle(Theme.text)
                        .lineLimit(1)
                    if let detail = detail(target) {
                        Text(detail)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                if selected {
                    Image(systemName: "checkmark")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.accent)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    /// The line under a target: the shown target's counts, or what the
    /// server says about it.
    private func detail(_ target: WireDiffTargetEntry) -> String? {
        if target.targetId == diff.target?.targetId, !diff.files.isEmpty {
            return [ChangesFormat.files(diff.files.count, more: diff.fileCursor != nil), ChangesFormat.lines(diff.additions, diff.deletions)]
                .compactMap { $0 }.joined(separator: " ")
        }
        return ChangesFormat.detail(target)
    }

    /// Targets whose label, subject, refs, or short hash contain every word
    /// of `query`, as on the Mac.
    static func filter(_ targets: [WireDiffTargetEntry], query: String) -> [WireDiffTargetEntry] {
        let words = query.lowercased().split(whereSeparator: \.isWhitespace)
        guard !words.isEmpty else { return targets }
        return targets.filter { target in
            let metadata = target.metadata
            let haystack = [metadata.label, metadata.subject, metadata.refName, metadata.baseRefName, metadata.abbreviatedOid]
                .compactMap { $0 }.joined(separator: " ").lowercased()
            return words.allSatisfy { haystack.contains($0) }
        }
    }
}
