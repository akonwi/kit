import SwiftUI

struct ModelSettingsView: View {
    var store: SharedSettingsStore = .shared
    @Environment(\.mica) private var theme
    @Environment(\.scenePhase) private var phase
    @State private var settings = SharedSettingsStore.Snapshot()
    @State private var models: [WireModelCapability] = []
    @State private var loaded = false
    @State private var saving = false
    @State private var error: String?
    @State private var catalogError: String?
    @State private var adding = false
    @State private var newModel = ""
    @State private var newTokens = ""
    @FocusState private var addingFocus: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 26) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Models").font(.kit(size: 23, weight: .semibold))
                Text("Shared by the Kit app and TUI.").foregroundStyle(theme.muted)
                Text("Stored in settings.json").font(.kit(size: 11, design: .monospaced))
                    .foregroundStyle(theme.muted).help(SharedSettingsStore.home.appendingPathComponent("settings.json").path)
            }
            if !loaded && error == nil { KitSpinner("Loading settings…") }
            if let error {
                VStack(alignment: .leading, spacing: 8) {
                    Text(error).foregroundStyle(theme.danger).textSelection(.enabled)
                    Button("Reload settings") { Task { await load() } }
                }
            }
            if loaded {
                VStack(alignment: .leading, spacing: 26) {
                    HStack(alignment: .top, spacing: 20) {
                        VStack(alignment: .leading, spacing: 5) {
                            Text("Default model")
                            Text("Used for new sessions. Existing sessions keep their selected model.")
                                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                        }
                        Spacer(minLength: 0)
                        Picker("Default model", selection: Binding(get: { settings.defaultModel }, set: { save(.defaultModel($0)) })) {
                            Text("Automatic").tag("")
                            if !settings.defaultModel.isEmpty && !models.contains(where: { $0.id == settings.defaultModel }) {
                                Text(settings.defaultModel).tag(settings.defaultModel)
                            }
                            ForEach(models, id: \.id) { Text("\($0.name) · \($0.provider)").tag($0.id) }
                        }.labelsHidden().frame(width: 230)
                    }
                    Divider()
                    VStack(alignment: .leading, spacing: 12) {
                        HStack {
                            Text("Context window overrides").fontWeight(.semibold)
                            Spacer()
                            Button("Add model", systemImage: "plus") { adding = true; addingFocus = true }
                                .buttonStyle(.link)
                        }
                        Text("Override the context limit for an exact provider and model.")
                            .font(.kit(size: 12)).foregroundStyle(theme.muted)
                        if adding { addForm }
                        if settings.overrides.isEmpty {
                            Text("All models use their default context limits.").foregroundStyle(theme.muted).padding(.vertical, 18)
                        }
                        ForEach(settings.overrides.keys.sorted(), id: \.self) { selector in
                            ModelOverrideRow(selector: selector, name: models.first { $0.id == selector }?.name,
                                             tokens: settings.overrides[selector]!, saving: saving) { tokens in
                                save(.contextWindow(selector, tokens))
                            }
                            Divider()
                        }
                        Text("Remove an override to use the model’s default context limit.")
                            .font(.kit(size: 12)).foregroundStyle(theme.muted)
                    }
                }.disabled(saving)
            }
            if let catalogError {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Model catalog unavailable. Saved overrides can still be edited.").foregroundStyle(theme.muted)
                    Text(catalogError).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    Button("Reload models") { Task { await loadCatalog() } }
                }
            }
        }
        .task { await load(); await loadCatalog() }
        .onChange(of: phase) { if phase == .active && !saving && !adding { Task { await load() } } }
    }

    private var addForm: some View {
        VStack(alignment: .leading, spacing: 10) {
            if !models.isEmpty {
                Picker("Model", selection: $newModel) {
                    Text("Choose a model…").tag("")
                    ForEach(models.filter { settings.overrides[$0.id] == nil }, id: \.id) {
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
                    save(.contextWindow(newModel, tokens), closeAddForm: true)
                }.disabled(!SharedSettingsStore.validSelector(newModel) || (Int(newTokens) ?? 0) <= 0)
            }
        }.padding(.vertical, 12)
    }

    private func load() async {
        do { settings = try await store.load(); loaded = true; error = nil }
        catch { self.error = error.localizedDescription }
    }
    private func loadCatalog() async {
        do { models = try await HTTPClient.local().models(); catalogError = nil }
        catch { catalogError = error.localizedDescription }
    }
    private func save(_ change: SharedSettingsStore.Change, closeAddForm: Bool = false) {
        guard !saving else { return }
        saving = true
        Task {
            defer { saving = false }
            do {
                settings = try await store.update(change); error = nil
                if closeAddForm { adding = false; newModel = ""; newTokens = "" }
            } catch { self.error = error.localizedDescription }
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
                Button { update(nil) } label: { Image(systemName: "xmark") }
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
