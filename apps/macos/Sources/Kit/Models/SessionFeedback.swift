import Foundation
import Observation

/// Evidence retained with the originating session's toast, without flattening paths into a message.
struct SessionReloadReport {
    struct Diagnostic {
        let message: String
        let sourcePath: String?
        let sourceID: String?
        let severity: String?
        let code: String?

        init(message: String, sourcePath: String?, sourceID: String? = nil,
             severity: String? = nil, code: String? = nil) {
            self.message = message; self.sourcePath = sourcePath
            self.sourceID = sourceID; self.severity = severity; self.code = code
        }
    }
    struct Source {
        let id: String
        let path: String?
    }
    let diagnostics: [Diagnostic]
    let sources: [Source]
    let warningCount: Int
    let refreshFailure: String?

    var preview: String {
        if let refreshFailure { return refreshFailure }
        if warningCount > 1 { return "\(warningCount) warnings" }
        return diagnostics.first?.message ?? ""
    }

    var accessibilityDetail: String {
        let issues = diagnostics.map { diagnostic in
            [diagnostic.message, diagnostic.severity, diagnostic.code, diagnostic.sourceID, diagnostic.sourcePath]
                .compactMap { $0 }.joined(separator: " · ")
        }
        let loaded = sources.map { $0.id + ($0.path.map { " · " + $0 } ?? "") }
        return (issues + (loaded.isEmpty ? [] : ["Loaded sources:"] + loaded)).joined(separator: "\n")
    }
}

/// Session-local feedback. Dismissal acknowledges a notice across all surfaces.
@MainActor @Observable
final class SessionFeedback {
    enum Tone { case info, warning, error }
    struct Item: Identifiable {
        let id: UUID
        let key: String
        let title: String
        let detail: String
        let reloadReport: SessionReloadReport?
        let tone: Tone
        let persistent: Bool
        let actionTitle: String?
        let action: (@MainActor () -> Void)?
    }
    private(set) var items: [Item] = []
    @ObservationIgnored private var seen = Set<String>()
    @ObservationIgnored private var acknowledged = Set<String>()
    @ObservationIgnored private var seenOrder: [String] = []
    @ObservationIgnored private var remainingByID: [UUID: TimeInterval] = [:]
    var visible: Item? { items.last }
    var notices: [Item] { items.filter(\.persistent) }

    func show(key: String = UUID().uuidString, title: String, detail: String = "", tone: Tone = .info,
              persistent: Bool = false, actionTitle: String? = nil, action: (@MainActor () -> Void)? = nil,
              reloadReport: SessionReloadReport? = nil) {
        guard !acknowledged.contains(key), !items.contains(where: { $0.key == key }), seen.insert(key).inserted else { return }
        seenOrder.append(key)
        if seenOrder.count > 512 { seen.remove(seenOrder.removeFirst()) }
        items.append(Item(id: UUID(), key: key, title: title, detail: detail, reloadReport: reloadReport,
                          tone: tone, persistent: persistent, actionTitle: actionTitle, action: action))
        // Bound short-lived confirmations without ever evicting an item that
        // requires acknowledgement. Visual grouping is a separate concern.
        while items.filter({ !$0.persistent }).count > 10 {
            if let index = items.firstIndex(where: { !$0.persistent }) {
                remainingByID.removeValue(forKey: items[index].id)
                items.remove(at: index)
            }
        }
    }
    func remainingLifetime(for id: UUID) -> TimeInterval { remainingByID[id] ?? 10 }
    func saveRemainingLifetime(_ remaining: TimeInterval, for id: UUID) {
        if items.contains(where: { $0.id == id && !$0.persistent }) { remainingByID[id] = max(0, remaining) }
    }
    func clear(key: String) {
        for item in items where item.key == key { remainingByID.removeValue(forKey: item.id) }
        items.removeAll { $0.key == key }
        acknowledged.remove(key)
        seen.remove(key); seenOrder.removeAll { $0 == key }
    }
    func dismiss(_ id: UUID) {
        remainingByID.removeValue(forKey: id)
        if let item = items.first(where: { $0.id == id }), item.persistent { acknowledged.insert(item.key) }
        items.removeAll { $0.id == id }
    }
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
