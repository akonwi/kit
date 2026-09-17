import AppKit
import SwiftUI

struct SessionWindowRequest: Codable, Hashable {
    var serverID: String = ""
    var sessionID: String
    var identity: SessionIdentity { SessionIdentity(server: serverID, session: sessionID) }

}

/// Owns window identity, restoration, and grouping. Each scene owns its session state.
@MainActor
final class SessionWindowRegistry: NSObject {
    private struct Entry {
        weak var window: NSWindow?
        var identity: SessionIdentity
    }
    private var pending: Set<SessionIdentity> = []
    private var tabTargets: [SessionIdentity: WeakWindow] = [:]
    private final class WeakWindow {
        weak var value: NSWindow?
        init(_ value: NSWindow) { self.value = value }
    }
    private var entries: [ObjectIdentifier: Entry] = [:]
    // Retain only weak references so late view callbacks cannot resurrect closed
    // windows, without confusing a future window that reuses an object address.
    private var closed: [ObjectIdentifier: WeakWindow] = [:]

    private func observeClose(_ window: NSWindow) {
        NotificationCenter.default.removeObserver(self, name: NSWindow.willCloseNotification, object: window)
        NotificationCenter.default.addObserver(self, selector: #selector(windowClosed(_:)),
                                               name: NSWindow.willCloseNotification, object: window)
    }

    private func retire(_ window: NSWindow) {
        observeClose(window)
        closed[ObjectIdentifier(window)] = WeakWindow(window)
        window.close()
    }

    private func isClosed(_ window: NSWindow) -> Bool {
        closed = closed.filter { $0.value.value != nil }
        return closed[ObjectIdentifier(window)]?.value === window
    }

    var registeredIdentities: [SessionIdentity] {
        entries.values.filter { $0.window != nil }.map(\.identity)
    }
    let restoration: WindowRestorationStore
    var terminating = false {
        willSet { if newValue && !terminating { captureLayout() } }
    }
    private var restored = false
    private var selectionsToRestore: Set<SessionIdentity> = []
    private var serverID = ""

    init(restoration: WindowRestorationStore = WindowRestorationStore()) {
        self.restoration = restoration
        selectionsToRestore = Set(restoration.groups.compactMap(\.selected))
        super.init()
    }

