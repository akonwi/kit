/// Waits out a delay. Models that debounce take one, so tests can decide
/// when the delay has passed instead of waiting for the clock.
typealias Sleeper = @Sendable (Duration) async throws -> Void

/// Waits out a delay in real time, throwing if the task is cancelled first.
let realTimeSleeper: Sleeper = { try await Task.sleep(for: $0) }
