import Foundation
import Observation

@MainActor @Observable final class BashOperation {
    private(set) var pending = false
    private(set) var stopping = false
    private(set) var error: String?
    private(set) var executions: [BashExecution] = []
    private var anchors: [String: String] = [:]
    private var admission: (id: String, draft: String)?
    var active: BashExecution? { executions.last(where: \.running) }
    var unresolved: Bool { admission != nil }
    func dismissError() { error = nil }
    func record(_ execution: BashExecution) {
        if let index = executions.firstIndex(where: { $0.id == execution.id }) {
            if !executions[index].running && execution.running { return }
            executions[index] = execution
        } else { executions.append(execution) }
        if executions.count > 64 { executions.removeFirst(executions.count - 64) }
        let retained = Set(executions.map(\.id)).union(admission.map { [$0.id] } ?? [])
        anchors = anchors.filter { retained.contains($0.key) }
    }
    func submit(_ draft: String, session: String, client: any BashClient,
                after messageID: String? = nil, acknowledged: @MainActor (String) -> Void) async {
        guard !pending, active == nil, let command = BashDraft(draft), !command.command.isEmpty else { return }
        // Reuse the identity after an uncertain request, even across explicit retries.
        if let admission, admission.draft != draft {
            error = "Resolve the previous shell command before starting another."
            return
        }
        let id = admission?.id ?? "bash_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        if anchors[id] == nil { anchors[id] = messageID ?? "" }
        admission = (id, draft)
        pending = true; error = nil
        defer { pending = false }
        do {
            var execution: BashExecution
            do {
                execution = try await client.startBash(session, input: .init(executionId: id, command: command.command, excludeFromContext: command.excluded))
            } catch {
                // Resolve acknowledgement loss by identity; never create a second command.
                if let found = try? await client.bash(session, id: id) { execution = found }
                else {
                    if error is MutationNotSent { admission = nil }
                    if case ClientError.http(let status) = error, [400, 401, 403, 404].contains(status) { admission = nil }
                    throw error
                }
            }
            record(execution); admission = nil; acknowledged(draft)
        } catch { self.error = error.localizedDescription }
    }
    func merge(into messages: [TranscriptMessage]) -> [TranscriptMessage] {
        var rows = messages
        for execution in executions {
            if let index = rows.firstIndex(where: { $0.id == execution.id }) {
                if rows[index].bash?.running != false || !execution.running { rows[index] = execution.message }
            } else if anchors[execution.id] == "" {
                rows.insert(execution.message, at: 0)
            } else if let anchor = anchors[execution.id], let index = rows.firstIndex(where: { $0.id == anchor }) {
                rows.insert(execution.message, at: index + 1)
            } else { rows.append(execution.message) }
        }
        return rows
    }
    func retry(session: String, client: any BashClient, acknowledged: @MainActor (String) -> Void) async {
        guard let admission else { return }
        await submit(admission.draft, session: session, client: client, acknowledged: acknowledged)
    }
    func resolve(session: String, client: any BashClient, acknowledged: @MainActor (String) -> Void) async {
        guard let admission, !pending else { return }
        pending = true
        defer { pending = false }
        do {
            let value: BashExecution
            do { value = try await client.bash(session, id: admission.id) }
            catch {
                let snapshot = try await client.snapshot(session)
                guard let found = snapshot.messages.first(where: { $0.bash?.id == admission.id })?.bash else { throw error }
                value = found
            }
            record(value); self.admission = nil; error = nil; acknowledged(admission.draft)
        } catch { self.error = error.localizedDescription }
    }
    func refresh(session: String, client: any BashClient) async {
        for execution in executions where execution.running {
            do { record(try await client.bash(session, id: execution.id)) }
            catch { self.error = error.localizedDescription }
        }
    }
    func abort(session: String, id: String, client: any BashClient) async {
        guard !stopping else { return }
        stopping = true; error = nil
        defer { stopping = false }
        do { try await client.abortBash(session, id: id) }
        catch { self.error = error.localizedDescription }
    }
}
