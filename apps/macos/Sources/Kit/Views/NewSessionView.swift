import AppKit
import SwiftUI

/// Dims the window and holds the new session form 180 pt from its top.
/// Esc cancels unless the form is waiting on the server.
struct NewSessionOverlay: View {
    @Environment(\.mica) private var theme
    let client: any SessionCreationClient
    let suggestedModel: String?
    let close: () -> Void
    var loadDefaults: @Sendable () async throws -> SharedSettingsStore.Snapshot = { try await SharedSettingsStore.shared.load() }
    let open: (SessionExcerpt) -> Void

    static let top: CGFloat = 180

    var body: some View {
        ZStack(alignment: .top) {
            Color.black.opacity(0.45).contentShape(Rectangle())
                .accessibilityHidden(true)
            NewSessionView(client: client, suggestedModel: suggestedModel, close: close, open: open, loadDefaults: loadDefaults)
                .clipShape(RoundedRectangle(cornerRadius: 4))
                .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
                .shadow(color: .black.opacity(theme.dark ? 0.5 : 0.18), radius: 24, y: 18)
                .padding(.top, Self.top)
        }
        .ignoresSafeArea()
    }
}

/// The new session form: folder, optional name, model and thinking level,
/// and whether the session is temporary.
struct NewSessionView: View {
    let client: any SessionCreationClient
    let suggestedModel: String?
    let close: () -> Void
    let open: (SessionExcerpt) -> Void
    var loadDefaults: @Sendable () async throws -> SharedSettingsStore.Snapshot = { try await SharedSettingsStore.shared.load() }
    @Environment(\.mica) private var theme
    @Environment(\.openSettings) private var openSettings
    @State private var temporary = false
    @State private var creationID = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
    @State private var name = ""
    @State private var directory = FileManager.default.homeDirectoryForCurrentUser.path
    @State private var models: [WireModelCapability] = []
    @State private var model = ""
    @State private var thinking = ""
    @State private var loading = true
    @State private var pending = false
    @State private var error: String?
    @State private var uncertain = false
    @State private var popover = AnchoredPopover()
    @State private var openMenu: ComposerChipMenu?
    @State private var modelAnchor = ComposerPickerAnchor()
    @State private var thinkingAnchor = ComposerPickerAnchor()
    @FocusState private var nameFocused: Bool

    static let width: CGFloat = 460

