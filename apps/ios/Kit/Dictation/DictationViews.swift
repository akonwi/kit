import KitDictation
import SwiftUI
import UIKit

// The pieces a field dictates with. The composer's mic takes a tap or a
// hold, and either sends; the dock's and a comment's take a tap and fill
// the field. While recording, a strip
// with a timer and level bars stands in the field's place, and the words
// arrive when it stops.

/// The composer's mic. Empty and idle, it's filled and sits in Send's slot;
/// beside Stop or Send it's plain. Through a recording it's the strip's
/// button: ↑ for a tapped one, an accent mic while held, and ✕ in the
/// danger color once slid past cancel. It keeps its size throughout; the
/// color says what letting go will do.
struct DictationButton: View {
    enum Look { case filled, plain }

    let dictation: Dictation
    let look: Look
    @Binding var press: DictationPress
    /// Sends the words.
    let send: (String) -> Void
    /// Resets even when the system takes the press away and onEnded never runs.
    @GestureState private var pressing = false

    var body: some View {
        let state = DictationButtonState(phase: dictation.phase, mode: press.mode, cancelArmed: press.cancelArmed)
        ZStack {
            Circle()
                .fill(circleColor(state))
            glyph(state)
        }
        .frame(width: 40, height: 40)
        .animation(.snappy(duration: 0.2), value: state)
        .contentShape(Circle())
        .gesture(gesture)
        .onChange(of: pressing) { _, pressing in
            // After onEnded when the press ended normally; otherwise it was taken away.
            guard !pressing else { return }
            Task { @MainActor in apply(press.interrupted()) }
        }
        .onChange(of: dictation.phase == .idle) { _, idle in
            if idle { press.reset() }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: state == .holding) { _, holding in holding }
        .sensoryFeedback(.selection, trigger: press.cancelArmed)
        .accessibilityElement()
        .accessibilityAddTraits(.isButton)
        .accessibilityLabel(state.label)
        .accessibilityIdentifier("dictate")
        .accessibilityAction { accessibilityTap() }
    }

    private var gesture: some Gesture {
        DragGesture(minimumDistance: 0)
            .updating($pressing) { _, pressing, _ in pressing = true }
            .onChanged { value in
                apply(press.began(at: .now, idle: dictation.phase == .idle))
                press.moved(width: value.translation.width)
            }
            .onEnded { _ in
                apply(press.ended(at: .now, recording: dictation.phase == .recording))
            }
    }

    private func apply(_ effect: DictationPress.Effect) {
        switch effect {
        case .none: break
        case .start:
            Task { if !(await dictation.start()) { press.reset() } }
        case .send:
            // Let go before the microphone was ready: nothing to send.
            if dictation.isPreparing {
                dictation.cancel()
                return
            }
            Task { if let text = await dictation.finish() { send(text) } }
        case .cancel:
            dictation.cancel()
        }
    }

    /// VoiceOver: one activation starts a tapped recording, the next sends it.
    private func accessibilityTap() {
        switch dictation.phase {
        case .idle:
            apply(press.began(at: .now, idle: true))
            apply(press.ended(at: .now, recording: false))
        case .recording:
            Task { if let text = await dictation.finish() { send(text) } }
        case .preparing, .transcribing:
            break
        }
    }

    private func circleColor(_ state: DictationButtonState) -> Color {
        switch state {
        case .idle: look == .filled ? Theme.text : .clear
        case .holding: Theme.accent
        case .cancelArmed: Theme.danger
        case .busy, .tapped: Theme.text
        }
    }

    @ViewBuilder
    private func glyph(_ state: DictationButtonState) -> some View {
        switch state {
        case .idle:
            Image(systemName: "mic.fill")
                .font(.system(size: look == .filled ? 16 : 18, weight: look == .filled ? .bold : .regular))
                .foregroundStyle(look == .filled ? Theme.surface : Theme.text)
        case .busy:
            ProgressView().controlSize(.small).tint(Theme.surface)
        case .tapped:
            Image(systemName: "arrow.up")
                .font(.system(size: 16, weight: .bold))
                .foregroundStyle(Theme.surface)
        case .holding:
            Image(systemName: "mic.fill")
                .font(.system(size: 15, weight: .bold))
                .foregroundStyle(.white)
        case .cancelArmed:
            Image(systemName: "xmark")
                .font(.system(size: 14, weight: .bold))
                .foregroundStyle(Theme.surface)
        }
    }
}

