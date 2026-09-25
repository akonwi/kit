import Foundation
import Observation

/// Session-local feedback. Dismissal acknowledges a notice across all surfaces.
@MainActor @Observable
final class SessionFeedback {
    enum Tone { case info, warning, error }
    struct Item: Identifiable {
        let id: UUID
        let key: String
        let title: String
        let detail: String
        let tone: Tone
        let persistent: Bool
        let actionTitle: String?
        let action: (@MainActor () -> Void)?
    }
    private(set) var items: [Item] = []
    @ObservationIgnored private var seen = Set<String>()
    @ObservationIgnored private var seenOrder: [String] = []
    var visible: Item? { items.last }
    var notices: [Item] { items.filter(\.persistent) }

    func show(key: String = UUID().uuidString, title: String, detail: String = "", tone: Tone = .info,
              persistent: Bool = false, actionTitle: String? = nil, action: (@MainActor () -> Void)? = nil) {
        guard seen.insert(key).inserted else { return }
        seenOrder.append(key)
        if seenOrder.count > 512 { seen.remove(seenOrder.removeFirst()) }
        // Replacing an ephemeral card prevents delayed, stale confirmations.
        items.removeAll { !$0.persistent }
        items.append(Item(id: UUID(), key: key, title: title, detail: detail, tone: tone,
                          persistent: persistent, actionTitle: actionTitle, action: action))
    }
    func clear(key: String) {
        items.removeAll { $0.key == key }
        seen.remove(key); seenOrder.removeAll { $0 == key }
    }
    func dismiss(_ id: UUID) { items.removeAll { $0.id == id } }
    func reveal(_ id: UUID) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        let item = items.remove(at: index); items.append(item)
    }
    func expire(_ id: UUID) {
        guard items.first(where: { $0.id == id })?.persistent == false else { return }
        dismiss(id)
    }

    func observe(_ session: SessionExcerpt, manualCompaction: Bool = false) {
        if let outcome = session.compactionOutcome {
            let key = "auto-compaction:" + outcome.id
            if manualCompaction {
                // The command owns richer retry feedback for its own operation.
                if seen.insert(key).inserted { seenOrder.append(key) }
            } else {
                show(key: key, title: outcome.failed ? "Auto-compaction failed" : "Session compacted",
                     detail: outcome.detail, tone: outcome.failed ? .error : .info, persistent: outcome.failed)
            }
        }
        for diagnostic in session.subagentDiagnostics ?? [] {
            let fields = [diagnostic.severity, diagnostic.code, diagnostic.message,
                          diagnostic.source.kind, diagnostic.source.path, diagnostic.source.pluginId ?? ""]
            let key = fields.map { "\($0.utf8.count):\($0)" }.joined()
            show(key: "diagnostic:" + key, title: "Subagent definition warning",
                 detail: [diagnostic.message, diagnostic.source.path].filter { !$0.isEmpty }.joined(separator: "\n"),
                 tone: .warning, persistent: true)
        }
        if let error = session.terminalError, !error.isEmpty {
            show(key: "run-error:\(session.messages.last?.id ?? ""):\(error)",
                 title: "Agent turn failed", detail: error, tone: .error, persistent: true)
        }
    }
}
