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
}
