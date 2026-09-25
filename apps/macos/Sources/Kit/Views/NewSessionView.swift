import AppKit
import SwiftUI

struct NewSessionView: View {
    let client: any SessionCreationClient
    let suggestedModel: String?
    let open: (SessionExcerpt) -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.mica) private var theme
    @State private var temporary = false
    @State private var creationID = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
    @State private var name = ""
    @State private var directory = ""
    @State private var models: [WireModelCapability] = []
    @State private var model = ""
    @State private var thinking = ""
    @State private var loading = true
    @State private var pending = false
    @State private var error: String?
    @State private var uncertain = false

    private var levels: [String] { models.first { $0.id == model }?.thinkingLevels?.map(\.rawValue) ?? [] }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("New session").font(.kit(size: 20, weight: .semibold))
            VStack(alignment: .leading, spacing: 8) {
                Text("Working directory").foregroundStyle(theme.muted)
                HStack {
                    Text(directory.isEmpty ? "Choose a folder…" : WorkspaceLocation.homeRelative(directory))
                        .font(.kit(size: 13, design: .monospaced)).lineLimit(2).truncationMode(.head)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Button("Choose…", action: chooseDirectory)
                }.padding(12).background(theme.raised, in: RoundedRectangle(cornerRadius: 10))
            }
            TextField("Name (optional)", text: $name).textFieldStyle(.roundedBorder)
                .accessibilityLabel("Session name")
            Toggle("Temporary session", isOn: $temporary).disabled(uncertain || !(client is any SessionDisposalClient))
            if temporary {
                Text("Kept only in daemon memory until you dispose of it or the daemon stops. Closing a tab or quitting the app does not dispose of it.")
                    .font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
            if loading { KitSpinner("Loading models…").controlSize(.small) }
            else if models.isEmpty {
                Text("No available models. Connect a provider in Kit’s terminal app, then try again.")
                    .foregroundStyle(theme.muted)
                Button("Reload models") { Task { await loadModels() } }
            } else {
                Picker("Model", selection: $model) {
                    ForEach(models, id: \.id) { Text("\($0.name) · \($0.provider)").tag($0.id) }
                }
                Picker("Thinking", selection: $thinking) {
                    ForEach(levels, id: \.self) { Text($0.capitalized).tag($0) }
                }
            }
            if let error { Text(error).foregroundStyle(theme.danger).textSelection(.enabled) }
            HStack {
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction).disabled(temporary && uncertain)
                if temporary && uncertain {
                    Button("Dispose temporary session") { Task { await disposeUncertain() } }
                }
                Spacer()
                if pending { KitSpinner() }
                Button("Create session") { Task { await create() } }
                    .keyboardShortcut(.defaultAction)
                    .disabled(directory.isEmpty || model.isEmpty || thinking.isEmpty || loading || uncertain)
            }
        }
        .font(.kit(size: 13)).padding(28).frame(width: 490)
        .foregroundStyle(theme.text).background(theme.surface).tint(theme.accent)
        .disabled(pending).interactiveDismissDisabled(pending || (temporary && uncertain))
        .task { await loadModels() }
        .onChange(of: model) { thinking = levels.contains("medium") ? "medium" : levels.first ?? "" }
    }

    private func chooseDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true; panel.canChooseFiles = false
        panel.allowsMultipleSelection = false; panel.prompt = "Choose folder"
        panel.begin { result in
            if result == .OK, let url = panel.url { directory = url.path }
        }
    }

    private func loadModels() async {
        loading = true; error = nil
        defer { loading = false }
        do {
            models = try await client.models()
            let defaults: SharedSettingsStore.Snapshot
            do { defaults = try await SharedSettingsStore.shared.load() }
            catch {
                defaults = .init()
                self.error = "Couldn’t load the default model. Choose a model below. " + error.localizedDescription
            }
            model = SharedSettingsStore.preferredModel(defaults, available: models.map(\.id), suggested: suggestedModel)
            thinking = levels.contains("medium") ? "medium" : levels.first ?? ""
        } catch { self.error = error.localizedDescription }
    }

    private func disposeUncertain() async {
        guard !pending, let disposal = client as? any SessionDisposalClient else { return }
        pending = true
        defer { pending = false }
        do {
            do { try await disposal.disposeSession(creationID) }
            catch ClientError.http(404) {}
            TemporarySessions.shared.forget(SessionIdentity(server: client.serverID, session: creationID))
            dismiss()
        } catch { self.error = error.localizedDescription }
    }

    private func create() async {
        guard !pending, !uncertain else { return }
        pending = true; error = nil
        defer { pending = false }
        if temporary { TemporarySessions.shared.remember(server: client.serverID, session: creationID) }
        do {
            var session = try await client.createSession(WireCreateSessionInput(id: creationID, cwd: directory,
                name: name.trimmingCharacters(in: .whitespacesAndNewlines), model: model,
                thinkingLevel: thinking, temporary: temporary))
            if temporary {
                TemporarySessions.shared.remember(server: client.serverID, session: session.id)
                session.isTemporary = true
            }
            dismiss(); open(session)
        } catch {
            if temporary {
                let definitelyRejected: Bool
                if error is MutationNotSent { definitelyRejected = true }
                else if case ClientError.http(let code) = error { definitelyRejected = [400, 401, 403, 404, 409, 422].contains(code) }
                else { definitelyRejected = false }
                if definitelyRejected {
                    TemporarySessions.shared.forget(SessionIdentity(server: client.serverID, session: creationID))
                    self.error = error.localizedDescription
                } else if var recovered = try? await client.snapshot(creationID) {
                    recovered.isTemporary = true
                    TemporarySessions.shared.remember(server: client.serverID, session: creationID)
                    dismiss(); open(recovered)
                } else {
                    uncertain = true
                    self.error = "The server did not confirm creation. Dispose of this temporary session before starting another. " + error.localizedDescription
                }
                return
            }
            if error is MutationNotSent { self.error = error.localizedDescription }
            else if case ClientError.http(let code) = error, [400, 401, 403, 404, 409, 422].contains(code) {
                self.error = error.localizedDescription
            } else {
                uncertain = true
                self.error = "Kit may have created the session. Close this form and refresh recent sessions before trying again. \(error.localizedDescription)"
            }
        }
    }
}
