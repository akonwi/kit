import Foundation
import KitDictation
import UIKit

/// Lends the microphone to one recording at a time, whichever mic started
/// it: the composer's, an answer's, or a comment's. A recording holds the
/// microphone from when it's prepared until it's finished or thrown away.
/// One that's ending is waited for, so its teardown can't stop the next;
/// one that's still recording refuses the next as unavailable. Transcribing
/// gets background time, so it finishes when the app is left; if iOS ends
/// that time first, the words transcribed so far are kept.
struct SharedMicrophone: DictationEngine {
    /// Keeps the app running for a transcription: called with what to do
    /// if the time runs out, it returns how to end it.
    typealias KeepRunning = @MainActor @Sendable (_ expired: @escaping @MainActor @Sendable () -> Void) -> (@MainActor @Sendable () -> Void)

    let engine: any DictationEngine
    private let lease = Lease()
    private let keepRunning: KeepRunning

    init(_ engine: any DictationEngine, keepRunning: @escaping KeepRunning = BackgroundTime.keep) {
        self.engine = engine
        self.keepRunning = keepRunning
    }

    func requestAccess() async -> DictationAccess {
        await engine.requestAccess()
    }

    func prepare(installing: @escaping @Sendable () -> Void,
                 level: @escaping @Sendable (Double) -> Void) async throws -> any DictationRecording {
        let id = try await lease.acquire()
        do {
            let recording = try await engine.prepare(installing: installing, level: level)
            return LentRecording(recording: recording, id: id, lease: lease, keepRunning: keepRunning)
        } catch {
            await lease.release(id)
            throw error
        }
    }

    /// Who holds the microphone, and who waits for it to be given back.
    /// Only the latest mic waits: an earlier one waiting is refused, so a
    /// mic given up while it waited can't take the microphone ahead of it.
    actor Lease {
        private var holder: UUID?
        private var ending = false
        private var waiting: CheckedContinuation<Bool, Never>?

        func acquire() async throws -> UUID {
            while holder != nil {
                guard ending else { throw DictationProblem.unavailable }
                waiting?.resume(returning: false)
                let turn = await withCheckedContinuation { waiting = $0 }
                guard turn else { throw DictationProblem.unavailable }
            }
            let id = UUID()
            holder = id
            ending = false
            return id
        }

        /// The holder is finishing or throwing its recording away.
        func ending(_ id: UUID) {
            if holder == id { ending = true }
        }

        func release(_ id: UUID) {
            guard holder == id else { return }
            holder = nil
            ending = false
            waiting?.resume(returning: true)
            waiting = nil
        }
    }

    /// A recording that gives the microphone back once it has ended.
    final class LentRecording: DictationRecording {
        let recording: any DictationRecording
        let id: UUID
        let lease: Lease
        let keepRunning: KeepRunning

        init(recording: any DictationRecording, id: UUID, lease: Lease, keepRunning: @escaping KeepRunning) {
            self.recording = recording
            self.id = id
            self.lease = lease
            self.keepRunning = keepRunning
        }

        /// A recording dropped without ending stops, then gives the
        /// microphone back.
        deinit {
            let lease = lease, id = id, recording = recording
            Task {
                await recording.cancel()
                await lease.release(id)
            }
        }

        func start() throws {
            try recording.start()
        }

        /// Finishes in the background time iOS gives. If that time ends
        /// first, the words transcribed so far are the answer at once, before
        /// the app is suspended, and the recording stops.
        func finish() async throws -> String {
            await lease.ending(id)
            let race = Race()
            let recording = recording, lease = lease, id = id
            let end = await keepRunning {
                let soFar = recording.transcribedSoFar()
                race.resolve(soFar.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                             ? .failure(DictationProblem.failed) : .success(soFar))
                Task { await recording.cancel() }
            }
            Task {
                let result: Result<String, any Error>
                do { result = .success(try await recording.finish()) } catch { result = .failure(error) }
                // The microphone is given back only once the recording has stopped.
                await lease.release(id)
                race.resolve(result)
                await end()
            }
            return try await race.value()
        }

        func transcribedSoFar() -> String {
            recording.transcribedSoFar()
        }

        func cancel() async {
            await lease.ending(id)
            await recording.cancel()
            await lease.release(id)
        }
    }
}

/// The first of finishing and running out of time.
private final class Race: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Result<String, any Error>?
    private var waiting: CheckedContinuation<String, any Error>?

    func resolve(_ outcome: Result<String, any Error>) {
        let continuation: CheckedContinuation<String, any Error>? = lock.withLock {
            guard result == nil else { return nil }
            result = outcome
            defer { waiting = nil }
            return waiting
        }
        continuation?.resume(with: outcome)
    }

    func value() async throws -> String {
        try await withCheckedThrowingContinuation { continuation in
            let ready: Result<String, any Error>? = lock.withLock {
                if result == nil { waiting = continuation }
                return result
            }
            if let ready { continuation.resume(with: ready) }
        }
    }
}

/// Background time for one transcription, ended once.
@MainActor final class BackgroundTime {
    private var task = UIBackgroundTaskIdentifier.invalid

    static func keep(expired: @escaping @MainActor @Sendable () -> Void) -> (@MainActor @Sendable () -> Void) {
        let time = begin(expired: expired)
        return { time.end() }
    }

    static func begin(expired: @escaping @MainActor () -> Void) -> BackgroundTime {
        let time = BackgroundTime()
        time.task = UIApplication.shared.beginBackgroundTask(withName: "Dictation") { [time] in
            MainActor.assumeIsolated {
                expired()
                time.end()
            }
        }
        return time
    }

    func end() {
        guard task != .invalid else { return }
        UIApplication.shared.endBackgroundTask(task)
        task = .invalid
    }
}
