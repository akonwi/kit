import Foundation

protocol SubagentMessagingClient: SubagentClient {
    func sendSubagent(_ session: String, agent: String, conversation: String?, message: String) async throws -> SubagentReceipt
}

struct SubagentReceipt: Sendable {
    let conversationID: String
    let taskID: String
    let warning: String?

    init(_ result: WireSubagentOperationResult, agent: String, conversation: String?) throws {
        func identifier(_ value: String, prefix: String) -> Bool {
            value.hasPrefix(prefix) && value.dropFirst(prefix.count).count == 32 &&
                value.dropFirst(prefix.count).allSatisfy { "0123456789abcdef".contains($0) }
        }
        guard let child = result.conversation, let task = result.task,
              identifier(child.id, prefix: "subagent_"), identifier(task.id, prefix: "task_"),
              child.agentName == agent, conversation == nil || child.id == conversation,
              !child.model.isEmpty, child.generation > 0, child.queuedTasks >= 0,
              ["idle", "running", "failed", "aborted", "interrupted"].contains(child.state),
              ["queued", "running", "completed", "failed", "aborted", "interrupted"].contains(task.state),
              task.sequence > 0, task.cancellationGeneration > 0,
              (result.warning?.utf8.count ?? 0) <= 4096 else { throw ClientError.invalidPayload }
        conversationID = child.id
        taskID = task.id
        warning = result.warning
    }
}

extension LocalClient: SubagentMessagingClient {
    func sendSubagent(_ session: String, agent: String, conversation: String?, message: String) async throws -> SubagentReceipt {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.sendSubagent(session, agent: agent, conversation: conversation, message: message)
    }
}