    func restoreRemaining(open: @escaping (SessionWindowRequest) -> Void) {
        self.open = open
        guard !restored else { return }
        restored = true
        // Let native restored scenes register first; openSession deduplicates them.
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(300))
            guard !terminating else { return }
            for identity in restoration.sessions where !entries.values.contains(where: { $0.identity == identity && $0.window != nil }) {
                openSession(identity.session, serverID: identity.server, open: open)
            }
        }
    }

    @discardableResult
    func register(_ window: NSWindow, request: SessionWindowRequest) -> Bool {
        let key = ObjectIdentifier(window)
        let identity = request.identity
        guard !isClosed(window), !terminating else { return false }
        if let existing = entries.first(where: { $0.key != key && $0.value.identity == identity && $0.value.window != nil })?.value.window {
            existing.makeKeyAndOrderFront(nil)
            retire(window)
            return false
        }
        if identity.server.isEmpty && identity.session.isEmpty,
           let existing = entries.first(where: { $0.key != key && $0.value.window != nil })?.value.window {
            existing.makeKeyAndOrderFront(nil)
            retire(window)
            return false
        }
        let previous = entries[key]?.identity
        observeClose(window)
        entries[key] = Entry(window: window, identity: identity)
        pending.remove(identity)
        if identity.session.isEmpty, let previous, previous != identity {
            restoration.forget(previous)
        } else { restoration.remember(identity, replacing: previous) }
        if !identity.server.isEmpty || !identity.session.isEmpty {
            let blanks = entries.filter { $0.key != key && $0.value.identity.server.isEmpty && $0.value.identity.session.isEmpty }
            for entry in blanks.values { if let blank = entry.window { retire(blank) } }
        }
        observeGroups(); scheduleCapture()
        return true
    }
    private var joined: Set<ObjectIdentifier> = []
    private var open: ((SessionWindowRequest) -> Void)?
    private var attached: [ObjectIdentifier: Entry] = [:]
    private var groupObservers: [ObjectIdentifier: [NSKeyValueObservation]] = [:]
    private var captureScheduled = false
    private var arranging = false
    private weak var anchor: NSWindow?

    func defaultRequest() -> SessionWindowRequest {
        if let identity = restoration.sessions.first {
            return SessionWindowRequest(serverID: identity.server, sessionID: identity.session)
        }
        return SessionWindowRequest(sessionID: "")
    }

    func configure(_ window: NSWindow, serverID: String, sessionID: String,
                   sessions: [SessionExcerpt], open: @escaping (SessionWindowRequest) -> Void) {
        self.open = open
        self.serverID = serverID
        guard register(window, request: SessionWindowRequest(serverID: serverID, sessionID: sessionID)) else { return }
        prepareTabbing(window, request: SessionWindowRequest(serverID: serverID, sessionID: sessionID))
    }

    /// Runs synchronously when the scene's root view attaches to its window,
    /// before connection loading or deferred registry bookkeeping.
    func prepareTabbing(_ window: NSWindow, request: SessionWindowRequest) {
        guard !isClosed(window), !terminating else { return }
        observeClose(window)
        attached[ObjectIdentifier(window)] = Entry(window: window, identity: request.identity)
        arrange(window, identity: request.identity)
        restoreSelection()
        observeGroups()
        scheduleCapture()
    }

    private func arrange(_ window: NSWindow, identity: SessionIdentity) {
        window.tabbingIdentifier = AppIdentity.tabbingIdentifier
        window.tabbingMode = .preferred
        guard joined.insert(ObjectIdentifier(window)).inserted else { return }
        let requestedTarget = tabTargets.removeValue(forKey: identity)?.value
        let target: NSWindow?
        if let saved = restoration.group(containing: identity) {
            // Saved singletons must remain detached even if AppKit restores
            // them into another native group first.
            target = saved.sessions.filter { $0 != identity }.compactMap { member in
                attached.values.first { $0.identity == member && $0.window !== window }?.window
            }.first
            if let group = window.tabGroup, group.windows.count > 1 {
                let foreign = group.windows.contains { item in
                    guard let entry = attached[ObjectIdentifier(item)] else { return true }
                    return !saved.sessions.contains(entry.identity)
                }
                if foreign { window.moveTabToNewWindow(nil) }
            }
            if let target, let group = target.tabGroup {
                let rank = saved.sessions.firstIndex(of: identity) ?? 0
                let before = group.windows.filter { item in
                    guard item !== window, let entry = attached[ObjectIdentifier(item)],
                          let index = saved.sessions.firstIndex(of: entry.identity) else { return false }
                    return index < rank
                }.count
                group.insertWindow(window, at: before)
            }
        } else {
            target = requestedTarget ?? anchor
            if let target, target !== window, !(target.tabGroup?.windows ?? []).contains(window) {
                target.addTabbedWindow(window, ordered: .above)
            }
        }
        if anchor == nil { anchor = window }
    }

    private func restoreSelection() {
        for saved in restoration.groups {
            guard let selected = saved.selected, selectionsToRestore.contains(selected),
                  let window = attached.values.first(where: { $0.identity == selected })?.window,
                  let group = window.tabGroup else { continue }
            if group.selectedWindow !== window { group.selectedWindow = window }
            if saved.sessions.allSatisfy({ member in entries.values.contains { $0.identity == member } }) {
                selectionsToRestore.remove(selected)
            }
        }
    }

    private func observeGroups() {
        let groups = attached.values.compactMap { $0.window?.tabGroup }
        let keys = Set(groups.map(ObjectIdentifier.init))
        groupObservers = groupObservers.filter { keys.contains($0.key) }
        for group in groups where groupObservers[ObjectIdentifier(group)] == nil {
            groupObservers[ObjectIdentifier(group)] = [
                group.observe(\.windows) { [weak self] _, _ in
                    Task { @MainActor in self?.scheduleCapture() }
                },
                group.observe(\.selectedWindow) { [weak self] _, _ in
                    Task { @MainActor in self?.scheduleCapture() }
                }
            ]
        }
    }

    private func scheduleCapture() {
        guard !captureScheduled, !terminating else { return }
        captureScheduled = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.captureScheduled = false
            self.observeGroups()
            self.restoreSelection()
            self.captureLayout()
        }
    }

    func captureLayout() {
        guard !arranging, !terminating else { return }
        var seen = Set<ObjectIdentifier>()
        var groups: [WindowRestorationStore.Group] = []
        for identity in restoration.sessions {
            guard let window = entries.values.first(where: { $0.identity == identity })?.window,
                  seen.insert(ObjectIdentifier(window)).inserted else { continue }
            let native = window.tabGroup?.windows ?? [window]
            let members = native.compactMap { item -> SessionIdentity? in
                seen.insert(ObjectIdentifier(item))
                return entries[ObjectIdentifier(item)]?.identity
            }.filter { !$0.server.isEmpty && !$0.session.isEmpty }
            guard !members.isEmpty else { continue }
            let selected = window.tabGroup?.selectedWindow.flatMap { entries[ObjectIdentifier($0)]?.identity }
            groups.append(.init(sessions: members, selected: selected ?? members.first))
        }
        restoration.rememberGroups(groups)
    }

    @objc private func windowClosed(_ notification: Notification) {
        guard let window = notification.object as? NSWindow else { return }
        closed[ObjectIdentifier(window)] = WeakWindow(window)
        if let entry = entries.removeValue(forKey: ObjectIdentifier(window)), !terminating,
           !entries.values.contains(where: { $0.identity == entry.identity }) {
            restoration.forget(entry.identity)
        }
        tabTargets = tabTargets.filter { $0.value.value != nil && $0.value.value !== window }
        joined.remove(ObjectIdentifier(window))
        attached.removeValue(forKey: ObjectIdentifier(window))
        scheduleCapture()
        if anchor == window {
            anchor = restoration.sessions.compactMap { identity in
                entries.values.first { $0.identity == identity }?.window
            }.first
        }
        NotificationCenter.default.removeObserver(self, name: NSWindow.willCloseNotification, object: window)
    }

    func showLauncher(from window: NSWindow? = nil, open action: ((SessionWindowRequest) -> Void)? = nil) {
        guard let action = action ?? open else { return }
        let sourceWindow = window ?? NSApp.keyWindow ?? NSApp.mainWindow
        let source = sourceWindow.flatMap { entries[ObjectIdentifier($0)] }
        let server = source?.identity.server ?? "local-v2"
        openSession("", serverID: server.isEmpty ? "local-v2" : server, tabSource: sourceWindow, open: action)
    }

    func openSession(_ id: String, serverID: String? = nil, tabSource: NSWindow? = nil, open: (SessionWindowRequest) -> Void) {
        guard !terminating else { return }
        let identity = SessionIdentity(server: serverID ?? self.serverID, session: id)
        if let window = entries.values.first(where: { $0.identity == identity && $0.window != nil })?.window {
            if id.isEmpty, let source = tabSource, source !== window,
               !(source.tabGroup?.windows ?? []).contains(window) {
                source.addTabbedWindow(window, ordered: .above)
            }
            window.makeKeyAndOrderFront(nil)
        } else if pending.insert(identity).inserted {
            if let source = tabSource ?? NSApp.keyWindow ?? NSApp.mainWindow,
               entries[ObjectIdentifier(source)] != nil {
                tabTargets[identity] = WeakWindow(source)
            }
            open(SessionWindowRequest(serverID: identity.server, sessionID: id))
        }
    }
}

struct SessionWindowBridge: NSViewRepresentable {
    let registry: SessionWindowRegistry
    let serverID: String
    let sessionID: String
    let sessions: [SessionExcerpt]
    let colorScheme: ColorScheme?
    var tabStatus: SessionTabStatus = .idle
    var sessionTitle: String = "Kit"
    let open: (SessionWindowRequest) -> Void

    func makeNSView(context: Context) -> WindowProbe { WindowProbe() }
    func updateNSView(_ view: WindowProbe, context: Context) {
        view.configure = { window in
            SessionTabPresentation.update(window, title: sessionTitle, status: tabStatus)
            window.appearance = colorScheme.map { NSAppearance(named: $0 == .dark ? .darkAqua : .aqua)! }
            registry.configure(window, serverID: serverID, sessionID: sessionID, sessions: sessions, open: open)
        }
        view.scheduleConfiguration()
    }

    final class WindowProbe: NSView {
        var prepare: ((NSWindow) -> Void)? {
            didSet { if let window { prepare?(window) } }
        }
        var configure: ((NSWindow) -> Void)?
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let window { prepare?(window) }
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