    private var levels: [String] { ComposerView.thinkingLevels(model: model, models: models) }
    private var selectedModel: WireModelCapability? { models.first { $0.id == model } }
    private var canCreate: Bool { !directory.isEmpty && !model.isEmpty && (levels.isEmpty || !thinking.isEmpty) && !loading && !uncertain && !pending }
    private var canCancel: Bool { !pending && !(temporary && uncertain) }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("New session").font(.kit(size: 15, weight: .semibold))
            Group {
                field("Working directory") { directoryField }
                field("Name") { nameField }
                if !loading && models.isEmpty { noModels } else { configuration }
                temporaryToggle
            }
            .disabled(pending)
            .opacity(pending ? 0.45 : 1)
            VStack(alignment: .leading, spacing: 18) {
                if let error { errorLine(error) }
                Rule()
            }
            buttons
        }
        .font(.kit(size: 13))
        .padding(24).frame(width: Self.width)
        .foregroundStyle(theme.text).background(theme.surface).tint(theme.accent)
        .task { await loadModels() }
        .onAppear { nameFocused = true }
        .onDisappear { popover.dismiss() }
        .onExitCommand { if canCancel { close() } }
    }

    // MARK: Fields

    private func field(_ label: String, @ViewBuilder content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(.kit(size: 12)).foregroundStyle(theme.muted)
            content()
        }
    }

    private var directoryField: some View {
        HStack(spacing: 8) {
            IconView(.folderSimple, size: 14).foregroundStyle(theme.muted)
            Text(WorkspaceLocation.homeRelative(directory))
                .font(.kit(size: 12, design: .monospaced)).lineLimit(1).truncationMode(.head)
                .frame(maxWidth: .infinity, alignment: .leading)
                .help(directory)
            Rectangle().fill(theme.border).frame(width: 1, height: 16)
            Button("Choose…", action: chooseDirectory)
                .buttonStyle(.plain).font(.kit(size: 12, weight: .medium))
                .padding(.leading, 2)
        }
        .padding(.horizontal, 10).frame(height: 30)
        .fieldBox(theme)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Working directory, \(directory)")
    }

    private var nameField: some View {
        TextField("", text: $name, prompt: Text("Optional").foregroundStyle(theme.strongBorder))
            .textFieldStyle(.plain).focused($nameFocused)
            .onSubmit { if canCreate { Task { await create() } } }
            .padding(.horizontal, 10).frame(height: 30)
            .fieldBox(theme, focused: nameFocused)
            .accessibilityLabel("Session name")
    }

    private var configuration: some View {
        HStack(alignment: .top, spacing: 12) {
            field("Model") {
                PickerField(open: openMenu == .model, anchor: modelAnchor, action: { toggle(.model) }) {
                    if loading {
                        KitSpinner()
                        Text("Loading models…").foregroundStyle(theme.muted)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    } else {
                        Text(selectedModel?.name ?? model).lineLimit(1).truncationMode(.middle)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        if let provider = selectedModel?.provider {
                            Text(provider).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                        }
                    }
                }
                .disabled(loading)
                .accessibilityLabel("Model: \(selectedModel.map(ComposerView.modelLabel) ?? "none")")
            }
            field("Thinking") {
                PickerField(open: openMenu == .thinking, anchor: thinkingAnchor, action: { toggle(.thinking) }) {
                    Text(loading || levels.isEmpty ? "—" : thinking.capitalized)
                        .foregroundStyle(loading || levels.isEmpty ? theme.muted : theme.text)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .disabled(loading || levels.isEmpty)
                .help(!loading && levels.isEmpty ? "This model has no thinking levels" : "Thinking level")
                .accessibilityLabel("Thinking: \(levels.isEmpty ? "none" : thinking)")
            }
            .frame(width: 132)
        }
    }

    private var noModels: some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text("No models available")
                Text("Sign in to a provider to start a session.").font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
            Spacer(minLength: 8)
            Button("Reload") { Task { await loadModels() } }
                .buttonStyle(FormButtonStyle(fill: .surface))
            Button("Sign in…") { SettingsSection.providers.open(with: openSettings) }
                .buttonStyle(FormButtonStyle(fill: .primary))
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .fieldBox(theme)
    }

    private var temporaryToggle: some View {
        Toggle(isOn: $temporary) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Temporary session")
                Text("Not saved. Gone when you dispose of it or the daemon stops.")
                    .font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
        }
        .toggleStyle(FormSwitchStyle())
        .disabled(uncertain || !(client is any SessionDisposalClient))
    }

    private func errorLine(_ message: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            IconView(.warningCircle, size: 12)
            Text(message).font(.kit(size: 12)).textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
        .foregroundStyle(theme.danger)
    }

    private var buttons: some View {
        HStack(spacing: 8) {
            if temporary && uncertain {
                Button("Dispose temporary session") { Task { await disposeUncertain() } }
                    .buttonStyle(FormButtonStyle())
                    .disabled(pending)
            }
            Spacer()
            Button("Cancel", action: close)
                .buttonStyle(FormButtonStyle())
                .keyboardShortcut(.cancelAction)
                .disabled(!canCancel)
                .opacity(pending ? 0.45 : 1)
            Button { Task { await create() } } label: {
                HStack(spacing: 6) {
                    if pending { KitSpinner() }
                    Text(pending ? "Creating…" : "Create")
                }
            }
            .buttonStyle(FormButtonStyle(fill: .primary))
            .keyboardShortcut(.defaultAction)
            .disabled(!canCreate)
        }
    }

    // MARK: Popovers

    private func toggle(_ menu: ComposerChipMenu) {
        if popover.isOpen {
            let same = openMenu == menu
            popover.dismiss()
            if same { return }
        } else if openMenu == nil, let closedAt = popover.closedAt, Date().timeIntervalSince(closedAt) < 0.3 {
            // The click that reached this field already closed its popover.
            return
        }
        let anchor = menu == .model ? modelAnchor : thinkingAnchor
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        let popover = popover, theme = theme
        let content: AnyView = switch menu {
        case .model:
            AnyView(ComposerModelPicker(models: models, selectedID: model,
                select: { id in popover.dismiss(); choose(model: id) },
                reload: { Task { await loadModels() } },
                dismiss: { popover.dismiss() })
                .popoverCard(theme).accessibilityLabel("Model"))
        case .thinking:
            AnyView(ComposerThinkingMenu(theme: theme, levels: levels, selected: thinking,
                select: { thinking = $0 }, dismiss: { popover.dismiss() }))
        }
        openMenu = menu
        popover.show(anchor: frame, parent: window, edge: .below, content: content) {
            if openMenu == menu { openMenu = nil }
        }
    }

    private func choose(model id: String) {
        model = id
        if !levels.contains(thinking) { thinking = Self.defaultThinking(levels) }
    }

    static func defaultThinking(_ levels: [String]) -> String {
        levels.contains("medium") ? "medium" : levels.first ?? ""
    }

    private func chooseDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true; panel.canChooseFiles = false
        panel.allowsMultipleSelection = false; panel.prompt = "Choose folder"
        panel.directoryURL = URL(fileURLWithPath: directory, isDirectory: true)
        panel.begin { result in
            if result == .OK, let url = panel.url { directory = url.path }
        }
    }

    // MARK: Server

    private func loadModels() async {
        loading = true; error = nil
        defer { loading = false }
        do {
            models = try await client.models()
            let defaults: SharedSettingsStore.Snapshot
            do { defaults = try await loadDefaults() }
            catch {
                defaults = .init()
                self.error = "Couldn’t load the default model. Choose a model above. " + error.localizedDescription
            }
            model = SharedSettingsStore.preferredModel(defaults, available: models.map(\.id), suggested: suggestedModel)
            thinking = Self.defaultThinking(levels)
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
            close()
        } catch { self.error = error.localizedDescription }
    }

    private func create() async {
        guard canCreate else { return }
        popover.dismiss()
        pending = true; error = nil
        defer { pending = false }
        if temporary { TemporarySessions.shared.remember(server: client.serverID, session: creationID) }
        do {
            var session = try await client.createSession(WireCreateSessionInput(id: creationID, cwd: directory,
                name: name.trimmingCharacters(in: .whitespacesAndNewlines), model: model,
                thinkingLevel: levels.isEmpty ? nil : thinking, temporary: temporary))
            if temporary {
                TemporarySessions.shared.remember(server: client.serverID, session: session.id)
                session.isTemporary = true
            }
            close(); open(session)
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
                    close(); open(recovered)
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

/// A 30 pt field that opens a popover below itself; it lights while hovered
/// or open.
struct PickerField<Label: View>: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let open: Bool
    let anchor: ComposerPickerAnchor
    let action: () -> Void
    @ViewBuilder let label: Label
    @State private var hovering = false

    var body: some View {
        let lit = enabled && (open || hovering)
        Button(action: action) {
            HStack(spacing: 8) {
                label
                IconView(.caretUpDown, size: 12).foregroundStyle(theme.muted)
            }
            .padding(.horizontal, 10).frame(height: 30)
            .background(lit ? theme.frameHover : theme.raised, in: RoundedRectangle(cornerRadius: 3))
            .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(lit ? theme.strongBorder : theme.border) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .background(ComposerPickerAnchorView(anchor: anchor))
        .onHover { hovering = $0 }
    }
}

/// The form's 28 pt buttons: bordered on the raised surface, or filled for
/// the action the form exists for.
struct FormButtonStyle: ButtonStyle {
    enum Fill { case raised, surface, primary }
    var fill = Fill.raised

    func makeBody(configuration: Configuration) -> some View {
        FormButtonLabel(configuration: configuration, fill: fill)
    }

    private struct FormButtonLabel: View {
        @Environment(\.mica) private var theme
        @Environment(\.isEnabled) private var enabled
        let configuration: Configuration
        let fill: Fill

        var body: some View {
            let primary = fill == .primary
            configuration.label
                .font(.kit(size: 12, weight: .medium))
                .foregroundStyle(primary ? theme.surface : theme.text)
                .padding(.horizontal, 12).frame(height: 28)
                .background(background.opacity(configuration.isPressed ? 0.8 : 1), in: RoundedRectangle(cornerRadius: 3))
                .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(primary ? theme.text : theme.border) }
                .contentShape(Rectangle())
                .opacity(enabled ? 1 : 0.45)
        }

        private var background: Color {
            switch fill {
            case .raised: theme.raised
            case .surface: theme.surface
            case .primary: theme.text
            }
        }
    }
}

/// A 36 × 20 pt switch: the accent track with a white knob on the right when
/// on, the strong border track with a text-coloured knob when off.
struct FormSwitchStyle: ToggleStyle {
    func makeBody(configuration: Configuration) -> some View { FormSwitch(configuration: configuration) }

    private struct FormSwitch: View {
        @Environment(\.mica) private var theme
        @Environment(\.isEnabled) private var enabled
        let configuration: Configuration

        var body: some View {
            Button { configuration.isOn.toggle() } label: {
                HStack(spacing: 10) {
                    ZStack(alignment: configuration.isOn ? .trailing : .leading) {
                        RoundedRectangle(cornerRadius: 3).fill(configuration.isOn ? theme.accent : theme.strongBorder)
                        RoundedRectangle(cornerRadius: 2).fill(configuration.isOn ? Color.white : theme.text)
                            .frame(width: 14, height: 14).padding(3)
                    }
                    .frame(width: 36, height: 20)
                    configuration.label
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .opacity(enabled ? 1 : 0.45)
            .accessibilityRepresentation { Toggle(isOn: configuration.$isOn) { configuration.label } }
        }
    }
}

extension View {
    /// A form field's box: the raised surface, a hairline border that turns
    /// accent while the field has focus, and 3 pt corners.
    func fieldBox(_ theme: MicaTheme, focused: Bool = false) -> some View {
        background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
            .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(focused ? theme.accent : theme.border) }
    }
}
