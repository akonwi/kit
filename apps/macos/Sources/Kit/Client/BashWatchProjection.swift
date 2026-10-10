import Foundation

/// Shell status has no SSE events. Merge targeted execution polling into the
/// live replica without replacing assistant deltas with an older snapshot.
///
/// Stream deliveries reveal executions through bash rows and the snapshot's
/// active execution. Only executions that are running, or announced but not
/// yet read, are polled; an idle session performs no shell requests.
actor BashWatchProjection {
    private var latest: SessionExcerpt?
    private var executions: [BashExecution] = []
    private var anchors: [String: String] = [:]
    private var lastPublishedID: String?
    private let receive: @Sendable (SessionExcerpt) async -> Void
    private let signals: AsyncStream<Void>
    private let signal: AsyncStream<Void>.Continuation

    init(receive: @escaping @Sendable (SessionExcerpt) async -> Void) {
        self.receive = receive
        (signals, signal) = AsyncStream.makeStream(of: Void.self, bufferingPolicy: .bufferingNewest(1))
    }

    deinit { signal.finish() }

    func stream(_ session: SessionExcerpt) async {
        latest = session
        for row in session.messages { if let bash = row.bash { upsert(bash) } }
        if !pollTargets().isEmpty { signal.yield() }
        await publish()
    }

    func poll(_ results: [BashExecution]) async {
        let previous = executions, previousID = activeID
        for value in results { upsert(value) }
        if previous != executions || previousID != activeID { await publish() }
    }

    /// Executions whose status must be read from the server.
    func pollTargets() -> [String] {
        var ids = executions.filter(\.running).map(\.id)
        if let announced = latest?.activeBashID, !executions.contains(where: { $0.id == announced }) {
            ids.append(announced)
        }
        return ids
    }

    /// Polls running executions until none remain, then waits for a delivery
    /// that announces another. Returns when the calling task is cancelled.
    func watch(interval: Duration, read: @escaping @Sendable (String) async throws -> BashExecution) async throws {
        for await _ in signals {
            while !Task.isCancelled {
                let ids = pollTargets()
                guard !ids.isEmpty else { break }
                var results: [BashExecution] = []
                for id in ids { results.append(try await read(id)) }
                await poll(results)
                try await Task.sleep(for: interval)
            }
        }
        try Task.checkCancellation()
    }

    /// A tracked running execution, else a snapshot-announced one not yet read.
    private var activeID: String? {
        if let running = executions.last(where: \.running) { return running.id }
        guard let announced = latest?.activeBashID, !executions.contains(where: { $0.id == announced }) else { return nil }
        return announced
    }

    private func upsert(_ value: BashExecution) {
        if let index = executions.firstIndex(where: { $0.id == value.id }) {
            if !executions[index].running && value.running { return }
            executions[index] = value
        } else {
            anchors[value.id] = lastPublishedID ?? ""
            executions.append(value)
        }
        if executions.count > 64 { executions.removeFirst(executions.count - 64) }
        let retained = Set(executions.map(\.id))
        anchors = anchors.filter { retained.contains($0.key) }
    }

    private func publish() async {
        guard var session = latest else { return }
        session.activeBashID = activeID
        session.messages = BashExecution.merge(executions, anchors: anchors, into: session.messages)
        lastPublishedID = session.messages.last?.id
        await receive(session)
    }
}
