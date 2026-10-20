import SwiftUI

/// Settings › Models: the settings the Kit server uses for every session,
/// read and changed through its settings API.
struct ModelSettingsView: View {
    @Environment(\.mica) private var theme
    @Environment(\.scenePhase) private var phase
    @State private var editor: ServerSettingsEditor
    @State private var adding = false
    @State private var newModel = ""
    @State private var newTokens = ""
    @FocusState private var addingFocus: Bool

    /// client is the server whose settings these are.
    init(client: any ServerSettingsClient = LocalClient()) {
        _editor = State(initialValue: ServerSettingsEditor(client: client))
    }

    private var settings: ServerSettings { editor.settings }
    private var models: [WireModelCapability] { editor.models }

    var body: some View {
        VStack(alignment: .leading, spacing: 26) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Models").font(.kit(size: 23, weight: .semibold))
                Text("The Kit server on this Mac uses these for every session.").foregroundStyle(theme.muted)
            }
            if !editor.loaded && editor.loadError == nil { KitSpinner("Loading settings…") }
            if let error = editor.loadError {
                VStack(alignment: .leading, spacing: 8) {
                    Text(error).foregroundStyle(theme.danger).textSelection(.enabled)
                    Button("Reload settings") { Task { await editor.load() } }
                }
            }
            if editor.loaded {
                VStack(alignment: .leading, spacing: 26) {
                    VStack(alignment: .leading, spacing: 18) {
                        row("Default model", "Used for new sessions. Existing sessions keep their selected model.", field: .defaultModel) {
                            modelPicker("Default model", none: automatic,
                                        selection: Binding(get: { settings.defaultModel }, set: { save(.defaultModel($0)) }))
                        }
                        row("Compaction model", "Summarizes context when a session compacts. Sessions use it after Reload.", field: .compactionModel) {
                            modelPicker("Compaction model", none: "Session’s own model",
                                        selection: Binding(get: { settings.compactionModel }, set: { save(.compactionModel($0)) }))
                        }
                        row("Prompt cache", "Long asks providers to keep prompt caches for an hour or more.", field: .promptCacheRetention) {
                            Picker("Prompt cache", selection: Binding(get: { settings.promptCacheRetention },
                                                                      set: { save(.promptCacheRetention($0)) })) {
                                Text("Short").tag(PromptCacheRetention.short)
                                Text("Long").tag(PromptCacheRetention.long)
                            }.labelsHidden().pickerStyle(.segmented).frame(width: 140)
                        }
                        VStack(alignment: .leading, spacing: 6) {
                            Toggle(isOn: Binding(get: { settings.readClaudeConfigs }, set: { save(.readClaudeConfigs($0)) })) {
                                VStack(alignment: .leading, spacing: 5) {
                                    Text("Claude Code commands")
                                    Text("Offers a project’s .claude/commands as prompt commands. Sessions read them after Reload.")
                                        .font(.kit(size: 12)).foregroundStyle(theme.muted)
                                }
                            }.toggleStyle(FormSwitchStyle(trailing: true))
                            failure(.readClaudeConfigs)
                        }
                    }
                    Divider()
                    VStack(alignment: .leading, spacing: 12) {
                        HStack {
                            Text("Context window overrides").fontWeight(.semibold)
                            Spacer()
                            Button("Add model", icon: .plus) { adding = true; addingFocus = true }
                                .buttonStyle(.link)
                        }
                        Text("Override the context limit for an exact provider and model.")
                            .font(.kit(size: 12)).foregroundStyle(theme.muted)
                        if adding { addForm }
                        if settings.contextWindows.isEmpty {
                            Text("All models use their default context limits.").foregroundStyle(theme.muted).padding(.vertical, 18)
                        }
                        ForEach(settings.contextWindows.keys.sorted(), id: \.self) { selector in
                            ModelOverrideRow(selector: selector, name: models.first { $0.id == selector }?.name,
                                             tokens: settings.contextWindows[selector]!, saving: editor.saving != nil) { tokens in
                                save(.contextWindow(selector, tokens))
                            }
                            failure(.contextWindow(selector))
                            Divider()
                        }
                        Text("Remove an override to use the model’s default context limit.")
                            .font(.kit(size: 12)).foregroundStyle(theme.muted)
                    }
                }.disabled(editor.saving != nil)
            }
            if let catalogError = editor.catalogError {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Model catalog unavailable. Saved settings can still be edited.").foregroundStyle(theme.muted)
                    Text(catalogError).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    Button("Reload models") { Task { await editor.loadCatalog() } }
                }
            }
        }
        .task { await editor.load(); await editor.loadCatalog() }
        .onChange(of: phase) { if phase == .active && editor.saving == nil && !adding { Task { await editor.load() } } }
    }

    /// The default model's "none": the server's current choice.
    private var automatic: String {
        settings.newSessionModel.isEmpty ? "Automatic" : "Automatic · \(editor.name(of: settings.newSessionModel))"
    }

    private func row(_ title: String, _ detail: String, field: ServerSettingsEditor.Field,
                     @ViewBuilder control: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 20) {
                VStack(alignment: .leading, spacing: 5) {
                    Text(title)
                    Text(detail).font(.kit(size: 12)).foregroundStyle(theme.muted)
                }
                Spacer(minLength: 0)
                if editor.saving == field { KitSpinner(nil) }
                control()
            }
            failure(field)
        }
    }

    @ViewBuilder private func failure(_ field: ServerSettingsEditor.Field) -> some View {
        if let message = editor.failure(for: field) {
            Text(message).font(.kit(size: 12)).foregroundStyle(theme.danger).textSelection(.enabled)
        }
    }

    /// A model menu whose empty selection is `none`. A saved model the
    /// catalog doesn't offer stays listed by its id.
    private func modelPicker(_ title: String, none: String, selection: Binding<String>) -> some View {
        let current = selection.wrappedValue
        return Picker(title, selection: selection) {
            Text(none).tag("")
            if !current.isEmpty && !models.contains(where: { $0.id == current }) {
                Text(current).tag(current)
            }
            ForEach(models, id: \.id) { Text("\($0.name) · \($0.provider)").tag($0.id) }
        }.labelsHidden().frame(width: 230)
    }

    private var addForm: some View {
        VStack(alignment: .leading, spacing: 10) {
            if !models.isEmpty {
                Picker("Model", selection: $newModel) {
                    Text("Choose a model…").tag("")
                    ForEach(models.filter { settings.contextWindows[$0.id] == nil }, id: \.id) {
                        Text("\($0.name) · \($0.provider)").tag($0.id)
                    }
                }.onChange(of: newModel) {
                    if let model = models.first(where: { $0.id == newModel }) { newTokens = String(model.contextWindow) }
                }
            }
            TextField("provider/model", text: $newModel).textFieldStyle(.roundedBorder)
                .font(.kit(size: 12, design: .monospaced)).focused($addingFocus).accessibilityLabel("Model identifier")
            HStack {
                TextField("Context limit", text: $newTokens).textFieldStyle(.roundedBorder).frame(width: 140)
                    .accessibilityLabel("New context window")
                Text("tokens").foregroundStyle(theme.muted)
                Spacer()
                Button("Cancel") { adding = false }.keyboardShortcut(.cancelAction)
                Button("Add") {
                    guard let tokens = Int(newTokens) else { return }
                    save(.contextWindow(newModel, tokens), as: .newOverride, closeAddForm: true)
                }.disabled(!Self.validSelector(newModel) || (Int(newTokens) ?? 0) <= 0)
            }
            failure(.newOverride)
        }.padding(.vertical, 12)
    }

    static func validSelector(_ value: String) -> Bool {
        let parts = value.split(separator: "/", maxSplits: 1, omittingEmptySubsequences: false)
        return parts.count == 2 && parts.allSatisfy { !$0.isEmpty && $0.trimmingCharacters(in: .whitespacesAndNewlines) == $0 }
    }

    private func save(_ change: ServerSettingsChange, as field: ServerSettingsEditor.Field? = nil, closeAddForm: Bool = false) {
        Task {
            if await editor.save(change, as: field), closeAddForm { adding = false; newModel = ""; newTokens = "" }
        }
    }
}

