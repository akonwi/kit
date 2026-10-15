import Foundation
import Observation

/// Keeps an answered request in the interaction dock for a moment after the
/// server accepts it. A plugin asking its next question within that window
/// replaces it in place, so the composer doesn't flash between the two.
@MainActor @Observable
final class InteractionHold {
    /// How long the dock waits for a next request before the composer returns.
    static let window: Duration = .milliseconds(300)

    /// The answered request still shown, in its sending state.
    private(set) var flow: InteractionFlow?
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var released: (() -> Void)?

    deinit { task?.cancel() }

    /// Shows `flow` until the window passes, then calls `released`.
    func hold(_ flow: InteractionFlow, for window: Duration = InteractionHold.window,
              released: @escaping () -> Void) {
        task?.cancel()
        self.flow = flow
        self.released = released
        task = Task { [weak self] in
            try? await Task.sleep(for: window)
            guard !Task.isCancelled, let self, self.flow === flow else { return }
            self.release()
        }
    }

    /// Ends the hold now and lets the composer return.
    func release() {
        guard flow != nil else { return }
        let released = self.released
        clear()
        released?()
    }

    /// Ends the hold without returning to the composer, because another
    /// request takes the dock or the session changed.
    func clear() {
        task?.cancel()
        task = nil
        flow = nil
        released = nil
    }
}
