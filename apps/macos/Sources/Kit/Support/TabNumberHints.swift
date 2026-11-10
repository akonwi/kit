import AppKit
import SwiftUI

/// When a window's session tabs show which ⌘1–⌘9 shortcut reaches them: once
/// ⌘ alone has been held for `delay`, until it is released. A key pressed
/// before then cancels them, so ⌘ shortcuts don't flash numbers; once shown,
/// they stay through ⌘ shortcuts until ⌘ is released.
///
/// ⌘ held alone for `holdDelay`, with no key or click in between, is also
/// a hold, which peeks a hidden sidebar so its numbers show. A key or a
/// click ends the hold, as does releasing ⌘; hints outlast it.
struct TabNumberHintTrigger: Equatable {
    static let delay: Duration = .milliseconds(100)
    /// How long ⌘ alone is held before it is a hold: longer than for
    /// hints, as a sidebar sliding over the tiles is more to see.
    static let holdDelay: Duration = .milliseconds(300)
    /// The modifiers that can change what a numbered key does.
    static let modifiers: NSEvent.ModifierFlags = [.command, .shift, .option, .control]

    private(set) var shown = false
    /// The show waiting for the delay. A newer one replaces it.
    private(set) var pending: Int?
    /// Whether ⌘ alone has been held for `holdDelay`, with nothing pressed.
    private(set) var holding = false
    /// The hold waiting for `holdDelay`.
    private(set) var pendingHold: Int?
    private var held: NSEvent.ModifierFlags = []
    private var generation = 0

    /// Records the window's modifier keys, returning the show to make after
    /// the delay when ⌘ alone has just been pressed.
    @discardableResult
    mutating func modifiersChanged(_ flags: NSEvent.ModifierFlags) -> Int? {
        let now = flags.intersection(Self.modifiers)
        defer { held = now }
        guard now == .command else {
            shown = false
            pending = nil
            endHold()
            return nil
        }
        // Caps Lock or Fn changing under ⌘ leaves hints as they are.
        guard held != .command else { return nil }
        generation += 1
        pending = generation
        pendingHold = generation
        return generation
    }

    /// Modifiers seen some other way than their own change: on another
    /// event, or read after a menu or drag tracked its own events. They can
    /// only end hints; showing waits for ⌘ to be pressed.
    mutating func modifiersSeen(_ flags: NSEvent.ModifierFlags) {
        guard flags.intersection(Self.modifiers) != .command else { return }
        modifiersChanged(flags)
    }

    /// A key pressed while ⌘ is held. Before the delay, it's a shortcut.
    /// It ends a hold either way.
    mutating func keyPressed() {
        pending = nil
        endHold()
    }

    /// A click while ⌘ is held. It leaves hints as they are and ends a hold.
    mutating func mousePressed() {
        endHold()
    }

    mutating func delayElapsed(_ generation: Int) {
        guard pending == generation else { return }
        pending = nil
        shown = true
    }

    mutating func holdDelayElapsed(_ generation: Int) {
        guard pendingHold == generation else { return }
        pendingHold = nil
        holding = true
    }

    private mutating func endHold() {
        pendingHold = nil
        holding = false
    }

    /// The window stopped being key. Returning with ⌘ still down shows
    /// nothing until ⌘ is pressed again.
    mutating func windowResignedKey() {
        shown = false
        pending = nil
        endHold()
        held = []
    }
}

/// Watches one window's modifier keys for its tab number hints. Only the
/// key window receives these events, so other windows never show hints.
@MainActor @Observable
final class TabNumberHints {
    private(set) var shown = false
    /// Whether ⌘ alone has been held long enough to peek a hidden sidebar,
    /// with no key or click since.
    private(set) var holding = false
    /// How long ⌘ alone is held before hints show.
    @ObservationIgnored var delay: Duration = TabNumberHintTrigger.delay
    /// How long ⌘ alone is held before it is a hold.
    @ObservationIgnored var holdDelay: Duration = TabNumberHintTrigger.holdDelay
    /// Called as a hold begins and ends.
    @ObservationIgnored var onHold: @MainActor (Bool) -> Void = { _ in }
    /// The modifiers held now. Menus and drags track their own events, so
    /// ⌘ can be released without this window seeing it.
    @ObservationIgnored var liveModifiers: @MainActor () -> NSEvent.ModifierFlags = { NSEvent.modifierFlags }
    @ObservationIgnored private var trigger = TabNumberHintTrigger()
    @ObservationIgnored private weak var window: NSWindow?
    @ObservationIgnored nonisolated(unsafe) private var monitor: Any?
    @ObservationIgnored nonisolated(unsafe) private var menuObserver: (any NSObjectProtocol)?
    @ObservationIgnored private var reveal: Task<Void, Never>?
    @ObservationIgnored private var hold: Task<Void, Never>?

