import KitClient
import SwiftUI

/// The bottom of a session: the status line, then either the request the
/// session waits on, or the queued follow-ups and the glass capsule with its
/// one button.
struct ComposerBar: View {
    @Bindable var composer: SessionComposer
    let actions: SessionActions
    let interactions: SessionInteractions
    let feed: SessionFeed
    let retryConnection: () -> Void
    @FocusState private var focused: Bool

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
                if let queue = composer.queue {
                    QueuedTray(queue: queue, error: composer.operations.queueError,
                               enabled: composer.queueEnabled, running: composer.running,
                               change: composer.changeQueue)
                }
                AttachmentStrip(composer: composer)
                capsule
            }
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 4)
        .animation(.snappy(duration: 0.2), value: composer.queue?.count)
        .animation(.snappy(duration: 0.2), value: composer.uploads.items.map(\.id))
        .animation(.snappy(duration: 0.25), value: interactions.docked?.id)
        .animation(.snappy(duration: 0.25), value: interactions.collapsed)
    }

    // MARK: Status line

    /// One line above the capsule: the reconnect line, then a refusal, then
    /// what the session is doing. The dock says what it waits on.
    @ViewBuilder
    private var statusLine: some View {
        Group {
            if case .reconnecting = feed.connection {
                ReconnectLine(server: feed.identity.server, retry: retryConnection)
            } else if let notice = composer.notice {
                NoticeLine(text: notice)
            } else if let notice = actions.notice {
                NoticeLine(text: notice.text, tone: notice.tone, dismiss: actions.dismissNotice)
            } else if interactions.docked != nil {
                EmptyView()
            } else if actions.compacting {
                CompactingLine()
            } else if let session = feed.session, case .live = feed.connection, let activity = ActivityLine.Content(session) {
                ActivityLine(content: activity)
            }
        }
        .padding(.horizontal, 8)
    }

    // MARK: Capsule

    private var capsule: some View {
        HStack(alignment: .bottom, spacing: 4) {
            AttachMenu(composer: composer)
            TextField("Ask Kit…", text: $composer.draft, axis: .vertical)
                .font(.system(size: 17))
                .foregroundStyle(composer.editable ? Theme.text : Theme.muted)
                .lineLimit(1...6)
                .focused($focused)
                .disabled(!composer.editable)
                .padding(.vertical, 9)
                .accessibilityIdentifier("composer")
                .contentShape(Rectangle())
                .onTapGesture { focused = true }
            ComposerButton(action: composer.action, send: composer.send, stop: composer.stop)
        }
        .padding(.leading, 8)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .frame(minHeight: 56)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 28, style: .continuous))
        .onChange(of: composer.draft) { actions.dismissNotice() }
        // A session with nothing in it yet, such as one just started, waits for its first message.
        .onChange(of: feed.session?.messages.isEmpty, initial: true) { _, empty in
            if empty == true, composer.draft.isEmpty, composer.editable { focused = true }
        }
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
private struct NoticeLine: View {
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

/// Shown while the session's context is being compacted.
private struct CompactingLine: View {
    var body: some View {
        HStack(spacing: 8) {
            LaneSpinner()
            Text("Compacting context…")
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
