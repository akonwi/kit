import Foundation
import OSLog

/// Hosts one client plugin instance for a window's watched session
/// (ADR 0051). The session watch carries the instance's calls; the host runs
/// each call once, answers it, and stops work on calls that ended.
@MainActor final class ClientPluginHost {
    let instanceID = ClientPluginHosting.newInstanceID()
    let declaration: WireAttachClientPluginInput
    private let perform: @MainActor (ClientPluginCallRequest) async -> WireCompleteClientPluginCallInput
    /// The session the instance is attached to.
    private(set) var session: String?
    private var client: (any ClientPluginHostingClient)?
    /// Calls already taken, oldest first, so a call delivered again (as on a
    /// reconnect) runs at most once.
    private var taken: [String] = []
    private var takenSet: Set<String> = []
    private var running: [String: Task<Void, Never>] = [:]
    /// Waits before each retry of an answer that couldn't be sent.
    var retryDelays: [Duration] = [.milliseconds(500), .seconds(1), .seconds(2), .seconds(4)]
    static let takenLimit = 512
    nonisolated private static let log = Logger(subsystem: AppIdentity.bundleIdentifier, category: "client-plugins")

    init(declaration: WireAttachClientPluginInput,
         perform: @escaping @MainActor (ClientPluginCallRequest) async -> WireCompleteClientPluginCallInput) {
        self.declaration = declaration
        self.perform = perform
    }

    /// The hosting a watch of `session` through `client` carries. Watching
    /// another session first leaves the previous one.
    func hosting(session id: String, client: any ClientPluginHostingClient) -> ClientPluginHosting {
        if session != id || client.serverID != self.client?.serverID { leave() }
        session = id
        self.client = client
        let plugin = declaration.pluginId
        var hosting = ClientPluginHosting(instanceID: instanceID, declaration: declaration) { [weak self] event in
            await self?.receive(event, session: id)
        }
        hosting.refused = { error in
            Self.log.notice("Session \(id, privacy: .public) refused the \(plugin, privacy: .public) plugin: \(error.localizedDescription, privacy: .public)")
        }
        return hosting
    }

    /// Stops the instance on its session: work in progress stops, and the
    /// server ends its pending calls as unavailable.
    func leave() {
        for task in running.values { task.cancel() }
        running = [:]
        guard let session, let client else { return }
        self.session = nil
        self.client = nil
        let instance = instanceID
        Task { try? await client.detachClientPlugin(session: session, instance: instance) }
    }

    func receive(_ event: ClientPluginCallEvent, session id: String) {
        guard id == session, let client else { return }
        switch event {
        case .requested(let call, let request):
            guard takenSet.insert(call).inserted else { return }
            taken.append(call)
            if taken.count > Self.takenLimit { takenSet.remove(taken.removeFirst()) }
            running[call] = Task { [weak self] in
                guard let self else { return }
                let result = await self.perform(request)
                guard !Task.isCancelled else { return }
                await self.answer(call, result: result, session: id, client: client)
                if self.session == id { self.running[call] = nil }
            }
        case .resolved(let call, _):
            running.removeValue(forKey: call)?.cancel()
        }
    }

    /// Sends an answer, retrying while the call may still be pending.
    private func answer(_ call: String, result: WireCompleteClientPluginCallInput, session id: String,
                        client: any ClientPluginHostingClient) async {
        var delays = retryDelays[...]
        while true {
            do {
                try await client.completeClientPluginCall(session: id, instance: instanceID, call: call, result: result)
                return
            } catch {
                // The call ended, the instance is gone, or the answer was refused.
                if case ClientError.http(let status) = error, (400..<500).contains(status) { return }
                guard let delay = delays.popFirst(), !Task.isCancelled else { return }
                try? await Task.sleep(for: delay)
                guard !Task.isCancelled, session == id else { return }
            }
        }
    }
}
