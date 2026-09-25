import Foundation

/// Only the latest group in the current conversation turn stays active between calls.
enum ToolGroupActivity {
    static func liveGroups(in messages: [TranscriptMessage], active: Bool) -> Set<String> {
        guard active else { return [] }
        let currentTurn = messages.reversed().prefix { $0.role != "user" }
        let groups = currentTurn.filter { $0.role == "tools" && !$0.tools.isEmpty }
        var live = Set(groups.filter { group in
            group.tools.contains { $0.status == "Running…" || $0.status == "Planned" }
        }.map(\.id))
        if let latest = groups.first { live.insert(latest.id) }
        return live
    }
}
