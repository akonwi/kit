import AppKit
import SwiftUI

/// The value that identifies a session window. Its sessions live in the
/// window's model, so restoring the value restores the window's tabs.
struct SessionWindowRequest: Codable, Hashable {
    var windowID: UUID
}

/// Owns live window models, restoration intent, and the servers every window
/// shares. Each window owns its session tabs; a session is shown in at most
/// one tab across windows.
@MainActor
final class SessionWindowRegistry: NSObject {
    private final class WeakModel {
        weak var value: SessionWindowModel?
        init(_ value: SessionWindowModel) { self.value = value }
    }
    private var models: [UUID: WeakModel] = [:]
    let restoration: WindowRestorationStore
    let servers: ServerDirectory
    var terminating = false
    private var restored = false

    init(restoration: WindowRestorationStore = WindowRestorationStore(), servers: ServerDirectory = .inert()) {
        self.restoration = restoration
        self.servers = servers
        super.init()
        servers.serverRenamed = { [weak self] name, newName in
            for model in self?.liveModels ?? [] { model.serverRenamed(from: name, to: newName) }
        }
        servers.serverRemoved = { [weak self] name in
            for model in self?.liveModels ?? [] { model.serverRemoved(name) }
        }
        servers.openTabs = { [weak self] name in
            (self?.liveModels ?? []).reduce(0) { count, model in
                count + model.tabs.filter { !$0.isLauncher && $0.identity.server == name }.count
            }
        }
        servers.busyTerminals = { [weak self] name in
            (self?.liveModels ?? []).reduce(0) { count, model in
                count + model.tabs.filter { $0.identity.server == name }.reduce(0) { $0 + ($1.store?.busyTerminals.count ?? 0) }
            }
        }
    }

    var liveModels: [SessionWindowModel] {
        models = models.filter { $0.value.value != nil }
        return models.values.compactMap(\.value)
    }

    /// The window to open when SwiftUI asks for one without a value.
    func defaultRequest() -> SessionWindowRequest {
        let live = Set(liveModels.map(\.id))
        let saved = restoration.windows.first { !live.contains($0.id) }
        return SessionWindowRequest(windowID: saved?.id ?? UUID())
    }

    /// Returns the window's model, seeded from restoration intent.
    func model(for request: SessionWindowRequest) -> SessionWindowModel {
        if let model = models[request.windowID]?.value { return model }
        let saved = restoration.window(request.windowID)
        let model = SessionWindowModel(id: request.windowID, servers: servers, sessions: saved?.sessions ?? [], selected: saved?.selected,
                                       layouts: saved?.layouts ?? [:], collapsed: saved?.collapsed ?? [],
                                       titles: saved?.titles ?? [:])
        model.onChange = { [weak self] in self?.capture($0) }
        models[request.windowID] = WeakModel(model)
        return model
    }

    /// Opens saved windows that SwiftUI did not restore itself.
    func restoreRemaining(open: @escaping (SessionWindowRequest) -> Void) {
        guard !restored else { return }
        restored = true
        Task { @MainActor in
            // Let natively restored scenes create their models first.
            try? await Task.sleep(for: .milliseconds(300))
            guard !terminating else { return }
            let live = Set(liveModels.map(\.id))
            for window in restoration.windows where !live.contains(window.id) {
                open(SessionWindowRequest(windowID: window.id))
            }
        }
    }

    /// Records a window's tabs, their tile layouts and titles, and its
    /// collapsed families as restoration intent.
    func capture(_ model: SessionWindowModel) {
        guard !terminating else { return }
        restoration.update(.init(id: model.id, sessions: model.sessions, selected: model.selectedSession,
                                 layouts: model.layouts, collapsed: model.collapsed, titles: model.titles))
    }

    /// Records every window's intent before quitting, including layout
    /// changes still waiting and titles learned since the last change.
    func captureBeforeQuit() {
        for model in liveModels {
            model.captureLayouts()
            capture(model)
        }
    }

    /// Attaches a model to its window. A second window for the same model,
    /// as a native restoration can create, is closed.
    func attach(_ model: SessionWindowModel, to window: NSWindow) {
        if let existing = model.window, existing !== window, existing.isVisible {
            existing.makeKeyAndOrderFront(nil)
            window.close()
            return
        }
        guard model.window !== window else { return }
        model.window = window
        model.trafficLights = TrafficLightPlacement(window: window)
        // The close button asks first if closing would end running commands.
        if let close = window.standardWindowButton(.closeButton) {
            close.target = self
            close.action = #selector(closeButtonClicked(_:))
        }
        window.tabbingMode = .disallowed
        NotificationCenter.default.addObserver(self, selector: #selector(windowWillClose(_:)),
                                               name: NSWindow.willCloseNotification, object: window)
        for name in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification] {
            NotificationCenter.default.addObserver(self, selector: #selector(windowKeyChanged(_:)), name: name, object: window)
        }
        model.isFocused = window.isKeyWindow
        capture(model)
    }

    @objc private func closeButtonClicked(_ sender: NSButton) {
        guard let window = sender.window else { return }
        let busy = liveModels.first { $0.window === window }?.busyTerminals ?? []
        TerminalCloseConfirmation.confirm(busy, scope: .window, in: window) { [weak window] in
            window?.close()
        }
    }

    @objc private func windowKeyChanged(_ notification: Notification) {
        guard let window = notification.object as? NSWindow,
              let model = liveModels.first(where: { $0.window === window }) else { return }
        model.isFocused = window.isKeyWindow
    }

    @objc private func windowWillClose(_ notification: Notification) {
        guard let window = notification.object as? NSWindow,
              let model = liveModels.first(where: { $0.window === window }) else { return }
        NotificationCenter.default.removeObserver(self, name: nil, object: window)
        model.closeAll()
        model.window = nil
        model.trafficLights = nil
        models[model.id] = nil
        // Quitting keeps open windows for the next launch; closing forgets.
        if !terminating { restoration.remove(model.id) }
    }

    /// Shows a session: focuses its tab in whichever window has it, or opens
    /// it as a tab in `source`.
    func open(_ identity: SessionIdentity, from source: SessionWindowModel) {
        guard !terminating, !identity.session.isEmpty else { return }
        if let owner = liveModels.first(where: { $0.tab(for: identity) != nil }), owner !== source {
            owner.open(identity)
            owner.window?.makeKeyAndOrderFront(nil)
            return
        }
        source.open(identity)
    }
}

/// Reports the hosting window to its model's registry.
struct SessionWindowAttachment: NSViewRepresentable {
    let registry: SessionWindowRegistry
    let model: SessionWindowModel

    func makeNSView(context: Context) -> WindowProbe { WindowProbe() }
    func updateNSView(_ view: WindowProbe, context: Context) {
        view.configure = { [registry, model] window in registry.attach(model, to: window) }
        view.scheduleConfiguration()
    }

    final class WindowProbe: NSView {
        var configure: ((NSWindow) -> Void)?
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            scheduleConfiguration()
        }
        func scheduleConfiguration() {
            DispatchQueue.main.async { [weak self] in
                guard let self, let window else { return }
                configure?(window)
            }
        }
    }
}
