import KitClient
import SwiftUI

/// The session menu, behind a ••• button so the title keeps the header:
/// changes and files, context, model, thinking, compact, rename, fork,
/// details, and delete.
struct SessionMenu: View {
    let session: SessionExcerpt
    let actions: SessionActions
    let diff: DiffState
    let present: (SessionScreen.Sheet) -> Void
    let openChanges: () -> Void
    let openFile: () -> Void
    let confirmDelete: () -> Void

    var body: some View {
        Menu {
            Section {
                Button(action: openChanges) {
                    Text("Changes")
                    Text(ChangesFormat.summary(diff))
                    Image(systemName: "plus.forwardslash.minus")
                }
                .accessibilityLabel("Changes, \(ChangesFormat.summary(diff))")
                .accessibilityIdentifier("changes")
                Button("Open File…", systemImage: "doc", action: openFile)
                    .accessibilityIdentifier("open-file")
            }
            Section(contextTitle) {
                modelPicker
                if !actions.thinkingLevels.isEmpty { thinkingPicker }
            }
            Section {
                Button {
                    actions.compact()
                } label: {
                    Label {
                        Text("Compact context")
                        if let reason = actions.compactionUnavailableReason { Text(reason) }
                    } icon: { Image(systemName: "arrow.down.right.and.arrow.up.left") }
                }
                .disabled(actions.compactionUnavailableReason != nil)
                Button("Rename…", systemImage: "pencil") { present(.rename) }
                    .disabled(!actions.live)
                Button {
                    present(.fork)
                } label: {
                    Label {
                        Text("Fork…")
                        if let reason = actions.forkUnavailableReason { Text(reason) }
                    } icon: { Image(systemName: "arrow.triangle.branch") }
                }
                .disabled(actions.forkUnavailableReason != nil)
                Button("Details", systemImage: "info.circle") { present(.details) }
            }
            Section {
                Button(actions.temporary ? "Dispose…" : "Delete…", systemImage: "trash", role: .destructive, action: confirmDelete)
                    .disabled(!actions.live)
            }
        } label: {
            // Neutral like the back button opposite it.
            Image(systemName: "ellipsis")
                .foregroundStyle(Theme.text)
                .accessibilityLabel("Session menu")
        }
        .accessibilityIdentifier("session-menu")
        .task { await actions.loadModels() }
    }

    private var contextTitle: String {
        guard let context = SessionContext(session) else { return "Context unknown" }
        return "Context  \(context.tokensLabel) · \(context.percent)%"
    }

    // MARK: Model

    @ViewBuilder
    private var modelPicker: some View {
        let selection = Binding(get: { actions.model }, set: { actions.changeModel($0) })
        if actions.configuration.models.isEmpty {
            Menu {
                if actions.configuration.loading {
                    Text("Loading models…")
                } else {
                    Button("Load models", systemImage: "arrow.clockwise") { Task { await actions.loadModels() } }
                }
            } label: {
                Text("Model")
                Text(actions.configuration.error.map { "Couldn’t load models. \($0)" } ?? actions.modelName)
            }
        } else {
            Picker(selection: selection) {
                ForEach(actions.modelGroups, id: \.provider) { group in
                    Section(group.provider) {
                        ForEach(group.models, id: \.id) { model in
                            Text(model.name).tag(model.id)
                        }
                    }
                }
            } label: {
                Text("Model")
                Text(actions.modelLockedReason ?? actions.modelName)
            }
            .pickerStyle(.menu)
            .disabled(actions.modelLockedReason != nil)
        }
    }

    // MARK: Thinking

    private var thinkingPicker: some View {
        Picker(selection: Binding(get: { actions.thinking }, set: { actions.changeThinking($0) })) {
            ForEach(actions.thinkingLevels, id: \.self) { level in
                Text(SessionContext.thinkingName(level)).tag(level)
            }
        } label: {
            Text("Thinking")
            Text(actions.thinkingLockedReason ?? SessionContext.thinkingName(actions.thinking))
        }
        .pickerStyle(.menu)
        .disabled(actions.thinkingLockedReason != nil)
    }
}

/// How full a session's context is, with the token counts written short.
struct SessionContext: Equatable {
    let tokens: Int
    let window: Int

    init?(_ session: SessionExcerpt) {
        guard let tokens = session.contextTokens, let window = session.contextWindow, window > 0 else { return nil }
        self.tokens = tokens
        self.window = window
    }

    var percent: Int { min(100, Int((Double(tokens) / Double(window) * 100).rounded())) }
    var fraction: Double { min(1, max(0, Double(tokens) / Double(window))) }
    /// `82k / 200k`.
    var tokensLabel: String { "\(Self.short(tokens)) / \(Self.short(window))" }

    /// A count to three significant digits: `950`, `48.2k`, `200k`, `1.24M`.
    static func short(_ value: Int) -> String {
        let (scaled, suffix): (Double, String) = switch value {
        case ..<1_000: (Double(value), "")
        case ..<999_500: (Double(value) / 1_000, "k")
        default: (Double(value) / 1_000_000, "M")
        }
        guard !suffix.isEmpty else { return "\(value)" }
        let digits = scaled >= 100 ? 0 : scaled >= 10 ? 1 : 2
        var text = String(format: "%.\(digits)f", locale: Locale(identifier: "en_US_POSIX"), scaled)
        if text.contains(".") {
            while text.hasSuffix("0") { text.removeLast() }
            if text.hasSuffix(".") { text.removeLast() }
        }
        return text + suffix
    }