private struct ModelOverrideRow: View {
    let selector: String
    let name: String?
    let tokens: Int
    let saving: Bool
    let update: (Int?) -> Void
    @Environment(\.mica) private var theme
    @State private var input: String
    @FocusState private var focused: Bool

    init(selector: String, name: String?, tokens: Int, saving: Bool, update: @escaping (Int?) -> Void) {
        self.selector = selector; self.name = name; self.tokens = tokens; self.saving = saving; self.update = update
        _input = State(initialValue: String(tokens))
    }
    private var valid: Bool { (Int(input) ?? 0) > 0 }
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(name ?? String(selector.split(separator: "/", maxSplits: 1).last ?? ""))
                    Text(selector).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                        .lineLimit(1).truncationMode(.middle).help(selector)
                }
                Spacer(minLength: 8)
                TextField("Tokens", text: $input).textFieldStyle(.roundedBorder)
                    .font(.kit(size: 12, design: .monospaced)).frame(width: 110).focused($focused)
                    .accessibilityLabel("Context window for \(name ?? selector)")
                    .onSubmit(commit).onChange(of: focused) { if !focused { commit() } }
                Text("tokens").font(.kit(size: 11)).foregroundStyle(theme.muted)
                Button { update(nil) } label: { IconView(.x, size: 10) }
                    .buttonStyle(.plain).foregroundStyle(theme.muted).accessibilityLabel("Remove \(name ?? selector) override")
            }
            if !valid { Text("Enter a positive whole number of tokens.").font(.kit(size: 12)).foregroundStyle(theme.danger) }
        }.padding(.vertical, 8)
        .onChange(of: tokens) { if !focused { input = String(tokens) } }
    }
    private func commit() {
        guard !saving, let value = Int(input), value > 0, value != tokens else { return }
        update(value)
    }
}
