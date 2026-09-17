import Foundation

/// Shell status has no SSE events. Merge bounded polling into the live replica
/// without replacing assistant deltas with an older snapshot.
actor BashWatchProjection {
    private var latest: SessionExcerpt?
    private var executions: [BashExecution] = []
    private var anchors: [String: String] = [:]
    private var lastPublishedID: String?
    private var activeID: String?
    private var hasPoll = false
    private let receive: @Sendable (SessionExcerpt) async -> Void
    init(receive: @escaping @Sendable (SessionExcerpt) async -> Void) { self.receive = receive }
    func stream(_ session: SessionExcerpt) async {
        latest = session
        for row in session.messages { if let bash = row.bash { upsert(bash) } }
        await publish()
    }
    func poll(_ session: SessionExcerpt, active: BashExecution?, settled: [BashExecution]) async {
        let previous = executions, previousID = hasPoll ? activeID : latest?.activeBashID
        hasPoll = true
        activeID = active?.running == true ? active?.id : nil
        for row in session.messages { if let bash = row.bash { upsert(bash) } }
        for value in settled { upsert(value) }
        if let active { upsert(active) }
        if previous != executions || previousID != activeID { await publish() }
    }
    func runningIDs() -> [String] { executions.filter(\.running).map(\.id) }
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
        if hasPoll { session.activeBashID = activeID }
        for execution in executions {
            if let index = session.messages.firstIndex(where: { $0.id == execution.id }) {
                session.messages[index] = execution.message
            } else if anchors[execution.id] == "" {
                session.messages.insert(execution.message, at: 0)
            } else if let anchor = anchors[execution.id], let index = session.messages.firstIndex(where: { $0.id == anchor }) {
                session.messages.insert(execution.message, at: index + 1)
            } else { session.messages.append(execution.message) }
        }
        lastPublishedID = session.messages.last?.id
        await receive(session)
    }
}
