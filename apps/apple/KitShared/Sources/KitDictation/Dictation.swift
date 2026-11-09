import Foundation
import Observation

/// One field's dictation: asks for access, records, and turns what was said
/// into text. Audio is transcribed on the device and never leaves it; only
/// the text goes anywhere, as whatever the field sends.
///
/// The words come all at once when recording stops. A field shows a
/// recording strip in its place until then.
@available(iOS 26, macOS 26, *)
@MainActor @Observable
public final class Dictation {
    public enum Phase: Equatable, Sendable {
        case idle
        /// Asking for access, or getting the recognizer ready. `installing`
        /// is set while the language's speech model downloads, which only
        /// the first recording in a language waits on.
        case preparing(installing: Bool)
        case recording
        /// Recording stopped; the last phrase is being finished.
        case transcribing
    }

    public private(set) var phase: Phase = .idle
    /// When recording began, for the strip's timer.
    public private(set) var startedAt: Date?
    /// The microphone's level from 0 to 1, for the strip's bars.
    public private(set) var level: Double = 0
    /// Why the last attempt gave nothing. Cleared by the next attempt or `dismissProblem()`.
    public private(set) var problem: DictationProblem?

    private let engine: any DictationEngine
    private var recording: (any DictationRecording)?
    /// The last canceled recording's teardown. The next start waits for it,
    /// so it can't stop the audio session the new recording starts.
    private var teardown: Task<Void, Never>?
    /// Bumped by each start and cancel, so a start that was overtaken drops its recording.
    private var attempt = 0

    public init(engine: any DictationEngine) {
        self.engine = engine
    }

    public var isActive: Bool { phase != .idle }
    public var isPreparing: Bool { if case .preparing = phase { true } else { false } }

    /// Starts recording. Returns false when it didn't: access was only just
    /// asked for or was refused, it was canceled while getting ready, or it
    /// failed. A press that brought up the system's prompt doesn't also record.
    @discardableResult
    public func start() async -> Bool {
        guard phase == .idle else { return false }
        attempt += 1
        let mine = attempt
        problem = nil
        phase = .preparing(installing: false)
        await teardown?.value
        switch await engine.requestAccess() {
        case .granted: break
        case .justGranted:
            if attempt == mine { phase = .idle }
            return false
        case .denied:
            if attempt == mine {
                phase = .idle
                problem = .notAllowed
            }
            return false
        }
        guard attempt == mine else { return false }
        do {
            let recording = try await engine.prepare(
                installing: { [weak self] in
                    Task { @MainActor in
                        guard let self, self.attempt == mine, self.isPreparing else { return }
                        self.phase = .preparing(installing: true)
                    }
                },
                level: { [weak self] level in
                    Task { @MainActor in
                        guard let self, self.attempt == mine, self.phase == .recording else { return }
                        self.level = level
                    }
                })
            guard attempt == mine, isPreparing else {
                await recording.cancel()
                return false
            }
            do {
                try recording.start()
            } catch {
                await recording.cancel()
                throw error
            }
            self.recording = recording
            startedAt = Date()
            phase = .recording
            return true
        } catch {
            guard attempt == mine else { return false }
            reset()
            problem = (error as? DictationProblem) ?? .failed
            return false
        }
    }

    /// Stops recording and returns what was said, trimmed. Nil when nothing
    /// was heard, which sets `problem`, or when it wasn't recording.
    public func finish() async -> String? {
        guard phase == .recording, let recording else { return nil }
        phase = .transcribing
        let mine = attempt
        let result: Result<String, Error>
        do { result = .success(try await recording.finish()) } catch { result = .failure(error) }
        guard attempt == mine else { return nil }
        reset()
        switch result {
        case .success(let text):
            let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
            if text.isEmpty { problem = .nothingHeard }
            return text.isEmpty ? nil : text
        case .failure(let error):
            problem = (error as? DictationProblem) ?? .failed
            return nil
        }
    }

    /// Throws the recording away. Nothing is transcribed.
    public func cancel() {
        guard phase != .idle else { return }
        attempt += 1
        let recording = self.recording
        reset()
        guard let recording else { return }
        let previous = teardown
        teardown = Task {
            await previous?.value
            await recording.cancel()
        }
    }

    public func dismissProblem() {
        problem = nil
    }

    private func reset() {
        recording = nil
        phase = .idle
        startedAt = nil
        level = 0
    }
}

/// Why dictation gave nothing.
public enum DictationProblem: Error, Equatable, Sendable {
    /// Microphone or speech recognition access was refused.
    case notAllowed
    /// The recording had no words in it.
    case nothingHeard
    /// The device can't transcribe the current language.
    case unsupportedLanguage
    /// The microphone couldn't be used, or recognition isn't available here.
    case unavailable
    /// Anything else.
    case failed
}

/// Whether dictation may use the microphone and speech recognition.
public enum DictationAccess: Equatable, Sendable {
    case granted
    /// Granted by the prompt this request showed.
    case justGranted
    case denied
}

/// Records and transcribes. `SpeechDictationEngine` uses the device's
/// microphone and speech model; `ScriptedDictationEngine` stands in for tests
/// and screenshots.
public protocol DictationEngine: Sendable {
    /// Asks for microphone and speech recognition access when it hasn't been decided.
    func requestAccess() async -> DictationAccess
    /// Gets a recording ready to start. `installing` is called if a speech
    /// model must download first; `level` is called with the microphone's
    /// level from 0 to 1 while it records.
    func prepare(installing: @escaping @Sendable () -> Void,
                 level: @escaping @Sendable (Double) -> Void) async throws -> any DictationRecording
}

/// One recording.
public protocol DictationRecording: AnyObject, Sendable {
    func start() throws
    /// Stops the microphone and returns everything said.
    func finish() async throws -> String
    /// Stops the microphone and drops what was said.
    func cancel() async
}

/// Joins what was said phrase by phrase.
public enum DictationText {
    /// Appends `segment`, with a space between them unless one is already there.
    public static func join(_ text: String, _ segment: String) -> String {
        guard let last = text.last, let first = segment.first else { return text + segment }
        if last.isWhitespace || first.isWhitespace { return text + segment }
        return text + " " + segment
    }

    /// Rough loudness from 0 to 1 for a buffer of samples: its RMS in
    /// decibels, with -50 dB as silence and -5 dB as full.
    public static func level(_ samples: UnsafeBufferPointer<Float>) -> Double {
        guard !samples.isEmpty else { return 0 }
        var sum: Float = 0
        for sample in samples { sum += sample * sample }
        let rms = (sum / Float(samples.count)).squareRoot()
        let decibels = 20 * log10(max(rms, 0.000_01))
        return Double(min(max((decibels + 50) / 45, 0), 1))
    }
}
