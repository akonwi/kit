import KitClient
import KitDictation
import SwiftUI

/// The bottom of a session: the status line, then either the request the
/// session waits on, or the queued follow-ups and the glass capsule with its
/// one button.
struct ComposerBar: View {
    @Bindable var composer: SessionComposer
    let actions: SessionActions
    let interactions: SessionInteractions
    let feed: SessionFeed
    /// Shows a comment in Changes or its file when its chip is tapped.
    var revealComment: ((FileAnnotation) -> Void)? = nil
    /// Which comments a chip tap can show from here.
    var revealable: (FileAnnotation) -> Bool = { $0.isDiff }
    /// Opens the session menu's actions from the command search. Without
    /// it, the composer has no command button.
    var commands: ComposerCommands? = nil
    let retryConnection: () -> Void
    @FocusState private var focused: Bool
    /// The command search, while it's open. The composer keeps it, so the
    /// session screen can close it from a tap outside.
    private var search: CommandSearchState? {
        get { composer.commandSearch }
        nonmutating set { composer.commandSearch = newValue }
    }
    @State private var recents: [String] = []
    @State private var dictation = Dictation(engine: DictationSupport.engine)
    @State private var press = DictationPress()
    /// Dictated words are on their way. The strip stays until the server
    /// answers, so the words don't show in the field just before they go.
    @State private var sendingDictation = false

    var body: some View {
        VStack(spacing: 8) {
            statusLine
            if let flow = interactions.docked, interactions.collapsed {
                CollapsedDock(flow: flow) { interactions.collapsed = false }
                    .transition(.opacity)
            } else if let flow = interactions.docked {
                InteractionDock(flow: flow, interactions: interactions)
                    .id(flow.id)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            } else {
                // The capsule stays one view, so its field keeps the keyboard
                // as the search opens and closes.
                if let commands, search != nil {
                    CommandPanel(search: searchBinding, commands: catalog(commands), recents: recents, actions: actions,
                                 pick: pick, choose: choose)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                        .accessibilityAction(.escape) { closeSearch() }
                } else {
                    if let command = composer.command {
                        CommandHint(command: command)
                    }
                    if let queue = composer.queue {
                        QueuedTray(queue: queue, error: composer.operations.queueError,
                                   enabled: composer.queueEnabled, running: composer.running,
                                   change: composer.changeQueue)
                    }
                    CommentChips(composer: composer, reveal: revealComment, revealable: revealable)
                    AttachmentStrip(composer: composer)
                }
                // The list gives way first, so the field stays above the keyboard.
                capsule
                    .layoutPriority(1)
            }
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 4)
        .animation(.snappy(duration: 0.2), value: composer.queue?.count)
        .animation(.snappy(duration: 0.2), value: composer.uploads.items.map(\.id))
        .animation(.snappy(duration: 0.2), value: composer.annotations.records.map(\.id))
        .animation(.snappy(duration: 0.25), value: interactions.docked?.id)
        .animation(.snappy(duration: 0.25), value: interactions.collapsed)
        .animation(.snappy(duration: 0.2), value: search == nil)
        .animation(.snappy(duration: 0.2), value: composer.command)
    }

    // MARK: Status line

    /// One line above the capsule: the reconnect line, then a refusal, then
    /// what the session is doing. The dock says what it waits on.
    @ViewBuilder
    private var statusLine: some View {
        Group {
            if case .reconnecting = feed.connection {
                ReconnectLine(server: feed.identity.server, retry: retryConnection)
            } else if dictation.phase == .preparing(installing: true) {
                DictationReadyLine()
            } else if let problem = dictation.problem {
                DictationProblemLine(problem: problem, dismiss: dictation.dismissProblem)
            } else if let notice = composer.notice {
                NoticeLine(text: notice)
            } else if let notice = composer.commentNotice {
                NoticeLine(text: notice, dismiss: composer.annotations.acknowledgeUncertainty)
            } else if let notice = actions.notice {
                NoticeLine(text: notice.text, tone: notice.tone, dismiss: actions.dismissNotice)
            } else if interactions.docked != nil {
                EmptyView()
            } else if actions.compacting {
                ProgressLine(text: "Compacting context…")
            } else if let plugin = actions.runningPlugin {
                ProgressLine(text: "Running \(plugin)…")
            } else if actions.reload.pending {
                ProgressLine(text: "Reloading context…")
            } else if let session = feed.session, case .live = feed.connection, let activity = ActivityLine.Content(session) {
                ActivityLine(content: activity)
            }
        }
        .padding(.horizontal, 8)
    }

    // MARK: Capsule

