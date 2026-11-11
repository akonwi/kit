import AppKit
import AVFAudio
import KitDictation
import SwiftUI

// The pieces a field dictates with on macOS 26 and later. A click on the
// mic records; while it does, a strip with a timer and level bars stands in
// the field's place. ✓ or Return stops it and puts the words in the field
// to read and fix; ✕ or Esc throws it away. Nothing is sent by dictating.
// One field dictates at a time: starting another throws this one away.

extension EnvironmentValues {
    /// A dictation fields use instead of their own. Tests and previews set
    /// one to show a field mid-recording.
    @Entry var dictationSlot: DictationSlot? = nil
}

enum DictationSupport {
    /// Whether this Mac can dictate: SpeechAnalyzer needs macOS 26.
    static var available: Bool {
        if #available(macOS 26, *) { true } else { false }
    }

    /// The engine the app dictates with. In debug builds `KIT_DICTATION_TRANSCRIPT`
    /// in the environment swaps in a scripted one that "hears" that text, so
    /// a run can dictate without a microphone; `KIT_DICTATION_ACCESS=denied`
    /// makes it refuse access.
    @available(macOS 26, *)
    static func engine() -> any DictationEngine {
        #if DEBUG
        let environment = ProcessInfo.processInfo.environment
        if let transcript = environment["KIT_DICTATION_TRANSCRIPT"] {
            let access: DictationAccess = environment["KIT_DICTATION_ACCESS"] == "denied" ? .denied : .granted
            let delay = Double(environment["KIT_DICTATION_DELAY"] ?? "").map { Duration.seconds($0) } ?? .milliseconds(300)
            return ScriptedDictationEngine(transcript: transcript, access: access, delay: delay)
        }
        #endif
        return SpeechDictationEngine()
    }

    static func message(_ problem: DictationProblem) -> String {
        switch problem {
        case .notAllowed: "Kit can’t use the microphone or speech recognition."
        case .nothingHeard: "Didn’t catch that."
        case .unsupportedLanguage: "Dictation isn’t available in your language."
        case .unavailable: "Kit couldn’t use the microphone."
        case .failed: "Couldn’t dictate that. Try again."
        }
    }

    /// The Privacy & Security pane for whichever access was refused.
    static var settingsURL: URL? {
        let pane = AVAudioApplication.shared.recordPermission == .denied ? "Privacy_Microphone" : "Privacy_SpeechRecognition"
        return URL(string: "x-apple.systempreferences:com.apple.preference.security?" + pane)
    }
}

/// Where the composer's mic sits.
enum ComposerMic: Equatable {
    case hidden
    /// Plain, beside Stop or Send.
    case beside
    /// Filled, in Send's square: with nothing to send or stop, and
    /// throughout a recording.
    case sendSlot

    /// What the composer holds, as far as the mic cares.
    struct Context: Equatable {
        var dictating = false
        var draft = ""
        /// Attachments, review notes, or comments, which Send can send without words.
        var hasOtherContent = false
        /// A turn, a replay, or a shell command that Stop would stop.
        var stoppable = false
        var shell = false
        var unavailable = false
    }

    /// The mic shows while dictating, and otherwise while the draft is empty
    /// and could take words. It takes Send's square unless Send or Stop has
    /// something to act on.
    static func placement(_ context: Context) -> ComposerMic {
        if context.dictating { return .sendSlot }
        guard !context.unavailable, !context.shell,
              context.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return .hidden }
        return context.hasOtherContent || context.stoppable ? .beside : .sendSlot
    }
}

/// A field's dictation, made the first time the field asks for it. Fields
/// keep one in `@State`; `Dictation` itself needs macOS 26, which the app
/// doesn't.
@MainActor final class DictationSlot {
    private var stored: AnyObject?
    private let engine: (any DictationEngine)?

    /// Dictates with `engine`, or ``DictationSupport/engine()`` when nil.
    init(engine: (any DictationEngine)? = nil) {
        self.engine = engine
    }

