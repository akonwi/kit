import SwiftUI

/// What a conversation pane's activity card reports, most urgent first.
enum ActivityLine: Equatable {
    /// The send request failed; the turn didn't start.
    case failedToStart(String)
    /// The stop request failed; the turn is still running.
    case failedToStop(String)
    /// A shell command couldn't start, resolve, or stop. A retryable one
    /// still has an unresolved request to settle.
    case shellFailed(String, retryable: Bool)
    /// The provider failed and the server retries at this ISO 8601 time.
    case retrying(at: String, attempt: Int?)
    case compacting(String)
    /// A plugin command the user ran from the palette, by name.
    case runningCommand(String)
    /// The last non-blank line of the model's streamed thinking, as markdown.
    case thinking(String)
    /// A fixed label: "Working…" or "Thinking…" before a line arrives.
    case working(String)
    case notice(String)

    static func resolve(activity: String?, compaction: String? = nil, retryAt: String? = nil,
                        retryAttempt: Int? = nil, startFailure: String? = nil, stopFailure: String? = nil,
                        shellFailure: String? = nil, shellRetryable: Bool = false,
                        command: String? = nil, notice: String = "") -> ActivityLine? {
        if let stopFailure { return .failedToStop(stopFailure) }
        if let startFailure { return .failedToStart(startFailure) }
        if let shellFailure { return .shellFailed(shellFailure, retryable: shellRetryable) }
        if let retryAt { return .retrying(at: retryAt, attempt: retryAttempt) }
        if let compaction { return .compacting(compaction) }
        if let command { return .runningCommand(command) }
        if let activity, !activity.isEmpty {
            if activity == LiveActivity.compacting { return .compacting(activity) }
            return LiveActivity.isLabel(activity) ? .working(activity) : .thinking(activity)
        }
        if !notice.isEmpty { return .notice(notice) }
        return nil
    }

    static func secondsRemaining(_ timestamp: String, now: Date) -> Int {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let date = formatter.date(from: timestamp) ?? ISO8601DateFormatter().date(from: timestamp)
        return max(0, Int(ceil((date ?? now).timeIntervalSince(now))))
    }
}

/// A conversation pane's activity card above its composer. The slot keeps its
/// height while idle, so the card appearing never shifts the transcript.
struct PendingActivityView: View {
    @Environment(\.mica) private var theme
    let line: ActivityLine?
    /// Retries the failed send, stop, or shell command.
    var retry: () -> Void = {}
    /// Clears a failure that has nothing to retry.
    var dismiss: () -> Void = {}

    init(line: ActivityLine?, retry: @escaping () -> Void = {}, dismiss: @escaping () -> Void = {}) {
        self.line = line
        self.retry = retry
        self.dismiss = dismiss
    }
    init(activity: String?) { line = ActivityLine.resolve(activity: activity) }

    var body: some View {
        Group {
            if let line {
                card(line)
            } else {
                Color.clear.frame(height: Self.height).accessibilityHidden(true)
            }
        }
    }

    private func card(_ line: ActivityLine) -> some View {
        HStack(spacing: 8) {
            content(line)
        }
        .font(.kit(size: 13)).foregroundStyle(theme.muted)
        .padding(.leading, 13).padding(.trailing, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .frame(height: Self.height)
        .background(theme.frame, in: RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private func content(_ line: ActivityLine) -> some View {
        switch line {
        case .failedToStart(let message):
            failure("Couldn’t start the turn: " + message)
        case .failedToStop(let message):
            failure("Couldn’t stop the turn: " + message)
        case let .shellFailed(message, retryable):
            failure("Shell command: " + message, retryable: retryable)
        case let .retrying(at, attempt):
            IconView(.arrowClockwise, size: 12).foregroundStyle(theme.warning)
            TimelineView(.periodic(from: .now, by: 1)) { context in
                Text("Retrying in " + String(ActivityLine.secondsRemaining(at, now: context.date)) + "s").lineLimit(1)
            }
            if let attempt {
                Text("attempt " + String(attempt)).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.strongBorder)
            }
            Spacer(minLength: 0)
        case .compacting(let title):
            spinner
            Text(title).lineLimit(1)
            Spacer(minLength: 0)
        case .runningCommand(let name):
            spinner
            (Text("Running ") + Text("/" + name).font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.text)
                + Text("…")).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: 0)
        case .working(let label):
            spinner
            Text(label).lineLimit(1)
            Spacer(minLength: 0)
        case .thinking(let thought):
            spinner
            Text(ActivityMarkdown.attributed(thought, color: theme.muted, theme: theme))
                .lineLimit(1).truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
        case .notice(let text):
            Text(text).lineLimit(1).truncationMode(.tail).help(text)
            Spacer(minLength: 0)
        }
    }

    @ViewBuilder private func failure(_ message: String, retryable: Bool = true) -> some View {
        IconView(.warningCircle, size: 12).foregroundStyle(theme.danger)
        Text(message).foregroundStyle(theme.danger).lineLimit(1).truncationMode(.tail).help(message)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
        if retryable {
            Button("Retry", action: retry).buttonStyle(SmallButtonStyle(bordered: true))
        } else {
            Button(action: dismiss) {
                IconView(.x, size: 10).foregroundStyle(theme.muted)
                    .frame(width: 18, height: 18).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Dismiss")
        }
    }

    private var spinner: some View { KitSpinner().foregroundStyle(theme.accent) }

    static let height: CGFloat = 30
}

/// Inline markdown for activity text: code spans become small monospace chips.
enum ActivityMarkdown {
    @MainActor static func attributed(_ text: String, color: Color, theme: MicaTheme) -> AttributedString {
        var value = (try? AttributedString(markdown: text, options: .init(
            interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(text)
        value.foregroundColor = color
        for run in value.runs where run.inlinePresentationIntent?.contains(.code) == true {
            value[run.range].font = .kit(size: 11, design: .monospaced)
            value[run.range].foregroundColor = theme.text
            // The card sits on the frame colour, so chips are inset in the surface.
            value[run.range].backgroundColor = theme.surface
        }
        return value
    }
}