    /// The attach button, the field, and the button at the end. While
    /// dictating, a recording strip takes the attach button's and field's
    /// place, and the mic stays where it was pressed.
    private var capsule: some View {
        let listening = dictation.phase == .recording || dictation.phase == .transcribing || sendingDictation
        let mic = dictation.isActive || composer.canDictate
        let micOnly = dictation.isActive || composer.dictatesInSendSlot
        return HStack(alignment: .bottom, spacing: 4) {
            if listening {
                DictationStrip(dictation: dictation, hint: hint, sending: sendingDictation)
            } else {
                if dictation.isPreparing {
                    Button { dictation.cancel() } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 16, weight: .medium))
                            .foregroundStyle(Theme.text)
                            .frame(width: 40, height: 40)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Cancel dictation")
                    .accessibilityIdentifier("dictation-cancel")
                } else if search != nil {
                    CommandButton(active: true, label: "Close commands") { closeSearch() }
                } else if let command = composer.command {
                    CommandButton(active: true, label: "Change command") { openSearch() }
                        .disabled(!composer.editable)
                    PickedCommandChip(name: command.name) { composer.command = nil }
                        .disabled(!composer.editable)
                } else {
                    AttachMenu(composer: composer)
                    if commands != nil {
                        CommandButton(active: false, label: "Commands") { openSearch() }
                            .disabled(!composer.editable)
                    }
                }
                TextField(placeholder, text: fieldText, axis: .vertical)
                    .font(.system(size: 17))
                    .foregroundStyle(composer.editable ? Theme.text : Theme.muted)
                    .lineLimit(1...6)
                    .focused($focused)
                    .disabled(!composer.editable || dictation.isActive)
                    .padding(.vertical, 9)
                    .accessibilityIdentifier("composer")
                    .contentShape(Rectangle())
                    .onTapGesture { focused = true }
            }
            if search != nil {
                Button { closeSearch() } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 40, height: 40)
                        .background(Theme.hover, in: Circle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Close commands")
                .accessibilityIdentifier("command-close")
            } else {
                if mic {
                    DictationButton(dictation: dictation, look: micOnly ? .filled : .plain, press: $press,
                                    send: sendDictation)
                }
                if !micOnly {
                    ComposerButton(action: composer.action, send: composer.send, stop: composer.stop)
                }
            }
        }
        .padding(.leading, 8)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .frame(minHeight: 56)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 28, style: .continuous))
        .dictationLifecycle(dictation)
        .onChange(of: dictation.isActive) { _, active in
            if active { focused = false }
        }
        // Sent or refused: a refused message's words are in the field to fix.
        // Commands wait while a send is unanswered.
        .onChange(of: composer.editable) { _, editable in
            if editable { sendingDictation = false } else { search = nil }
        }
        // A session with nothing in it yet, such as one just started, waits for its first message.
        .onChange(of: feed.session?.messages.isEmpty, initial: true) { _, empty in
            if empty == true, composer.draft.isEmpty, composer.editable { focused = true }
        }
    }

    // MARK: Commands

    private var placeholder: String {
        if search != nil { return search?.page == .commands ? "Search commands" : "Search" }
        if let command = composer.command { return CommandHint.placeholder(command) }
        return "Ask Kit…"
    }

    /// The field types the search while it's open, and the draft otherwise.
    private var fieldText: Binding<String> {
        Binding {
            search?.query ?? composer.draft
        } set: { text in
            guard search != nil else {
                // The field writes back unchanged text as it ends editing.
                guard text != composer.draft else { return }
                composer.draft = text
                // Typing hides the last action's notice.
                actions.dismissNotice()
                dictation.dismissProblem()
                return
            }
            // Return picks the first row; the field is multiline.
            if text.contains("\n") {
                search?.query = text.replacingOccurrences(of: "\n", with: "")
                pickFirst()
            } else {
                search?.query = text
            }
        }
    }

    private var searchBinding: Binding<CommandSearchState> {
        Binding { search ?? CommandSearchState() } set: { search = $0 }
    }

    private func catalog(_ commands: ComposerCommands) -> [SessionCommand] {
        let session = composer.latest
        let state = CommandState(
            changes: commands.changes, model: actions.modelName, modelLocked: actions.modelLockedReason,
            thinking: SessionContext.thinkingName(actions.thinking), hasThinking: !actions.thinkingLevels.isEmpty,
            thinkingLocked: actions.thinkingLockedReason, contextPercent: session.flatMap(SessionContext.init)?.percent,
            compactUnavailable: actions.compactionUnavailableReason, live: actions.live,
            forkUnavailable: actions.forkUnavailableReason, reloadUnavailable: actions.reloadUnavailableReason,
            pluginUnavailable: actions.pluginUnavailableReason, scratchpad: !actions.temporary)
        return CommandCatalog.commands(state, prompts: session?.promptCommands ?? [], plugins: session?.pluginCommands ?? [])
    }

    private func openSearch() {
        recents = CommandRecents().ids
        search = CommandSearchState()
        focused = true
    }

    private func closeSearch() {
        search = nil
    }

    private func pickFirst() {
        guard let search, let commands else { return }
        switch search.page {
        case .commands:
            let rows = CommandCatalog.sections(catalog(commands), query: search.query, recents: recents).flatMap(\.rows)
            if let first = rows.first(where: { $0.unavailableReason == nil }) { pick(first) }
        case .models, .thinking:
            break
        }
    }

    /// Runs a built-in or a plugin command without an argument, opens the
    /// model or thinking choices, or puts a command that takes text in a chip.
    private func pick(_ command: SessionCommand) {
        guard command.unavailableReason == nil, let commands else { return }
        switch command.kind {
        case .builtIn(.model):
            search = CommandSearchState(page: .models)
            return
        case .builtIn(.thinking):
            search = CommandSearchState(page: .thinking)
            return
        default: break
        }
        CommandRecents().record(command.id)
        search = nil
        switch command.kind {
        case .builtIn(let builtIn):
            focused = false
            commands.perform(builtIn)
        case .prompt(let prompt):
            composer.command = .prompt(prompt)
        case .plugin(let plugin) where command.takesArgument:
            composer.command = .plugin(plugin)
        case .plugin(let plugin):
            focused = false
            actions.runPlugin(plugin, args: "")
        }
    }

    /// Applies a model or thinking level, unless it locked while the page was open.
    private func choose(_ choice: CommandPanel.Choice) {
        switch choice {
        case .model(let id):
            guard actions.modelLockedReason == nil else { return }
            CommandRecents().record(SessionCommand(kind: .builtIn(.model), title: "").id)
            actions.changeModel(id)
        case .thinking(let level):
            guard actions.thinkingLockedReason == nil else { return }
            CommandRecents().record(SessionCommand(kind: .builtIn(.thinking), title: "").id)
            actions.changeThinking(level)
        }
        search = nil
    }

    private func sendDictation(_ text: String) {
        actions.dismissNotice()
        sendingDictation = true
        composer.sendDictation(text)
        // It didn't go, such as while offline; the words wait in the field.
        if composer.editable { sendingDictation = false }
    }

    /// What the recording strip suggests: ↑ for a tapped recording, sliding
    /// to cancel for a held one.
    private var hint: DictationStrip.Hint {
        if press.mode == .tap { return .send }
        return press.cancelArmed ? .letGoToCancel : .slide
    }
}