    @available(macOS 26, *)
    var dictation: Dictation {
        if let dictation = stored as? Dictation { return dictation }
        let dictation = Dictation(engine: engine ?? DictationSupport.engine())
        stored = dictation
        return dictation
    }

    /// Throws away a recording in progress and what the last one said, as
    /// the field moves on to another session or question.
    func reset() {
        guard #available(macOS 26, *), let dictation = stored as? Dictation else { return }
        dictation.cancel()
        dictation.dismissProblem()
    }

    /// Whether a recording is starting, running, or finishing. A field's own
    /// Return and Esc wait meanwhile, so they can't act behind the strip.
    var isActive: Bool {
        if #available(macOS 26, *), let dictation = stored as? Dictation { return dictation.isActive }
        return false
    }
}

@available(macOS 26, *)
extension Dictation {
    /// Recording, or finishing the last phrase: the strip shows in the field's place.
    var listening: Bool { phase == .recording || phase == .transcribing }

    /// Whether ``DictationNotice`` has something to say.
    var hasNotice: Bool { phase == .preparing(installing: true) || problem != nil }

    /// Stops recording and hands over the words, if any were heard.
    func finish(into fill: @escaping @MainActor (String) -> Void) {
        Task { if let text = await finish() { fill(text) } }
    }

    /// Starts recording in this field and throws away a recording another
    /// field has going, so only one field dictates at a time.
    @discardableResult
    func startAlone() async -> Bool {
        if let other = DictatingField.current, other !== self { other.cancel() }
        DictatingField.current = self
        return await start()
    }
}

/// The field that last started dictating.
@available(macOS 26, *)
@MainActor private enum DictatingField {
    static weak var current: Dictation?
}

/// The recording strip: ✕, a red dot, the timer, level bars, and a hint.
/// It takes focus so Return stops the recording and Esc throws it away.
@available(macOS 26, *)
struct DictationStrip: View {
    @Environment(\.mica) private var theme
    let dictation: Dictation
    /// Stops recording and puts the words in the field.
    let done: () -> Void
    /// Called after ✕ or Esc throws the recording away, so the field can take focus back.
    var cancelled: () -> Void = {}
    var size: CGFloat = 14
    var height: CGFloat = 20
    @FocusState private var focused: Bool

