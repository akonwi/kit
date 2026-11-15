import Foundation
import KitDictation

/// Lends the microphone to one recording at a time, whichever mic started
/// it: the composer's, an answer's, or a comment's. A recording holds the
/// microphone from when it's prepared until it's finished or thrown away.
/// One that's ending is waited for, so its teardown can't stop the next;
/// one that's still recording refuses the next as unavailable.
struct SharedMicrophone: DictationEngine {
    let engine: any DictationEngine
    private let lease = Lease()

    init(_ engine: any DictationEngine) {
        self.engine = engine
    }

    func requestAccess() async -> DictationAccess {
        await engine.requestAccess()
    }

    func prepare(installing: @escaping @Sendable () -> Void,
                 level: @escaping @Sendable (Double) -> Void) async throws -> any DictationRecording {
        let id = try await lease.acquire()
        do {
            let recording = try await engine.prepare(installing: installing, level: level)
            return LentRecording(recording: recording, id: id, lease: lease)
        } catch {
            await lease.release(id)
            throw error
        }
    }

    /// Who holds the microphone, and who waits for it to be given back.
    actor Lease {
        private var holder: UUID?
        private var ending = false
        private var waiting: [CheckedContinuation<Void, Never>] = []

        func acquire() async throws -> UUID {
            while holder != nil {
                guard ending else { throw DictationProblem.unavailable }
                await withCheckedContinuation { waiting.append($0) }
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
            let resumed = waiting
            waiting = []
            for continuation in resumed { continuation.resume() }
        }
    }

    /// A recording that gives the microphone back once it has ended.
    final class LentRecording: DictationRecording {
        let recording: any DictationRecording
        let id: UUID
        let lease: Lease

        init(recording: any DictationRecording, id: UUID, lease: Lease) {
            self.recording = recording
            self.id = id
            self.lease = lease
        }

        /// A recording dropped without ending still gives the microphone back.
        deinit {
            let lease = lease, id = id
            Task { await lease.release(id) }
        }

        func start() throws {
            try recording.start()
        }

        func finish() async throws -> String {
            await lease.ending(id)
            do {
                let words = try await recording.finish()
                await lease.release(id)
                return words
            } catch {
                await lease.release(id)
                throw error
            }
        }

        func cancel() async {
            await lease.ending(id)
            await recording.cancel()
            await lease.release(id)
        }
    }
}
