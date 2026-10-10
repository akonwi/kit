import Foundation
import Testing
@testable import Kit

struct MCPStatusTests {
    @Test func snapshotProjectsSanitizedMCPStatus() throws {
        let json = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"mcpServers":[{"name":"granola","state":"connected","transport":"http","toolCount":6,"oauthSaved":true,"description":"Meetings","source":"kit-project","configPath":"/tmp/.agents/mcp.json"}],"mcpWarnings":["Ignored invalid server"]}"#
        let wire = try JSONDecoder().decode(WireSessionSnapshot.self, from: Data(json.utf8))
        let projected = try SessionProjection.snapshot(wire)
        let server = try #require(projected.mcpServers?.first)
        #expect(server.name == "granola")
        #expect(server.toolCount == 6)
        #expect(server.oauthSaved)
        #expect(projected.mcpWarnings == ["Ignored invalid server"])
    }

    @Test func snapshotRejectsUnknownMCPState() throws {
        let json = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"mcpServers":[{"name":"bad","state":"secret","transport":"http","toolCount":0,"oauthSaved":false,"source":"kit-user"}]}"#
        let wire = try JSONDecoder().decode(WireSessionSnapshot.self, from: Data(json.utf8))
        #expect(throws: ClientError.self) { try SessionProjection.snapshot(wire) }
    }

    @MainActor @Test func statusViewPollsLatestStatusAndFallsBackToSnapshotWhenClosed() async throws {
        actor PolledClient: SessionClient {
            nonisolated let serverID = "mcp-test"
            nonisolated let isDemo = false
            private(set) var reads = 0
            let polled: SessionExcerpt
            init(polled: SessionExcerpt) { self.polled = polled }
            func sessions() async throws -> [SessionExcerpt] { [] }
            func snapshot(_ id: String) async throws -> SessionExcerpt { reads += 1; return polled }
            func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
        }
        let json = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"mcpServers":[{"name":"granola","state":"connected","transport":"http","toolCount":6,"oauthSaved":true,"description":"Meetings","source":"kit-project","configPath":"/tmp/.agents/mcp.json"}],"mcpWarnings":["Ignored invalid server"]}"#
        let polled = try SessionProjection.snapshot(JSONDecoder().decode(WireSessionSnapshot.self, from: Data(json.utf8)))
        var initial = polled
        initial.mcpServers = []
        initial.mcpWarnings = []
        let client = PolledClient(polled: polled)
        let state = SessionStore(fixture: Fixture(sessions: [initial]), client: client)
        #expect(state.mcpStatus.servers.map(\.name) == [])

        let monitor = Task { await state.monitorMCPStatus(interval: .milliseconds(10)) }
        for _ in 0..<100 where state.mcpStatus.servers.isEmpty { try await Task.sleep(for: .milliseconds(10)) }
        #expect(state.mcpStatus.servers.map(\.name) == ["granola"])
        #expect(state.mcpStatus.servers.map(\.state) == ["connected"])
        #expect(state.mcpStatus.warnings == ["Ignored invalid server"])
        // Polling continues while the view remains open.
        for _ in 0..<200 where await client.reads < 2 { try await Task.sleep(for: .milliseconds(10)) }
        #expect(await client.reads >= 2)

        monitor.cancel()
        await monitor.value
        #expect(state.mcpStatus.servers.map(\.name) == [])
        let reads = await client.reads
        try await Task.sleep(for: .milliseconds(50))
        #expect(await client.reads == reads)
    }
}