    var body: some View {
        let transcribing = dictation.phase == .transcribing
        HStack(spacing: 8) {
            Button(action: cancel) {
                IconView(.x, size: 11).frame(width: 11, height: height).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .foregroundStyle(theme.muted.opacity(transcribing ? 0.5 : 1))
            .disabled(transcribing)
            .help("Cancel dictation")
            .accessibilityLabel("Cancel dictation")
            if transcribing {
                Text("Transcribing…").font(.kit(size: size)).foregroundStyle(theme.muted).lineLimit(1)
            } else {
                Circle().fill(theme.danger).frame(width: 6, height: 6).accessibilityHidden(true)
                if let startedAt = dictation.startedAt {
                    TimelineView(.periodic(from: startedAt, by: 1)) { context in
                        Text(Duration.seconds(max(0, context.date.timeIntervalSince(startedAt))),
                             format: .time(pattern: .minuteSecond))
                            .font(.kit(size: size)).monospacedDigit()
                            .foregroundStyle(theme.text)
                    }
                    .accessibilityLabel("Recording")
                }
                DictationLevel(level: dictation.level)
            }
            Spacer(minLength: 8)
            if !transcribing {
                Text("↩ done · esc cancel").font(.kit(size: 11)).foregroundStyle(theme.muted).lineLimit(1)
                    .layoutPriority(-1)
            }
        }
        .frame(maxWidth: .infinity, minHeight: height, maxHeight: height, alignment: .leading)
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .onKeyPress(.return) {
            done()
            return .handled
        }
        .onExitCommand(perform: cancel)
        .task {
            await Task.yield()
            focused = true
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Dictation")
    }

    private func cancel() {
        dictation.cancel()
        cancelled()
    }
}

/// Five bars that follow the microphone's level.
@available(macOS 26, *)
struct DictationLevel: View {
    @Environment(\.mica) private var theme
    let level: Double
    private static let weights: [Double] = [0.3, 0.7, 1, 0.6, 0.3]

    var body: some View {
        HStack(spacing: 2.5) {
            ForEach(Self.weights.indices, id: \.self) { index in
                Capsule().fill(theme.muted)
                    .frame(width: 2.5, height: 4 + 10 * level * Self.weights[index])
            }
        }
        .frame(height: 14)
        .animation(.linear(duration: 0.1), value: level)
        .accessibilityHidden(true)
    }
}

/// The square at the end of a field while dictating, or the composer's mic
/// in Send's place: a mic to start, a spinner while getting ready or
/// transcribing, and ✓ while recording.
@available(macOS 26, *)
struct DictationSquare: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let dictation: Dictation
    var size: CGFloat = 26
    let done: () -> Void

    var body: some View {
        Button {
            switch dictation.phase {
            case .idle: Task { await dictation.startAlone() }
            case .recording: done()
            case .preparing, .transcribing: break
            }
        } label: {
            ZStack {
                switch dictation.phase {
                case .idle: IconView(.microphoneFill, size: size / 2)
                case .recording: IconView(.check, size: size * 0.46)
                case .preparing, .transcribing: KitSpinner()
                }
            }
            .foregroundStyle(theme.surface)
            .frame(width: size, height: size)
            .background(theme.text.opacity(enabled ? 1 : 0.4), in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(dictation.phase == .transcribing)
        .accessibilityLabel(label)
        .help(label)
    }

    private var label: String {
        switch dictation.phase {
        case .idle: "Dictate"
        case .preparing: "Getting dictation ready"
        case .recording: "Done dictating"
        case .transcribing: "Transcribing"
        }
    }
}

/// A plain mic beside Stop or Send, or at a field's end. It shows a
/// spinner while getting ready.
@available(macOS 26, *)
struct DictationMic: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let dictation: Dictation
    var size: CGFloat = 26
    var iconSize: CGFloat = 14
    @State private var hovering = false

    var body: some View {
        Button {
            Task { await dictation.startAlone() }
        } label: {
            ZStack {
                if dictation.isPreparing {
                    KitSpinner()
                } else {
                    IconView(.microphone, size: iconSize)
                }
            }
            .foregroundStyle(enabled && hovering ? theme.text : theme.muted)
            .frame(width: size, height: size)
            .background(hovering && enabled ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(dictation.isActive)
        .onHover { hovering = $0 }
        .accessibilityLabel("Dictate")
        .help("Dictate")
    }
}

/// Why dictation gave nothing, or that the first recording is waiting for
/// its speech model. Refused access offers Settings; the rest can be
/// dismissed, and clear when the field is typed in or dictated into again.
@available(macOS 26, *)
struct DictationNotice: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    let dictation: Dictation

    var body: some View {
        if dictation.phase == .preparing(installing: true) {
            HStack(spacing: 8) {
                KitSpinner().foregroundStyle(theme.muted)
                Text("Getting dictation ready…").foregroundStyle(theme.muted)
                Spacer(minLength: 8)
                Button("Cancel") { dictation.cancel() }.buttonStyle(.plain).foregroundStyle(theme.accent)
            }
            .font(.kit(size: 12))
        } else if let problem = dictation.problem {
            HStack(alignment: .top, spacing: 8) {
                Text(DictationSupport.message(problem))
                    .foregroundStyle(problem == .nothingHeard ? theme.muted : theme.danger)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if problem == .notAllowed {
                    Button("Open Settings") {
                        if let url = DictationSupport.settingsURL { openURL(url) }
                    }.buttonStyle(.plain).foregroundStyle(theme.accent)
                } else {
                    Button { dictation.dismissProblem() } label: {
                        IconView(.x, size: 10).frame(width: 16, height: 15).contentShape(Rectangle())
                    }
                    .buttonStyle(.plain).foregroundStyle(theme.muted)
                    .accessibilityLabel("Dismiss")
                }
            }
            .font(.kit(size: 12))
            .accessibilityElement(children: .contain)
        }
    }
}
