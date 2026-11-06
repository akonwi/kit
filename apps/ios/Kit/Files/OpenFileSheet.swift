import KitClient
import SwiftUI

/// Open File: every file in the session's project in one fuzzy list, with
/// the keyboard up to narrow it. A tap opens the file.
struct OpenFileSheet: View {
    let files: WorkspaceFiles
    let client: any FileBrowsingClient
    let session: String
    let open: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""
    @FocusState private var searching: Bool

    var body: some View {
        let matches = files.search(query)
        NavigationStack {
            VStack(spacing: 8) {
            SearchField(query: $query, focused: $searching)
            List {
                ForEach(matches, id: \.self) { path in
                    Button { open(path) } label: { FileRow(path: path) }
                        .buttonStyle(.plain)
                        .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
                        .listRowBackground(Theme.surface)
                        .accessibilityIdentifier("open-file-row")
                }
                if let note = note(matches.count) {
                    Text(note)
                        .font(.system(size: 13))
                        .foregroundStyle(Theme.muted)
                        .listRowBackground(Theme.surface)
                        .listRowSeparator(.hidden)
                        .accessibilityIdentifier("open-file-note")
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .scrollDismissesKeyboard(.immediately)
            .overlay {
                if !files.ready {
                    if let error = files.error {
                        ContentUnavailableView {
                            Label(error, systemImage: "exclamationmark.triangle")
                        } actions: {
                            Button("Try Again") { load() }
                        }
                    } else {
                        ProgressView()
                    }
                }
            }
            }
            .background(Theme.surface)
            .navigationTitle("Open File")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark", role: .close) { dismiss() }
                }
            }
        }
        .presentationDetents([.large])
        .task {
            searching = true
            await files.load(client: client, session: session)
        }
        .accessibilityIdentifier("open-file")
    }

    /// What the list says under its files: none match, or the index stopped
    /// before the end.
    private func note(_ count: Int) -> String? {
        guard files.ready else { return nil }
        if files.files.isEmpty { return "This project has no files." }
        if count == 0 { return files.truncated ? "No matching files. Kit listed only part of this project." : "No matching files" }
        if files.truncated { return "Kit listed only part of this large project, so some files may be missing." }
        return nil
    }

    private func load() {
        Task { await files.load(client: client, session: session, refresh: true) }
    }
}

/// The search above the files, always shown: a magnifier, the query, and a
/// button that clears it.
private struct SearchField: View {
    @Binding var query: String
    var focused: FocusState<Bool>.Binding

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 15))
                .foregroundStyle(Theme.muted)
            TextField("Search files", text: $query)
                .font(.system(size: 17))
                .foregroundStyle(Theme.text)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.search)
                .focused(focused)
                .accessibilityIdentifier("open-file-search")
            if !query.isEmpty {
                Button { query = "" } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(Theme.strongBorder)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 12)
        .frame(height: 40)
        .background(Theme.hover, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .padding(.horizontal, 16)
    }
}

/// A file's name over its folder.
private struct FileRow: View {
    let path: String

    var body: some View {
        let parts = ChangesFormat.split(path)
        HStack(spacing: 12) {
            Image(systemName: "doc")
                .font(.system(size: 15))
                .foregroundStyle(Theme.muted)
                .frame(width: 18)
            VStack(alignment: .leading, spacing: 2) {
                Text(parts.name)
                    .font(.system(size: 14, weight: .semibold, design: .monospaced))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if !parts.folder.isEmpty {
                    Text(String(parts.folder.dropLast()))
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                        .truncationMode(.head)
                }
            }
            Spacer(minLength: 0)
        }
        .frame(minHeight: 58)
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(path)
    }
}
