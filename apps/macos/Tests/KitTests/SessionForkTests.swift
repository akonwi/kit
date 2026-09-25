import Foundation
import Testing
@testable import Kit

private actor ForkClient: SessionForkClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var requests: [WireForkSessionInput] = []
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws { }
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> SessionExcerpt {
        requests.append(input)
        if requests.count == 1 { throw ClientError.disconnected }
        return SessionExcerpt(id: input.id!, title: input.name!, parentSessionID: id,
            sourceTitle: "Test", model: "m", thinking: "high", workspace: "Test", date: "", messages: [])
    }
}

@MainActor struct SessionForkTests {
    @Test func lostAcknowledgementRetriesTheSameChild() async throws {
        let client = ForkClient(), operation = SessionForkOperation()
        let first = await operation.submit(client: client, source: "parent", name: "  Child  ")
        #expect(first == nil)
        #expect(operation.error == ClientError.disconnected.localizedDescription)
        let child = try #require(await operation.submit(client: client, source: "parent", name: "Ignored edit"))
        let requests = await client.requests
        #expect(requests.count == 2)
        #expect(requests[0].id == requests[1].id)
        #expect(requests[1].name == "Child")
        #expect(child.parentSessionID == "parent")
        #expect(child.title == "Child")
        #expect(operation.pending == false)
    }

    @Test func invalidNameStaysEditableWithoutRequest() async {
        let client = ForkClient(), operation = SessionForkOperation()
        _ = await operation.submit(client: client, source: "parent", name: String(repeating: "x", count: 257))
        #expect(operation.input == nil)
        #expect(operation.error == "Use a name without control characters, up to 256 bytes.")
        #expect(await client.requests.isEmpty)
    }
}
