import Foundation

/// When the app saw a tool call start and finish.
struct ToolTiming: Equatable, Sendable {
    var started: Date
    var finished: Date?

    /// Keeps the times of watched calls across snapshots, which carry none,
    /// for as long as the replica lives. Times recorded live win; snapshot
    /// rows take the times recorded for their call.
    static func merge(_ messages: [TranscriptMessage], log: inout [String: ToolTiming], limit: Int = 4096) -> [TranscriptMessage] {
        var result = messages
        for row in result.indices where result[row].role == "tools" {
            for index in result[row].tools.indices {
                let tool = result[row].tools[index]
                if let started = tool.startedAt {
                    let finished = tool.finishedAt ?? log[tool.id]?.finished
                    log[tool.id] = ToolTiming(started: started, finished: finished)
                    result[row].tools[index].finishedAt = finished
                } else if let timing = log[tool.id] {
                    result[row].tools[index].startedAt = timing.started
                    result[row].tools[index].finishedAt = timing.finished
                }
            }
        }
        if log.count > limit {
            let visible = Set(result.flatMap { $0.tools.map(\.id) })
            log = log.filter { visible.contains($0.key) }
        }
        return result
    }

    /// A duration as the transcript shows it: "<1s", "8s", "1m 12s", "2h 5m".
    static func format(_ interval: TimeInterval) -> String {
        let seconds = Int(max(0, interval))
        if seconds < 1 { return "<1s" }
        if seconds < 60 { return "\(seconds)s" }
        if seconds < 3600 { return "\(seconds / 60)m \(seconds % 60)s" }
        return "\(seconds / 3600)h \(seconds % 3600 / 60)m"
    }
}
