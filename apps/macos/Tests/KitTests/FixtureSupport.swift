import Foundation
@testable import Kit

extension Fixture {
    /// Synthetic data for tests that do not require a daemon or private recordings.
    static func load() throws -> Fixture {
        Fixture(sessions: [SessionExcerpt(id: "welcome", title: "Test session", sourceTitle: "Test", model: "Test model", thinking: "high", workspace: "Test", date: "Test", messages: [TranscriptMessage(id: "welcome-message", role: "assistant", text: "Test conversation", tools: [])])])
    }
}
