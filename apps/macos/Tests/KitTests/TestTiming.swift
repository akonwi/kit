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