/// Send, Stop, or a spinner while the server answers.
private struct ComposerButton: View {
    @Environment(\.colorScheme) private var colorScheme
    let action: SessionComposer.Action
    let send: () -> Void
    let stop: () -> Void

    var body: some View {
        Button {
            switch action {
            case .send: send()
            case .stop: stop()
            case .waiting: break
            }
        } label: {
            ZStack {
                Circle().fill(enabled ? Theme.text : Theme.border)
                switch action {
                case .send:
                    Image(systemName: "arrow.up")
                        .font(.system(size: 16, weight: .bold))
                        .foregroundStyle(enabled ? Theme.surface : disabledGlyph)
                case .stop:
                    RoundedRectangle(cornerRadius: 2)
                        .fill(enabled ? Theme.surface : disabledGlyph)
                        .frame(width: 12, height: 12)
                case .waiting:
                    ProgressView()
                        .controlSize(.small)
                        .tint(Theme.surface)
                }
            }
            .frame(width: 40, height: 40)
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .accessibilityLabel(label)
        .accessibilityIdentifier("composer-action")
    }

    private var enabled: Bool {
        switch action {
        case .send(let enabled), .stop(let enabled): enabled
        case .waiting: true
        }
    }

    /// A grey button's glyph: the surface in light, white at 35% in dark.
    private var disabledGlyph: Color {
        colorScheme == .dark ? .white.opacity(0.35) : Theme.surface
    }

    private var label: String {
        switch action {
        case .send: "Send"
        case .stop: "Stop"
        case .waiting: "Waiting for the server"
        }
    }
}

// MARK: Lines

private struct ReconnectLine: View {
    let server: String
    let retry: () -> Void

