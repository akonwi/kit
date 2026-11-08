import KitClient
import SwiftUI

/// Starts a session: server, folder, model, thinking, and an optional name.
struct NewSessionSheet: View {
    @State var form: NewSessionForm
    let open: (SessionIdentity) -> Void
    @Environment(\.dismiss) private var dismiss
    /// While the folder field has focus, the sheet offers recent folders in
    /// place of the rest of the form.
    @FocusState private var choosing: Bool

    var body: some View {
        NavigationStack {
            Form {
                if form.places.count > 1 && !choosing { serverSection }
                folderSection
                if choosing { suggestionsSection } else { configurationSection }
            }
            .animation(.default, value: choosing)
            .scrollDismissesKeyboard(.interactively)
            .disabled(form.pending)
            .navigationTitle("New session")
            .navigationBarTitleDisplayMode(.inline)
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
            HStack(spacing: 10) {
                TextField(form.home == nil ? "/path/to/folder" : "~/path/to/folder", text: $form.folderText)
                    .font(.system(size: 15, design: .monospaced))
                    .foregroundStyle(Theme.text)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.done)
                    .focused($choosing)
                    .accessibilityIdentifier("new-session-folder")
                if choosing {
                    if !form.folderText.isEmpty {
                        Button {
                            form.folderText = ""
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .font(.system(size: 17))
                                .foregroundStyle(Theme.strongBorder)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Clear")
                    }
                } else {
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Theme.muted)
                        .accessibilityHidden(true)
                }
            }
            .listRowBackground(FieldBackground(invalid: form.folderError != nil))
        } header: {
            Text("Folder")
        } footer: {
            if let error = form.folderError { ErrorLine(text: error) }
        }
    }

    private var suggestionsSection: some View {
        Section {
            ForEach(form.suggestions, id: \.self) { path in
                Button {
                    form.folder = .recent(path)
                    choosing = false
                } label: {
                    suggestion(path)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(form.display(path))
                .accessibilityIdentifier("new-session-suggestion")
            }
        } header: {
            if !form.suggestions.isEmpty { Text("Recent on \(form.server)") }
        } footer: {
            Text(footer)
        }
    }

    /// A recent folder, with the part already typed muted. One text, so a
    /// long path loses its middle rather than the part being completed.
    private func suggestion(_ path: String) -> some View {
        var text: AttributedString
        if let completion = form.completion(path) {
            var typed = AttributedString(completion.typed)
            typed.foregroundColor = Theme.muted
            var rest = AttributedString(completion.rest)
            rest.foregroundColor = Theme.text
            rest.font = .system(size: 15, weight: .semibold, design: .monospaced)
            text = typed + rest
        } else {
            text = AttributedString(form.display(path))
            text.foregroundColor = Theme.text
        }
        return Text(text)
            .font(.system(size: 15, design: .monospaced))
            .lineLimit(1)
            .truncationMode(.middle)
    }

    private var footer: String {
        let home = form.home.map { ", \($0)" } ?? ""
        let prefix = form.suggestions.isEmpty ? "Type any path." : "Or type any path."
        return "\(prefix) ~ is your home folder on \(form.server)\(home)."
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

/// A form row's background, outlined in the danger color while its value is refused.
private struct FieldBackground: View {
    /// The corners of a one-row section in a grouped form.
    static let radius: CGFloat = 26
    let invalid: Bool

    var body: some View {
        Color(uiColor: .secondarySystemGroupedBackground)
            .overlay {
                if invalid {
                    RoundedRectangle(cornerRadius: Self.radius, style: .continuous)
                        .strokeBorder(Theme.danger, lineWidth: 1.5)
                }
            }
    }
}

/// A refusal under a field or the form, in the danger color.
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
