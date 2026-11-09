import Foundation
import KitDictation

/// The engine the app dictates with. In debug builds `KIT_DICTATION_TRANSCRIPT`
/// in the launch environment swaps in a scripted one that "hears" that text,
/// so the simulator and UI tests can dictate without a microphone;
/// `KIT_DICTATION_ACCESS=denied` makes it refuse access.
enum DictationSupport {
    static let engine: any DictationEngine = {
        #if DEBUG
        let environment = ProcessInfo.processInfo.environment
        if let transcript = environment["KIT_DICTATION_TRANSCRIPT"] {
            let access: DictationAccess = environment["KIT_DICTATION_ACCESS"] == "denied" ? .denied : .granted
            let delay = Double(environment["KIT_DICTATION_DELAY"] ?? "").map { Duration.seconds($0) } ?? .milliseconds(300)
            return ScriptedDictationEngine(transcript: transcript, access: access, delay: delay)
        }
        #endif
        return SpeechDictationEngine()
    }()

    /// What a field says when dictation gave nothing, other than
    /// `notAllowed` and `nothingHeard`, which have their own lines.
    static func message(_ problem: DictationProblem) -> String {
        switch problem {
        case .notAllowed: "Kit can’t use the microphone or speech recognition."
        case .nothingHeard: "Didn’t catch that."
        case .unsupportedLanguage: "Dictation isn’t available in your language."
        case .unavailable: "Kit couldn’t use the microphone."
        case .failed: "Couldn’t dictate that. Try again."
        }
    }
}

/// One press on the composer's mic, as tap or hold. A press starts
/// recording at once. Let go quickly and it keeps recording until ↑ is
/// tapped; hold and let go instead. Either way what was said is sent.
/// Sliding left past `cancelDistance` while holding throws it away.
struct DictationPress: Equatable {
    /// How the recording that's running was started.
    enum Mode: Equatable {
        case none
        /// Held down: letting go sends.
        case hold
        /// Tapped: ↑ sends.
        case tap
    }

    /// What the press asks the dictation to do.
    enum Effect: Equatable {
        case none
        case start
        /// Stop and send the words.
        case send
        case cancel
    }

    static let holdThreshold: TimeInterval = 0.35
    static let cancelDistance: Double = -70

    private(set) var mode = Mode.none
    /// The finger is past the cancel threshold; letting go cancels.
    private(set) var cancelArmed = false
    private var pressedAt: Date?
    /// This press started the recording, rather than being ↑ on a tapped one.
    private var startedRecording = false

    /// The finger went down. `idle` is whether dictation is idle.
    mutating func began(at time: Date, idle: Bool) -> Effect {
        guard pressedAt == nil else { return .none }
        pressedAt = time
        startedRecording = idle
        guard idle else { return .none }
        mode = .hold
        return .start
    }

    /// The finger moved `width` points sideways from where it went down.
    mutating func moved(width: Double) {
        guard pressedAt != nil, startedRecording else { return }
        cancelArmed = width < Self.cancelDistance
    }

    /// The finger lifted. `recording` is whether dictation is recording.
    mutating func ended(at time: Date, recording: Bool) -> Effect {
        guard let pressedAt else { return .none }
        let held = time.timeIntervalSince(pressedAt)
        let armed = cancelArmed
        self.pressedAt = nil
        cancelArmed = false
        guard startedRecording else {
            // ↑ on a tapped recording.
            guard mode == .tap, recording else { return .none }
            mode = .none
            return .send
        }
        startedRecording = false
        if armed {
            mode = .none
            return .cancel
        }
        if held < Self.holdThreshold {
            mode = .tap
            return .none
        }
        mode = .none
        return .send
    }

    /// The system took the press away, such as for an alert or a call. A
    /// held recording is thrown away; a tapped one keeps going.
    mutating func interrupted() -> Effect {
        guard pressedAt != nil else { return .none }
        pressedAt = nil
        cancelArmed = false
        guard startedRecording else { return .none }
        startedRecording = false
        guard mode == .hold else { return .none }
        mode = .none
        return .cancel
    }

    /// Dictation went back to idle some other way: ✕, a failed start, or the words arrived.
    mutating func reset() {
        mode = .none
        cancelArmed = false
        startedRecording = false
    }
}
