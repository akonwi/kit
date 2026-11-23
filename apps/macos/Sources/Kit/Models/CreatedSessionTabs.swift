import Foundation
import KitTranscript

/// Sessions a session's model created with create_session, which open as
/// background session tabs beside their creator.
///
/// A session opens only for a call the app saw complete live, and at most
/// once, so loading a transcript, replaying events from before the session
/// attached, or catching up after a reconnect never opens one, and a tab the
/// user closes stays closed.
enum CreatedSessionTabs {
    /// A session a call created.
    struct Created: Equatable {
        let id: String
        /// The name the call gave it, to title its tab until it arrives.
        let name: String
    }

    static let toolName = "create_session"
    /// Remembered calls beyond which those no longer shown are forgotten.
    static let handledLimit = 256

    /// The sessions created by successful live create_session calls in
    /// `messages` not yet in `handled`, oldest first. Every call returned is
    /// added to `handled`.
    static func pending(in messages: [TranscriptMessage], handled: inout Set<String>) -> [Created] {
        var created: [Created] = []
        var visible: Set<String> = []
        for row in messages where row.role == "tools" {
            for tool in row.tools where tool.name == toolName && tool.completedLive == true {
                visible.insert(tool.id)
                guard handled.insert(tool.id).inserted else { continue }
                let presentation = CreatedSessionPresentation(tool)
                guard let id = presentation.sessionID else { continue }
                created.append(Created(id: id, name: presentation.name))
            }
        }
        // A call still shown stays remembered, so it can never open twice.
        if handled.count > handledLimit { handled.formIntersection(visible) }
        return created
    }
}