    init() {}

    /// A window's model owns this and is released on the main thread, so
    /// removing the monitor here is safe if the window never closed.
    deinit {
        if let monitor { NSEvent.removeMonitor(monitor) }
        if let menuObserver { NotificationCenter.default.removeObserver(menuObserver) }
    }

    /// Starts watching the window's events. Hints never consume them.
    func attach(to window: NSWindow) {
        guard self.window !== window || monitor == nil else { return }
        detach()
        self.window = window
        let events: NSEvent.EventTypeMask = [.flagsChanged, .keyDown, .leftMouseDown, .leftMouseUp, .rightMouseDown, .scrollWheel]
        monitor = NSEvent.addLocalMonitorForEvents(matching: events) { [weak self] event in
            MainActor.assumeIsolated {
                guard let self, let window = self.window, event.window === window else { return }
                if event.type == .flagsChanged {
                    self.modifiersChanged(event.modifierFlags)
                } else {
                    // Every event carries the modifiers held when it happened.
                    self.modifiersSeen(event.modifierFlags)
                    if event.type == .keyDown { self.keyPressed() }
                    if event.type == .leftMouseDown || event.type == .rightMouseDown { self.mousePressed() }
                }
            }
            return event
        }
        // A menu may have seen ⌘ released while it was open.
        menuObserver = NotificationCenter.default.addObserver(forName: NSMenu.didEndTrackingNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                self.modifiersSeen(self.liveModifiers())
            }
        }
    }

    func detach() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        if let menuObserver { NotificationCenter.default.removeObserver(menuObserver) }
        monitor = nil
        menuObserver = nil
        window = nil
        windowResignedKey()
    }

    func modifiersChanged(_ flags: NSEvent.ModifierFlags) {
        if let generation = trigger.modifiersChanged(flags) {
            reveal?.cancel()
            reveal = Task { [weak self, delay] in
                try? await Task.sleep(for: delay)
                guard !Task.isCancelled, let self else { return }
                // ⌘ may have been released where this window couldn't see.
                self.trigger.modifiersSeen(self.liveModifiers())
                self.trigger.delayElapsed(generation)
                self.sync()
            }
            hold?.cancel()
            hold = Task { [weak self, holdDelay] in
                try? await Task.sleep(for: holdDelay)
                guard !Task.isCancelled, let self else { return }
                self.trigger.modifiersSeen(self.liveModifiers())
                self.trigger.holdDelayElapsed(generation)
                self.sync()
            }
        }
        sync()
    }

    func modifiersSeen(_ flags: NSEvent.ModifierFlags) {
        trigger.modifiersSeen(flags)
        sync()
    }

    func keyPressed() {
        trigger.keyPressed()
        sync()
    }

    func mousePressed() {
        trigger.mousePressed()
        sync()
    }

    func windowResignedKey() {
        trigger.windowResignedKey()
        sync()
    }

    /// Hints fade in, unless Reduce Motion is on, and vanish at once.
    private func sync() {
        if trigger.pending == nil {
            reveal?.cancel()
            reveal = nil
        }
        if trigger.pendingHold == nil {
            hold?.cancel()
            hold = nil
        }
        if trigger.holding != holding {
            holding = trigger.holding
            onHold(holding)
        }
        guard trigger.shown != shown else { return }
        if trigger.shown {
            let reduceMotion = NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
            withAnimation(reduceMotion ? nil : .easeOut(duration: 0.12)) { shown = true }
        } else {
            var transaction = Transaction()
            transaction.disablesAnimations = true
            withTransaction(transaction) { shown = false }
        }
    }
}

/// The number ⌘1–⌘9 gives each row they reach: tabs by ID, closed parents
/// by tree key.
struct TabShortcutNumbers: Equatable {
    static let none = TabShortcutNumbers()

    var tabs: [UUID: Int] = [:]
    var closedParents: [String: Int] = [:]

    @MainActor func number(for content: SessionTreeRow.Content) -> Int? {
        switch content {
        case .tab(let tab): tabs[tab.id]
        case .closedParent(let key, _): closedParents[key]
        }
    }
}

/// A tab's ⌘ number, shown in its trailing action slot while ⌘ is held.
struct TabNumberHint: View {
    @Environment(\.mica) private var theme
    let number: Int

    static let size: CGFloat = 16

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 3)
        Text("\(number)")
            .font(.kit(size: 11, weight: .medium, design: .monospaced))
            .foregroundStyle(theme.text)
            .frame(width: Self.size, height: Self.size)
            .background(theme.surface, in: shape)
            .overlay(shape.strokeBorder(theme.strongBorder))
            .transition(.opacity)
            .allowsHitTesting(false)
            // The Session menu already names each tab's shortcut.
            .accessibilityHidden(true)
    }
}