/// What the composer's mic shows.
enum DictationButtonState: Equatable {
    case idle
    /// Getting ready or transcribing.
    case busy
    /// A tapped recording: ↑ sends it.
    case tapped
    /// Held down while recording: letting go sends.
    case holding
    /// Held and slid past cancel: letting go throws it away.
    case cancelArmed

    init(phase: Dictation.Phase, mode: DictationPress.Mode, cancelArmed: Bool) {
        switch phase {
        case .idle: self = .idle
        case .preparing, .transcribing: self = .busy
        case .recording:
            switch mode {
            case .tap: self = .tapped
            case .hold: self = cancelArmed ? .cancelArmed : .holding
            case .none: self = .busy
            }
        }
    }

    var label: String {
        switch self {
        case .idle: "Dictate"
        case .busy: "Dictation"
        case .tapped: "Send dictation"
        case .holding: "Let go to send"
        case .cancelArmed: "Let go to cancel"
        }
    }
}

/// The recording strip in a field's place: ✕, a red dot, the time, the
/// microphone's level, and a hint. While transcribing, and while the words
/// are being sent, it says so.
struct DictationStrip: View {
    let dictation: Dictation
    let hint: Hint
    /// The words are on their way to the server.
    var sending = false
    /// The strip's text size: 17 in the composer, smaller in the dock and comments.
    var size: CGFloat = 17

    enum Hint: Equatable {
        /// A tapped recording in the dock or a comment.
        case done
        /// A tapped recording in the composer.
        case send
        case slide
        case letGoToCancel

        var text: String {
            switch self {
            case .done: "Tap ✓ when done"
            case .send: "Tap ↑ to send"
            case .slide: "‹ Slide to cancel"
            case .letGoToCancel: "Let go to cancel"
            }
        }
    }

    var body: some View {
        let busy = dictation.phase == .transcribing || sending
        HStack(spacing: 8) {
            Button { dictation.cancel() } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(busy ? Theme.border : Theme.text)
                    .frame(width: 32, height: 40)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(busy)
            .accessibilityLabel("Cancel dictation")
            .accessibilityIdentifier("dictation-cancel")
            if busy {
                Text(sending ? "Sending…" : "Transcribing…")
                    .font(.system(size: size))
                    .foregroundStyle(Theme.muted)
                    .accessibilityIdentifier(sending ? "dictation-sending" : "dictation-transcribing")
                Spacer(minLength: 0)
            } else {
                Circle().fill(Theme.danger).frame(width: 8, height: 8).accessibilityHidden(true)
                if let startedAt = dictation.startedAt {
                    TimelineView(.periodic(from: startedAt, by: 1)) { context in
                        Text(Duration.seconds(max(0, context.date.timeIntervalSince(startedAt))),
                             format: .time(pattern: .minuteSecond))
                            .font(.system(size: size))
                            .monospacedDigit()
                            .foregroundStyle(Theme.text)
                    }
                    .accessibilityLabel("Recording")
                    .accessibilityIdentifier("dictation-timer")
                }
                if hint != .letGoToCancel {
                    DictationLevel(level: dictation.level)
                }
                Spacer(minLength: 4)
                Text(hint.text)
                    .font(.system(size: 13, weight: hint == .letGoToCancel ? .semibold : .regular))
                    .foregroundStyle(hint == .letGoToCancel ? Theme.danger : Theme.muted)
                    .lineLimit(1)
                    .padding(.trailing, 6)
            }
        }
        .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("dictation-strip")
    }
}

/// Five bars that follow the microphone's level.
private struct DictationLevel: View {
    let level: Double
    private let weights: [Double] = [0.35, 0.7, 1, 0.6, 0.35]

