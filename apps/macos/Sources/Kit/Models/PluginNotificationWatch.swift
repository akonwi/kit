import Foundation

/// One attachment-owned live subscription; reconnect always starts fresh.
@MainActor final class PluginNotificationWatch {
    private var task: Task<Void, Never>?
    private var generation = UUID()
    deinit { task?.cancel() }

    func stop() { generation = UUID(); task?.cancel(); task = nil }
    func start(session: String, client: (any PluginNotificationClient)?, receive: @escaping @MainActor (PluginNotification) -> Void) {
        stop()
        guard let client, !session.isEmpty else { return }
        let token = generation
        task = Task { [weak self] in
            var delay = 1
            while !Task.isCancelled {
                do {
                    try await client.watchPluginNotifications(session) { [weak self] notification in
                        await self?.deliver(notification, token: token, receive: receive)
                    }
                } catch is CancellationError { return }
                catch ClientError.http(401) { return }
                catch ClientError.http(403) { return }
                catch ClientError.http(404) { return }
                catch ClientError.invalidPayload { return }
                catch ClientError.oversized { return }
                catch ClientError.incompatible { return }
                catch is DecodingError { return }
                catch { }
                do { try await Task.sleep(for: .seconds(delay)) } catch { return }
                delay = min(15, delay * 2)
            }
        }
    }
    private func deliver(_ notification: PluginNotification, token: UUID, receive: @MainActor (PluginNotification) -> Void) {
        guard token == generation, !Task.isCancelled else { return }
        receive(notification)
    }
}

extension SessionFeedback {
    func showPluginNotification(_ notification: PluginNotification) {
        // Bound persistent plugin alerts independently of other session feedback.
        let retained = items.filter { $0.key.hasPrefix("plugin-alert:") && $0.persistent }
        if notification.persistent, retained.count >= 32 {
            for item in retained.prefix(retained.count - 31) { clear(key: item.key) }
        }
        show(key: "plugin-alert:" + UUID().uuidString, title: notification.title + " · " + notification.pluginID,
             detail: notification.detail, tone: notification.variant == "error" ? .error : notification.variant == "warning" ? .warning : .info,
             persistent: notification.persistent)
    }
    func clearTransientPluginNotifications() {
        for item in items where item.key.hasPrefix("plugin-alert:") && !item.persistent { clear(key: item.key) }
    }
}
