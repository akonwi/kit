import Foundation
import Darwin
import Testing
@testable import Kit

struct ClientTests {
    @Test func sseHandlesCommentsCRLFAndMultilineData() throws {
        var parser = SSEParser()
        var frames: [SSEParser.Frame] = []
        for byte in ": heartbeat\r\n\r\nevent: session.events\r\ndata: {\r\ndata: \"events\": []}\r\n\r\n".utf8 {
            if let frame = try parser.append(byte) { frames.append(frame) }
        }
        #expect(frames.count == 1)
        #expect(frames[0].event == "session.events")
        #expect(String(decoding: frames[0].data, as: UTF8.self) == "{\n\"events\": []}")
    }

    @Test func cursorsIgnoreReplayAndRejectGapsAndWrongSession() throws {
        func batch(_ sequences: [Int], session: String = "s") throws -> WireSessionEventBatch {
            let events = sequences.map { ["streamId": "stream", "sequence": $0, "sessionId": session,
                "turnId": "", "runId": "", "kind": "session.renamed"] as [String: Any] }
            return try JSONDecoder().decode(WireSessionEventBatch.self, from: JSONSerialization.data(withJSONObject: ["streamId": "stream", "events": events]))
        }
        #expect(try EventCursor.accept(batch([2, 3, 4]), session: "s", stream: "stream", cursor: 3) == 4)
        #expect(throws: (any Error).self) { try EventCursor.accept(batch([5]), session: "s", stream: "stream", cursor: 3) }
        #expect(throws: (any Error).self) { try EventCursor.accept(batch([4], session: "other"), session: "s", stream: "stream", cursor: 3) }
    }

    @Test func generatedEnumsRejectUnknownContentKinds() {
        #expect(throws: (any Error).self) {
            try JSONDecoder().decode(WireTranscriptContent.self, from: Data(#"{"kind":"invented"}"#.utf8))
        }
    }

    @Test func localEndpointRejectsRemoteAndEmbeddedCredentials() {
        for url in ["http://example.com:80", "http://user:secret@127.0.0.1:1234", "http://127.0.0.1:1234?token=secret"] {
            #expect(throws: (any Error).self) { try HTTPClient(endpoint: URL(string: url)!, token: "test", instance: "i", serverID: "test") }
        }
    }

    @Test func identitiesIncludeServer() {
        #expect(SessionIdentity(server: "one", session: "same") != SessionIdentity(server: "two", session: "same"))
    }

    @MainActor @Test func switchingSessionsRetainsDraftAndWorkspace() {
        let fixture = Fixture(sessions: [
            SessionExcerpt(id: "a", title: "A", sourceTitle: "Test", model: "m", thinking: "high", workspace: "w", date: "", messages: []),
            SessionExcerpt(id: "b", title: "B", sourceTitle: "Test", model: "m", thinking: "high", workspace: "w", date: "", messages: [])
        ])
        let store = SessionStore(fixture: fixture)
        store.ui.draft = "Draft A"
        store.ui.workspace.scratchpad = "Scratch A"
        store.select("b")
        store.ui.draft = "Draft B"
        store.select("a")
        #expect(store.ui.draft == "Draft A")
        #expect(store.ui.workspace.scratchpad == "Scratch A")
        store.select("b")
        #expect(store.ui.draft == "Draft B")
    }
}

struct LiveClientSmokeTests {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_LIVE_TEST"] == "1"))
    func localServerReadOnly() async throws {
        // Warm Foundation networking first, then ensure per-operation transports
        // do not accumulate sockets across catalog refreshes.
        _ = try await LocalClient().sessions()
        func openDescriptors() -> Int {
            (0..<getdtablesize()).reduce(0) { $0 + (fcntl($1, F_GETFD) == -1 ? 0 : 1) }
        }
        try await Task.sleep(for: .milliseconds(100))
        let baseline = openDescriptors()
        for _ in 0..<40 { _ = try await LocalClient().sessions() }
        try await Task.sleep(for: .milliseconds(300))
        #expect(openDescriptors() <= baseline + 8)
        let client = try await HTTPClient.local()
        let sessions = try await client.sessions()
        if let session = sessions.first {
            let snapshot = try await client.snapshot(session.id)
            #expect(snapshot.id == session.id)
            var historyCursor = snapshot.historyCursor
            var seen = Set(snapshot.messages.map { $0.id })
            // Exercise actual server pages without logging transcript content.
            for _ in 0..<3 {
                guard let cursor = historyCursor else { break }
                let page = try await client.history(session.id, before: cursor)
                #expect(page.sessionID == session.id)
                for message in page.messages { #expect(seen.insert(message.id).inserted) }
                historyCursor = page.previousCursor
            }
            // Cancel after the initial snapshot: detachment must not abort the run.
            let streamBaseline = openDescriptors()
            for _ in 0..<8 {
                let task = Task { try await LocalClient().watch(session.id) { _ in } }
                try await Task.sleep(for: .milliseconds(100))
                task.cancel()
                _ = await task.result
            }
            try await Task.sleep(for: .milliseconds(100))
            #expect(openDescriptors() <= streamBaseline + 8)
        }
    }
}
