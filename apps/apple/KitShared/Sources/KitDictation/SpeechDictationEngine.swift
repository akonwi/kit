import AVFAudio
import Foundation
import Speech

/// Dictation with the device's microphone and on-device speech model
/// (`SpeechAnalyzer`). The first recording in a language downloads its model.
@available(iOS 26, macOS 26, *)
public struct SpeechDictationEngine: DictationEngine {
    public init() {}

    public func requestAccess() async -> DictationAccess {
        var prompted = false
        switch AVAudioApplication.shared.recordPermission {
        case .granted: break
        case .denied: return .denied
        default:
            prompted = true
            guard await AVAudioApplication.requestRecordPermission() else { return .denied }
        }
        switch SFSpeechRecognizer.authorizationStatus() {
        case .authorized: break
        case .notDetermined:
            prompted = true
            let status = await withCheckedContinuation { continuation in
                SFSpeechRecognizer.requestAuthorization { continuation.resume(returning: $0) }
            }
            guard status == .authorized else { return .denied }
        default: return .denied
        }
        return prompted ? .justGranted : .granted
    }

    public func prepare(installing: @escaping @Sendable () -> Void,
                        level: @escaping @Sendable (Double) -> Void) async throws -> any DictationRecording {
        try await SpeechRecording.make(installing: installing, level: level)
    }
}

/// Microphone → format converter → `SpeechAnalyzer`. The audio thread calls
/// the tap. Stopping is idempotent and guarded by a lock, so a cancel that
/// arrives while finishing doesn't tear the audio down twice.
@available(iOS 26, macOS 26, *)
private final class SpeechRecording: DictationRecording, @unchecked Sendable {
    private let engine = AVAudioEngine()
    private let analyzer: SpeechAnalyzer
    private let format: AVAudioFormat
    private let input: AsyncStream<AnalyzerInput>
    private let continuation: AsyncStream<AnalyzerInput>.Continuation
    private let transcript: Task<String, Error>
    private let level: @Sendable (Double) -> Void
    private let lock = NSLock()
    /// Guarded by `lock`.
    private var tapInstalled = false
    private var sessionActive = false
    private var stopped = false

    private init(analyzer: SpeechAnalyzer, format: AVAudioFormat, transcriber: DictationTranscriber,
                 level: @escaping @Sendable (Double) -> Void) {
        self.analyzer = analyzer
        self.format = format
        self.level = level
        (input, continuation) = AsyncStream.makeStream(of: AnalyzerInput.self)
        transcript = Task {
            var text = ""
            for try await result in transcriber.results {
                text = DictationText.join(text, String(result.text.characters))
            }
            return text
        }
    }

    static func make(installing: @escaping @Sendable () -> Void,
                     level: @escaping @Sendable (Double) -> Void) async throws -> SpeechRecording {
        guard let locale = await DictationTranscriber.supportedLocale(equivalentTo: Locale.current) else {
            throw DictationProblem.unsupportedLanguage
        }
        let transcriber = DictationTranscriber(locale: locale, contentHints: [], transcriptionOptions: [.punctuation],
                                               reportingOptions: [], attributeOptions: [])
        if let request = try await AssetInventory.assetInstallationRequest(supporting: [transcriber]) {
            installing()
            try await request.downloadAndInstall()
        }
        guard let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: [transcriber]) else {
            throw DictationProblem.unavailable
        }
        let analyzer = SpeechAnalyzer(modules: [transcriber])
        try await analyzer.prepareToAnalyze(in: format)
        return SpeechRecording(analyzer: analyzer, format: format, transcriber: transcriber, level: level)
    }

    func start() throws {
        lock.lock()
        defer { lock.unlock() }
        guard !stopped else { throw DictationProblem.failed }
        #if os(iOS)
        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.record, mode: .default)
            try session.setActive(true)
            sessionActive = true
        } catch {
            throw DictationProblem.unavailable
        }
        #endif
        let node = engine.inputNode
        let inputFormat = node.outputFormat(forBus: 0)
        guard inputFormat.sampleRate > 0, let converter = AudioConverter(from: inputFormat, to: format) else {
            throw DictationProblem.unavailable
        }
        let continuation = self.continuation, level = self.level
        node.installTap(onBus: 0, bufferSize: 4096, format: inputFormat) { buffer, _ in
            if let samples = buffer.floatChannelData?[0] {
                level(DictationText.level(UnsafeBufferPointer(start: samples, count: Int(buffer.frameLength))))
            }
            if let converted = converter.convert(buffer) { continuation.yield(AnalyzerInput(buffer: converted)) }
        }
        tapInstalled = true
        engine.prepare()
        do {
            try engine.start()
        } catch {
            throw DictationProblem.unavailable
        }
        let analyzer = self.analyzer, input = self.input
        Task { try? await analyzer.start(inputSequence: input) }
    }

    func finish() async throws -> String {
        stopAudio()
        try await analyzer.finalizeAndFinishThroughEndOfInput()
        return try await transcript.value
    }

    func cancel() async {
        stopAudio()
        transcript.cancel()
        await analyzer.cancelAndFinishNow()
    }

    /// Stops the microphone and ends the analyzer's input, once. The caller
    /// of a failed `start()` cancels, which undoes what it got to.
    private func stopAudio() {
        lock.lock()
        defer { lock.unlock() }
        guard !stopped else { return }
        stopped = true
        if engine.isRunning { engine.stop() }
        if tapInstalled { engine.inputNode.removeTap(onBus: 0) }
        continuation.finish()
        #if os(iOS)
        if sessionActive {
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        }
        #endif
    }
}