    /// A thinking level as the menu writes it: `xhigh` reads `Extra high`.
    static func thinkingName(_ level: String) -> String {
        switch level {
        case "off": "Off"
        case "minimal": "Minimal"
        case "low": "Low"
        case "medium": "Medium"
        case "high": "High"
        case "xhigh": "Extra high"
        case "max": "Max"
        default: level
        }
    }
}

// MARK: Rename

/// Renames the session. The field starts with the current title.
struct RenameSessionSheet: View {
    let actions: SessionActions
    let renamed: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Session name", text: $name)
                        .focused($focused)
                        .submitLabel(.done)
                        .onSubmit(save)
                        .disabled(actions.renaming)
                        .accessibilityIdentifier("rename-field")
                } footer: {
                    if let error = actions.renameError {
                        Text(error).foregroundStyle(Theme.danger)
                    }
                }
            }
            .navigationTitle("Rename session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }.disabled(actions.renaming)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if actions.renaming {
                        ProgressView()
                    } else {
                        Button("Save", role: .confirm, action: save)
                            .disabled(SessionName.normalized(name) == nil || name == actions.title)
                    }
                }
            }
        }
        .presentationDetents([.height(220)])
        .interactiveDismissDisabled(actions.renaming)
        .onAppear {
            actions.clearRenameError()
            name = actions.title
            focused = true
        }
    }

    private func save() {
        guard SessionName.normalized(name) != nil, !actions.renaming else { return }
        Task {
            if await actions.rename(to: name) {
                renamed()
                dismiss()
            }
        }
    }
}

// MARK: Fork

/// Forks the session from its current history, with an optional name and
/// first message. The fork opens when it's made.
struct ForkSessionSheet: View {
    let actions: SessionActions
    let open: (SessionIdentity) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var message = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name (optional)", text: $name)
                        .accessibilityIdentifier("fork-name")
                    TextField("First message (optional)", text: $message, axis: .vertical)
                        .lineLimit(3...8)
                        .accessibilityIdentifier("fork-message")
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Continues from this session’s current history in a separate session. A first message starts the fork’s first turn.")
                        if let error = actions.fork.error {
                            Text(error).foregroundStyle(Theme.danger)
                        }
                    }
                }
                .disabled(actions.fork.pending)
            }
            .navigationTitle("Fork session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }.disabled(actions.fork.pending)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if actions.fork.pending {
                        ProgressView()
                    } else {
                        Button(actions.fork.error == nil ? "Fork" : "Retry", role: .confirm, action: submit)
                            .disabled(actions.forkUnavailableReason != nil)
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .interactiveDismissDisabled(actions.fork.pending)
    }

    private func submit() {
        Task {
            guard let child = await actions.fork(name: name, message: message) else { return }
            dismiss()
            open(child)
        }
    }
}

// MARK: Details

/// A read-only look at the session: how full its context is, and what it has
/// used and cost so far.
struct SessionDetailsSheet: View {
    let session: SessionExcerpt
    let server: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(alignment: .center, spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(session.title)
                        .font(.system(size: 20, weight: .bold))
                        .foregroundStyle(Theme.text)
                        .lineLimit(2)
                    Text("\(server) · \(SessionScreen.shortDirectory(session.cwd ?? ""))")
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
                .padding(.leading, 4)
                .frame(maxWidth: .infinity, alignment: .leading)
                Button { dismiss() } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 32, height: 32)
                        .background(Theme.surface, in: Circle())
                        .overlay(Circle().strokeBorder(Theme.border, lineWidth: 0.5))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Close")
            }
            group("Context") { context }
            group("Usage") { usage }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .padding(.top, 24)
        .background(Theme.raised)
        .presentationDetents([.medium])
        .presentationBackground(Theme.raised)
        .accessibilityIdentifier("session-details")
    }

    private func group(_ title: String, @ViewBuilder content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title.uppercased())
                .font(.system(size: 13))
                .foregroundStyle(Theme.muted)
                .padding(.leading, 16)
                .accessibilityAddTraits(.isHeader)
            content()
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cardRadius, style: .continuous))
        }
    }

    @ViewBuilder
    private var context: some View {
        if let context = SessionContext(session) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(alignment: .firstTextBaseline) {
                    Text("\(context.percent)% used")
                        .font(.system(size: 17))
                        .foregroundStyle(Theme.text)
                    Spacer(minLength: 8)
                    Text(context.tokensLabel)
                        .font(.system(size: 13, design: .monospaced))
                        .foregroundStyle(Theme.muted)
                }
                Capsule().fill(Theme.hover)
                    .frame(height: 6)
                    .overlay(alignment: .leading) {
                        GeometryReader { proxy in
                            Capsule().fill(Theme.text)
                                .frame(width: (proxy.size.width * context.fraction).rounded())
                        }
                    }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .accessibilityElement(children: .combine)
        } else {
            unavailable("Context size unknown")
        }
    }

    @ViewBuilder
    private var usage: some View {
        if let usage = session.usage {
            HStack(spacing: 12) {
                figure("Input", SessionContext.short(usage.input))
                figure("Output", SessionContext.short(usage.output))
                figure("Cost", usage.formattedCost)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
        } else {
            unavailable("Usage unavailable")
        }
    }

    private func figure(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.system(size: 13))
                .foregroundStyle(Theme.muted)
            Text(value)
                .font(.system(size: 15, design: .monospaced))
                .foregroundStyle(Theme.text)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private func unavailable(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 15))
            .foregroundStyle(Theme.muted)
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
    }
}