    var body: some View {
        HStack(spacing: 3) {
            ForEach(weights.indices, id: \.self) { index in
                Capsule()
                    .fill(Theme.muted)
                    .frame(width: 3, height: 5 + 13 * level * weights[index])
            }
        }
        .frame(height: 20)
        .animation(.linear(duration: 0.1), value: level)
        .accessibilityHidden(true)
    }
}

/// A mic inside a field that takes a tap: the dock's text answer and the
/// comment box. While it records, the field shows a strip and a ✓.
struct FieldDictation<Field: View>: View {
    let dictation: Dictation
    let enabled: Bool
    /// Puts the words in the field.
    let fill: (String) -> Void
    @ViewBuilder let field: Field

    var body: some View {
        HStack(alignment: .center, spacing: 6) {
            if dictation.phase == .recording || dictation.phase == .transcribing {
                DictationStrip(dictation: dictation, hint: .done, size: 16)
                done
            } else {
                field
                mic
            }
        }
    }

    private var mic: some View {
        Button {
            Task { _ = await dictation.start() }
        } label: {
            ZStack {
                if dictation.isPreparing {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "mic.fill")
                        .font(.system(size: 17))
                        .foregroundStyle(enabled ? Theme.text : Theme.muted)
                }
            }
            .frame(width: 32, height: 32)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!enabled || dictation.isActive)
        .accessibilityLabel("Dictate")
        .accessibilityIdentifier("dictate-field")
    }

    private var done: some View {
        Button {
            Task { if let text = await dictation.finish() { fill(text) } }
        } label: {
            ZStack {
                Circle().fill(Theme.text)
                if dictation.phase == .transcribing {
                    ProgressView().controlSize(.mini).tint(Theme.surface)
                } else {
                    Image(systemName: "checkmark")
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(Theme.surface)
                }
            }
            .frame(width: 28, height: 28)
            .frame(width: 32, height: 32)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(dictation.phase != .recording)
        .accessibilityLabel("Done dictating")
        .accessibilityIdentifier("dictation-done")
    }
}

/// Why dictation gave nothing, under a field or above the composer.
/// Refused access offers Settings; the rest can be dismissed.
struct DictationProblemLine: View {
    let problem: DictationProblem
    let dismiss: () -> Void
    @Environment(\.openURL) private var openURL

    var body: some View {
        switch problem {
        case .notAllowed:
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: "exclamationmark.circle.fill")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.danger)
                Text(DictationSupport.message(problem))
                    .font(.system(size: 14))
                    .foregroundStyle(Theme.danger)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Button("Settings") {
                    if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                }
                .font(.system(size: 14))
                .foregroundStyle(Theme.accent)
            }
            .frame(minHeight: 30)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("dictation-problem")
        case .nothingHeard:
            HStack(spacing: 8) {
                Text(DictationSupport.message(problem))
                    .font(.system(size: 14))
                    .foregroundStyle(Theme.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                closeButton
            }
            .frame(minHeight: 30)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("dictation-problem")
        default:
            NoticeLine(text: DictationSupport.message(problem), dismiss: dismiss)
                .accessibilityIdentifier("dictation-problem")
        }
    }

    private var closeButton: some View {
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

/// Said above the composer while the first recording in a language waits
/// for its speech model.
struct DictationReadyLine: View {
    var body: some View {
        HStack(spacing: 8) {
            LaneSpinner()
            Text("Getting dictation ready…")
                .font(.system(size: 14))
                .foregroundStyle(Theme.muted)
            Spacer(minLength: 0)
        }
        .frame(height: 30)
        .accessibilityElement(children: .combine)
    }
}

extension View {
    /// Throws a recording away when the field goes or the app leaves the foreground.
    func dictationLifecycle(_ dictation: Dictation) -> some View {
        modifier(DictationLifecycle(dictation: dictation))
    }
}

private struct DictationLifecycle: ViewModifier {
    let dictation: Dictation
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            .onDisappear { dictation.cancel() }
            .onChange(of: scenePhase) { _, phase in
                // Not on inactive: the system's access prompt makes the scene inactive.
                if phase == .background { dictation.cancel() }
            }
    }
}
