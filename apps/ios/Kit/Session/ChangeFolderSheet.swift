import KitClient
import SwiftUI

/// Moves the session to another folder on its server. The path is read as
/// `cd` reads it: absolute, from `~`, or relative to the current folder.
/// The server's recent session folders are offered below the field.
struct ChangeFolderSheet: View {
    let actions: SessionActions
    /// The session's folder now.
    let current: String
    /// Other folders this server's sessions use, newest first.
    let recent: [String]
    @Environment(\.dismiss) private var dismiss
    @State private var path = ""
    @FocusState private var focused: Bool

    var body: some View {
        let operation = actions.directory
        NavigationStack {
            Form {
                Section {
                    HStack(spacing: 10) {
                        TextField("~/path or ../sibling", text: $path)
                            .font(.system(size: 15, design: .monospaced))
                            .foregroundStyle(Theme.text)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .submitLabel(.go)
                            .focused($focused)
                            .onSubmit(change)
                            // A change that may have reached the server is retried as it was.
                            .disabled(operation.pending || operation.request != nil)
                            .accessibilityIdentifier("change-folder-field")
                        if !path.isEmpty && operation.request == nil {
                            Button { path = "" } label: {
                                Image(systemName: "xmark.circle.fill")
                                    .font(.system(size: 17))
                                    .foregroundStyle(Theme.strongBorder)
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel("Clear")
                        }
                    }
                } header: {
                    Text("Folder")
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Now in \(SessionFormat.directory(current)). A relative path starts there.")
                        if let error = operation.error {
                            Text(error).foregroundStyle(Theme.danger)
                        } else if let reason = actions.folderUnavailableReason, !operation.pending {
                            Text(reason).foregroundStyle(Theme.danger)
                        }
                    }
                }
                if !recent.isEmpty && operation.request == nil {
                    Section("Recent") {
                        ForEach(recent, id: \.self) { folder in
                            Button {
                                path = folder
                            } label: {
                                Text(SessionFormat.directory(folder))
                                    .font(.system(size: 15, design: .monospaced))
                                    .foregroundStyle(Theme.text)
                                    .lineLimit(1)
                                    .truncationMode(.head)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("change-folder-recent")
                        }
                    }
                }
            }
            .navigationTitle("Change folder")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }.disabled(operation.pending)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if operation.pending {
                        ProgressView()
                    } else {
                        Button(operation.request == nil ? "Change" : "Retry", role: .confirm, action: change)
                            .disabled(DirectoryChangeOperation.path(path) == nil || actions.folderUnavailableReason != nil)
                            .accessibilityIdentifier("change-folder-submit")
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .interactiveDismissDisabled(operation.pending)
        .onAppear {
            path = operation.request?.path ?? ""
            focused = operation.request == nil
        }
    }

    private func change() {
        guard DirectoryChangeOperation.path(path) != nil, actions.folderUnavailableReason == nil else { return }
        Task {
            if await actions.changeFolder(to: path) { dismiss() }
        }
    }
}