/// Converts microphone buffers to the analyzer's format. Used only on the audio thread.
@available(iOS 26, macOS 26, *)
private final class AudioConverter: @unchecked Sendable {
    private let converter: AVAudioConverter
    private let format: AVAudioFormat

    init?(from input: AVAudioFormat, to output: AVAudioFormat) {
        guard let converter = AVAudioConverter(from: input, to: output) else { return nil }
        self.converter = converter
        format = output
    }

    func convert(_ buffer: AVAudioPCMBuffer) -> AVAudioPCMBuffer? {
        let ratio = format.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount((Double(buffer.frameLength) * ratio).rounded(.up)) + 1
        guard let output = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: capacity) else { return nil }
        nonisolated(unsafe) var supplied = false
        var error: NSError?
        converter.convert(to: output, error: &error) { _, status in
            if supplied {
                status.pointee = .noDataNow
                return nil
            }
            supplied = true
            status.pointee = .haveData
            return buffer
        }
        return error == nil && output.frameLength > 0 ? output : nil
    }
}

/// Stands in for the microphone in UI tests and previews: access is as
/// configured, the level rises and falls, and finishing returns `transcript`.
@available(iOS 26, macOS 26, *)
public struct ScriptedDictationEngine: DictationEngine {
    public var transcript: String
    public var access: DictationAccess
    /// How long getting ready and transcribing take.
    public var delay: Duration

    public init(transcript: String, access: DictationAccess = .granted, delay: Duration = .milliseconds(300)) {
        self.transcript = transcript
        self.access = access
        self.delay = delay
    }

    public func requestAccess() async -> DictationAccess { access }

    public func prepare(installing: @escaping @Sendable () -> Void,
                        level: @escaping @Sendable (Double) -> Void) async throws -> any DictationRecording {
        try await Task.sleep(for: delay)
        return ScriptedRecording(transcript: transcript, delay: delay, level: level)
    }
}

@available(iOS 26, macOS 26, *)
private final class ScriptedRecording: DictationRecording, @unchecked Sendable {
    private let transcript: String
    private let delay: Duration
    private let level: @Sendable (Double) -> Void
    private var pulse: Task<Void, Never>?

    init(transcript: String, delay: Duration, level: @escaping @Sendable (Double) -> Void) {
        self.transcript = transcript
        self.delay = delay
        self.level = level
    }

    func start() throws {
        let level = self.level
        pulse = Task {
            var step = 0.0
            while !Task.isCancelled {
                level(0.45 + 0.35 * sin(step))
                step += 0.7
                try? await Task.sleep(for: .milliseconds(100))
            }
        }
    }

    func finish() async throws -> String {
        pulse?.cancel()
        try await Task.sleep(for: delay)
        return transcript
    }

    func cancel() async {
        pulse?.cancel()
    }
}
