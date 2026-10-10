import Foundation

/// Ordered latest-value delivery for one watch.
///
/// Authoritative values are delivered immediately. Live values are coalesced
/// so a token stream produces at most one delivery per interval instead of one
/// per server batch. Deliveries never overlap or reorder, and the most recently
/// sent value is always delivered while ``run()`` is active.
actor LatestValuePublisher<Value: Sendable> {
    private let interval: Duration
    private let deliver: @Sendable (Value) async -> Void
    private let signals: AsyncStream<Void>
    private let signal: AsyncStream<Void>.Continuation
    private var pending: Value?
    private var delivering = false

    init(interval: Duration, deliver: @escaping @Sendable (Value) async -> Void) {
        self.interval = interval
        self.deliver = deliver
        (signals, signal) = AsyncStream.makeStream(of: Void.self, bufferingPolicy: .bufferingNewest(1))
    }

    deinit { signal.finish() }

    /// Replaces the pending value. Coalesced values wait for ``run()``; others
    /// are delivered now, after any delivery already in flight.
    func send(_ value: Value, coalesce: Bool) async {
        pending = value
        if coalesce { signal.yield() } else { await drain() }
    }

    /// Delivers coalesced values, leading edge first, then at most once per
    /// interval. Returns when the calling task is cancelled.
    func run() async {
        for await _ in signals {
            await drain()
            do { try await Task.sleep(for: interval) } catch { return }
        }
    }

    private func drain() async {
        guard !delivering else { return }
        delivering = true
        defer { delivering = false }
        while let value = pending {
            pending = nil
            await deliver(value)
        }
    }
}
