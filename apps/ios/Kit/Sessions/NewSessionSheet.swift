import KitClient
import SwiftUI

/// Starts a session: server, folder, model, thinking, and an optional name.
struct NewSessionSheet: View {
    @State var form: NewSessionForm
    let open: (SessionIdentity) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var typing = false

    var body: some View {
        NavigationStack {
            Form {
                if form.places.count > 1 { serverSection }
                folderSection
                configurationSection
            }
            .disabled(form.pending)
            .navigationTitle("New session")
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(isPresented: $typing) {
                OtherFolderView(form: form)
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                        .disabled(form.pending)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if form.pending {
                        ProgressView()
                    } else {
                        Button("Create", role: .confirm, action: create)
                            .disabled(!form.canCreate)
                            .accessibilityIdentifier("new-session-create")
                    }
                }
            }
        }
        .interactiveDismissDisabled(form.pending)
        .task { await form.load() }
    }

    private func create() {
        Task {
            guard let session = await form.create() else { return }
            dismiss()
            open(session)
        }
    }

    // MARK: Server

    private var serverSection: some View {
        Section {
            Picker("Server", selection: Binding(get: { form.server }, set: { name in Task { await form.select(server: name) } })) {
                ForEach(form.places, id: \.server) { place in
                    Text(place.server).tag(place.server)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .disabled(form.uncertain)
            .listRowInsets(EdgeInsets())
            .listRowBackground(Color.clear)
        } header: {
            Text("Server")
        }
    }

    // MARK: Folder

    private var folderSection: some View {
        Section {
            ForEach(form.recent, id: \.self) { path in
                folderRow(form.display(path), selected: form.folder == .recent(path)) {
                    form.folder = .recent(path)
                }
            }
            if case .typed(let typed) = form.folder {
                folderRow(typed, selected: true) { typing = true }
            }
            Button("Other folder…") { typing = true }
                .foregroundStyle(Theme.accent)
                .accessibilityIdentifier("new-session-other-folder")
        } header: {
            Text("Folder")
        }
    }

    private func folderRow(_ title: String, selected: Bool, choose: @escaping () -> Void) -> some View {
        Button(action: choose) {
            HStack(spacing: 10) {
                Text(title)
                    .font(.system(size: 15, design: .monospaced))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .frame(maxWidth: .infinity, alignment: .leading)
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

    // MARK: Model, thinking, name

    private var configurationSection: some View {
        Section {
            modelRow
            if !form.levels.isEmpty {
                Picker("Thinking", selection: $form.thinking) {
                    ForEach(form.levels, id: \.self) { level in
                        Text(SessionContext.thinkingName(level)).tag(level)
                    }
                }
                .pickerStyle(.navigationLink)
            }
            HStack(spacing: 8) {
                Text("Name")
                    .frame(width: 96, alignment: .leading)
                TextField("Optional", text: $form.name)
                    .accessibilityIdentifier("new-session-name")
            }
        } footer: {
            if let error = form.error { ErrorLine(text: error) }
        }
    }

    @ViewBuilder
    private var modelRow: some View {
        if form.loading {
            HStack {
                Text("Model")
                Spacer()
                ProgressView()
            }
        } else if form.modelGroups.isEmpty {
            LabeledContent("Model") {
                Button("Reload") { Task { await form.load() } }
            }
        } else {
            Picker("Model", selection: $form.model) {
                ForEach(form.modelGroups, id: \.provider) { group in
                    Section(group.provider) {
                        ForEach(group.models, id: \.id) { model in
                            Text(model.name).tag(model.id)
                        }
                    }
                }
            }
            .pickerStyle(.navigationLink)
        }
    }
}

/// A refusal under the form, in the danger color.
private struct ErrorLine: View {
    let text: String

    var body: some View {
        Label {
            Text(text)
        } icon: {
            Image(systemName: "exclamationmark.circle")
        }
        .font(.system(size: 15))
        .foregroundStyle(Theme.danger)
        .padding(.top, 8)
    }
}

/// A folder typed as a path on the server: `~` is the server user's home.
private struct OtherFolderView: View {
    let form: NewSessionForm
    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        Form {
            Section {
                TextField("~/src/project", text: $text)
                    .font(.system(size: 15, design: .monospaced))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .focused($focused)
                    .submitLabel(.done)
                    .onSubmit(choose)
                    .accessibilityIdentifier("new-session-folder")
            } header: {
                Text("Folder on \(form.server)")
            } footer: {
                Text(footer)
            }
        }
        .navigationTitle("Other folder")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Use folder", role: .confirm, action: choose)
                    .disabled(trimmed.isEmpty)
                    .accessibilityIdentifier("new-session-use-folder")
            }
        }
        .onAppear {
            if case .typed(let typed) = form.folder { text = typed } else { text = "~/" }
            focused = true
        }
    }

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

    private var footer: String {
        let home = form.home.map { ", \($0)" } ?? ""
        return "~ is your home folder on \(form.server)\(home). The server checks the folder exists when you create the session."
    }

    private func choose() {
        guard !trimmed.isEmpty else { return }
        form.folder = .typed(trimmed)
        dismiss()
    }
}
