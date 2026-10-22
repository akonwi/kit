import Foundation
import Synchronization
import Testing

/// How long `eventually` waits before failing. It only keeps a broken build
/// from hanging: it is not a measure of how fast anything should be. The
/// suite runs its tests in parallel, and on a CI runner the main actor and
/// the cooperative thread pool have each been seen busy for seconds at a
/// time, so a test must never fail because a wait took a few seconds.
let eventuallyLimit: Duration = .seconds(60)

/// Waits until `condition` holds, checking it every few milliseconds on the
/// caller's actor, and records an issue if it never does.
func eventually(isolation: isolated (any Actor)? = #isolation, sourceLocation: SourceLocation = #_sourceLocation,
                _ condition: () async -> Bool) async throws {
    let deadline = ContinuousClock.now + eventuallyLimit
    while await !condition() {
        guard ContinuousClock.now < deadline else {
            Issue.record("The condition still didn’t hold after \(eventuallyLimit)", sourceLocation: sourceLocation)
            return
        }
        try await Task.sleep(for: .milliseconds(10))
    }
}

/// Time that passes only when a test says so. Code under test sleeps through
/// `sleep(for:)`; the test waits for those sleeps to begin and then ends
/// them with `elapse()`, so a delay's outcome doesn't depend on how busy the
/// machine running the tests is.
final class ManualSleeper: Sendable {
    private struct State {
        var sleeping: [UInt64: CheckedContinuation<Void, any Error>] = [:]
        /// Sleeps cancelled before they could be registered.
        var cancelledEarly: Set<UInt64> = []
        var next: UInt64 = 0
        var durations: [Duration] = []
        var cancelled = 0
    }

    private let state = Mutex(State())

    /// How long each sleep begun so far was for, in order.
    var durations: [Duration] { state.withLock { $0.durations } }
    /// How many sleeps were cancelled before their time passed.
    var cancelled: Int { state.withLock { $0.cancelled } }

    /// Sleeps until `elapse()`, or throws `CancellationError` when the
    /// sleeping task is cancelled first, as `Task.sleep` does.
    @Sendable func sleep(for duration: Duration) async throws {
        let id = state.withLock { state in
            defer { state.next += 1 }
            return state.next
        }
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
                let cancelled = state.withLock { state -> Bool in
                    state.durations.append(duration)
                    guard state.cancelledEarly.remove(id) == nil else {
                        state.cancelled += 1
                        return true
                    }
                    state.sleeping[id] = continuation
                    return false
                }
                if cancelled { continuation.resume(throwing: CancellationError()) }
            }
        } onCancel: {
            let continuation = state.withLock { state -> CheckedContinuation<Void, any Error>? in
                guard let continuation = state.sleeping.removeValue(forKey: id) else {
                    state.cancelledEarly.insert(id)
                    return nil
                }
                state.cancelled += 1
                return continuation
            }
            continuation?.resume(throwing: CancellationError())
        }
    }

    /// Waits until `count` sleeps have begun in all.
    func waitForSleeps(_ count: Int, sourceLocation: SourceLocation = #_sourceLocation) async throws {
        try await eventually(sourceLocation: sourceLocation) { durations.count >= count }
    }

    /// Ends every pending sleep, as though its time passed.
    func elapse() {
        let sleeping = state.withLock { state in
            defer { state.sleeping = [:] }
            return Array(state.sleeping.values)
        }
        sleeping.forEach { $0.resume() }
    }
}