    var body: some View {
        HStack(spacing: 8) {
            LaneSpinner(tint: Theme.warning)
            Text("Reconnecting to \(server)…")
                .font(.system(size: 14))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
            Spacer(minLength: 0)
            Button("Retry now", action: retry)
                .font(.system(size: 14))
                .foregroundStyle(Theme.accent)
        }
        .frame(height: 30)
    }
}

/// Why the last send, stop, or queue change didn't happen.
struct NoticeLine: View {
    let text: String
    var tone = SessionActions.Notice.Tone.danger
    /// Shows a close button when set.
    var dismiss: (() -> Void)?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: tone == .danger ? "exclamationmark.circle.fill" : "info.circle.fill")
                .font(.system(size: 13))
                .foregroundStyle(color)
            Text(text)
                .font(.system(size: 14))
                .foregroundStyle(color)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let dismiss {
                Button(action: dismiss) {
                    Image(systemName: "xmark")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 24, height: 24)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Dismiss")
            }
        }
        .frame(minHeight: 30)
        .accessibilityElement(children: .combine)
    }

    private var color: Color { tone == .danger ? Theme.danger : Theme.muted }
}

/// Shown while the session's context is compacted or reloaded, or a
/// plugin command runs.
private struct ProgressLine: View {
    let text: String

    var body: some View {
        HStack(spacing: 8) {
            LaneSpinner()
            Text(text)
                .font(.system(size: 14))
                .foregroundStyle(Theme.muted)
            Spacer(minLength: 0)
        }
        .frame(height: 30)
        .accessibilityElement(children: .combine)
    }
}

// MARK: Queue

/// The follow-ups queued behind the running turn. The server acts on the
/// queue as a whole, so the rows are previews and only the header's actions
/// change it. A held queue says why its first message couldn't start.
struct QueuedTray: View {
    let queue: FollowUpState
    var error: String?
    var enabled = true
    var running = false
    let change: (SessionOperations.QueueAction) -> Void

    /// The tray shows this many previews, then counts the rest.
    static let visibleRows = 3

    static func rows(_ queue: FollowUpState) -> (previews: [String], more: Int) {
        let previews = Array(queue.previews.prefix(visibleRows))
        return (previews, max(0, queue.count - previews.count))
    }

    static func title(_ queue: FollowUpState) -> String {
        "\(queue.count) " + (queue.admissionFailure == nil ? "queued" : "held")
    }

    var body: some View {
        let rows = Self.rows(queue)
        VStack(spacing: 0) {
            header
            if let failure = queue.admissionFailure {
                rule
                line("Couldn’t start: " + failure, color: Theme.danger)
            }
            ForEach(Array(rows.previews.enumerated()), id: \.offset) { index, preview in
                rule
                HStack(spacing: 10) {
                    Text("\(index + 1)")
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(Theme.strongBorder)
                        .frame(width: 10, alignment: .leading)
                    Text(preview)
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.text)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(.horizontal, 16)
                .frame(height: 34)
                .accessibilityElement(children: .combine)
            }
            if rows.more > 0 {
                rule
                Text("\(rows.more) more")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
                    .padding(.leading, 36)
                    .frame(maxWidth: .infinity, minHeight: 34, alignment: .leading)
            }
            if let error {
                rule
                line(error, color: Theme.danger)
            }
        }
        .padding(.vertical, 4)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("queued-tray")
    }

    private var header: some View {
        HStack(spacing: 12) {
            Text(Self.title(queue))
                .font(.system(size: 14))
                .foregroundStyle(Theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
            Group {
                Button("Edit") { change(.edit) }
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.accent)
                Button("Clear") { change(.remove) }
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.accent)
            }
            .disabled(!enabled)
            .opacity(enabled ? 1 : 0.5)
            Button { change(.promote) } label: {
                Text("Send now")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(sendNowEnabled ? Theme.surface : Theme.surface.opacity(0.9))
                    .padding(.horizontal, 12)
                    .frame(height: 30)
                    .background(sendNowEnabled ? Theme.text : Theme.border, in: Capsule())
            }
            .buttonStyle(.plain)
            .disabled(!sendNowEnabled)
        }
        .padding(.leading, 16)
        .padding(.trailing, 8)
        .frame(height: 40)
    }

    /// Send now steers the running turn, so it needs one.
    private var sendNowEnabled: Bool { enabled && running && queue.admissionFailure == nil }

    private var rule: some View {
        Rectangle().fill(Theme.border).frame(height: 0.5)
    }

    private func line(_ text: String, color: Color) -> some View {
        Text(text)
            .font(.system(size: 14))
            .foregroundStyle(color)
            .lineLimit(2)
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, minHeight: 34, alignment: .leading)
    }
}
