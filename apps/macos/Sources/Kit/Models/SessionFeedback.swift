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
///
/// A toast is for a point-in-time outcome with no better home in view: a
/// failure, an outcome the user can act on, a background event, or the result
/// of an explicit request that would otherwise be invisible. Ongoing states,
/// errors owned by a control, and routine successes the UI already shows
/// belong elsewhere.
@MainActor @Observable
final class SessionFeedback {
    enum Tone {
        case info, warning, error

        /// How long a toast of this tone shows when it isn't persistent,
        /// counting only time the stack is visible and unhovered.
        var lifetime: TimeInterval {
            switch self {
            case .info: 5
            case .warning, .error: 8
            }
        }
    }
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
        /// Replaces the tone's icon, as a download's arrow does.
        var icon: Icon? = nil
        /// Where the toast came from when that isn't Kit, such as a plugin's id.
        var source: String? = nil
        /// Whether a repeat of the same notice joins this toast.
        var coalesces = false
        /// How many times the notice arrived while this toast showed.
        var count = 1

        func repeats(_ other: Item) -> Bool {
            coalesces && other.coalesces && reloadReport == nil && other.reloadReport == nil
                && title == other.title && detail == other.detail && tone == other.tone
                && persistent == other.persistent && icon == other.icon && source == other.source
                && actionTitle == other.actionTitle
        }
    }
    private(set) var items: [Item] = []
    @ObservationIgnored private var seen = Set<String>()
    @ObservationIgnored private var acknowledged = Set<String>()
    @ObservationIgnored private var seenOrder: [String] = []
    @ObservationIgnored private var remainingByID: [UUID: TimeInterval] = [:]
    var visible: Item? { items.last }
    var notices: [Item] { items.filter(\.persistent) }

    /// Shows a toast. Errors always persist until acknowledged. A notice
    /// without a `key`, or with `coalesce`, joins a showing toast with the
    /// same content instead of stacking, restarting its lifetime and counting
    /// the repeat.
    func show(key: String? = nil, title: String, detail: String = "", tone: Tone = .info,
              icon: Icon? = nil, persistent: Bool = false, actionTitle: String? = nil, action: (@MainActor () -> Void)? = nil,
              reloadReport: SessionReloadReport? = nil, source: String? = nil, coalesce: Bool = false) {
        let coalesces = key == nil || coalesce
        let key = key ?? UUID().uuidString
        guard !acknowledged.contains(key), !items.contains(where: { $0.key == key }), seen.insert(key).inserted else { return }
        seenOrder.append(key)
        if seenOrder.count > 512 { seen.remove(seenOrder.removeFirst()) }
        var item = Item(id: UUID(), key: key, title: title, detail: detail, reloadReport: reloadReport,
                        tone: tone, persistent: persistent || tone == .error, actionTitle: actionTitle, action: action,
                        icon: icon, source: source, coalesces: coalesces)
        if let index = items.lastIndex(where: { $0.repeats(item) }) {
            let earlier = items.remove(at: index)
            remainingByID.removeValue(forKey: earlier.id)
            item.count = earlier.count + 1
        }
        items.append(item)
        // Bound short-lived confirmations without ever evicting an item that
        // requires acknowledgement. Visual grouping is a separate concern.
        while items.filter({ !$0.persistent }).count > 10 {
            if let index = items.firstIndex(where: { !$0.persistent }) {
                remainingByID.removeValue(forKey: items[index].id)
                items.remove(at: index)
            }
        }
    }
    func remainingLifetime(for id: UUID) -> TimeInterval {
        remainingByID[id] ?? items.first(where: { $0.id == id })?.tone.lifetime ?? Tone.info.lifetime
    }
    func saveRemainingLifetime(_ remaining: TimeInterval, for id: UUID) {
        if items.contains(where: { $0.id == id && !$0.persistent }) { remainingByID[id] = max(0, remaining) }
    }
    func clear(key: String) {
        for item in items where item.key == key { remainingByID.removeValue(forKey: item.id) }
        items.removeAll { $0.key == key }
        acknowledged.remove(key)
        seen.remove(key); seenOrder.removeAll { $0 == key }
    }
    /// Clears every toast whose key starts with `prefix`.
    func clear(prefix: String) {
        for key in Set(items.map(\.key)) where key.hasPrefix(prefix) { clear(key: key) }
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

    /// What VoiceOver says when `item` appears.
    static func announcement(for item: Item) -> String {
        let severity = switch item.tone { case .info: ""; case .warning: "Warning: "; case .error: "Error: " }
        let detail = item.reloadReport?.preview ?? item.detail
        return severity + item.title + (item.source.map { ", from " + $0 } ?? "") + (detail.isEmpty ? "" : ". " + detail)
    }

    /// Reports a session's background outcomes. A completed compaction is
    /// shown by its transcript marker and a failed turn by its transcript row,
    /// so neither raises a toast.
    func observe(_ session: SessionExcerpt, manualCompaction: Bool = false,
                 showSubagents: (@MainActor () -> Void)? = nil) {
        if let outcome = session.compactionOutcome {
            let key = "auto-compaction:" + outcome.id
            if manualCompaction || !outcome.failed {
                // The command owns richer retry feedback for its own operation.
                if seen.insert(key).inserted { seenOrder.append(key) }
            } else {
                show(key: key, title: "Auto-compaction failed", detail: outcome.detail, tone: .error)
            }
        }
        observeSubagentDiagnostics(session.subagentDiagnostics ?? [], showSubagents: showSubagents)
    }

    /// One toast for the session's definition warnings, replaced when they change.
    private func observeSubagentDiagnostics(_ diagnostics: [WireSubagentDiagnostic],
                                            showSubagents: (@MainActor () -> Void)?) {
        let prefix = "subagent-diagnostics:"
        let identities = diagnostics.map { diagnostic in
            [diagnostic.severity, diagnostic.code, diagnostic.message,
             diagnostic.source.kind, diagnostic.source.path, diagnostic.source.pluginId ?? ""]
                .map { "\($0.utf8.count):\($0)" }.joined()
        }
        let key = prefix + Set(identities).sorted().joined(separator: "|")
        for item in items where item.key.hasPrefix(prefix) && item.key != key { clear(key: item.key) }
        guard !diagnostics.isEmpty, !items.contains(where: { $0.key == key }) else { return }
        let detail = diagnostics.map { [$0.message, $0.source.path].filter { !$0.isEmpty }.joined(separator: "\n") }
            .joined(separator: "\n")
        show(key: key, title: diagnostics.count == 1 ? "Subagent definition warning"
                 : "\(diagnostics.count) subagent definition warnings",
             detail: detail, tone: .warning, persistent: true,
             actionTitle: showSubagents == nil ? nil : "Show subagents", action: showSubagents)
    }
}
