import AppKit

/// App-wide Dock feedback for live sessions, including sessions in background tabs.
@MainActor
final class SessionAttention {
    static let shared = SessionAttention()

    private struct State {
        let generation: String?
        let run: String?
        let interactions: Set<String>
    }
    private var states: [SessionIdentity: State] = [:]
    private var order: [SessionIdentity] = []
    private struct Event: Hashable {
        let session: SessionIdentity
        let id: String
    }
    private var delivered = Set<Event>()
    private var deliveredOrder: [Event] = []
    private var lastBounce: TimeInterval?
    private let isActive: () -> Bool
    private let now: () -> TimeInterval
    private let bounce: () -> Void

    init(isActive: @escaping () -> Bool = { NSApp.isActive },
         now: @escaping () -> TimeInterval = { ProcessInfo.processInfo.systemUptime },
         bounce: @escaping () -> Void = { NSApp.requestUserAttention(.informationalRequest) }) {
        self.isActive = isActive; self.now = now; self.bounce = bounce
    }

    func observe(_ session: SessionExcerpt, server: String) {
        let identity = SessionIdentity(server: server, session: session.id)
        let next = State(generation: session.watchGeneration, run: session.activeRunID,
                         interactions: Set((session.pendingInteractions ?? []).map(\.id)))
        let previous = states.updateValue(next, forKey: identity)
        if previous == nil {
            order.append(identity)
            if order.count > 128 { states.removeValue(forKey: order.removeFirst()) }
        }
        // Snapshots establish a baseline; reconnects must not replay old feedback.
        guard let previous, previous.generation == next.generation else { return }
        var events = next.interactions.subtracting(previous.interactions).map { "interaction:\($0)" }
        if let run = previous.run, run != next.run { events.append("run:\(run)") }
        var fresh = false
        for event in events {
            let key = Event(session: identity, id: event)
            if delivered.insert(key).inserted {
                fresh = true
                deliveredOrder.append(key)
                if deliveredOrder.count > 256 { delivered.remove(deliveredOrder.removeFirst()) }
            }
        }
        // Consume foreground events too, so returning to the background is quiet.
        guard fresh, !isActive() else { return }
        let time = now()
        guard lastBounce.map({ time - $0 >= 2 }) ?? true else { return }
        lastBounce = time
        bounce()
    }
}
