import AppKit
import SwiftUI
import GhosttyKit
import WebKit

/// Owns the AppKit views behind native panes so they outlive SwiftUI's view
/// identity: tab switches, moves between tiles, zoom and session switches
/// detach and reattach the same instance instead of recreating it.
@MainActor final class NativeViews {
    static let shared = NativeViews()
    fileprivate var views: [UUID: NSView] = [:]
    fileprivate var hosts: [UUID: [NativePaneHost.ContainerView]] = [:]
    weak var model: AppModel?

    func view(for pane: Pane) -> NSView {
        if let view = views[pane.id] { return view }
        let view: NSView
        switch pane.kind {
        case .terminal:
            let shell = ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/zsh"
            if let terminal = TerminalSurfaceView(
                command: "\(shell) -l", workingDirectory: Fixtures.workingDirectory, environment: [:]) {
                let id = pane.id
                terminal.onFocusChange = { [weak self] focused in
                    if focused { self?.model?.nativeFocused(pane: id) }
                }
                terminal.hostDelegate = TerminalHost.shared
                view = terminal
            } else {
                view = NSTextField(labelWithString: "libghostty surface failed")
            }
        case .browser(let url):
            let web = FocusReportingWebView(frame: .zero, configuration: WKWebViewConfiguration())
            web.paneID = pane.id
            web.load(URLRequest(url: url))
            view = web
        default:
            view = NSView()
        }
        views[pane.id] = view
        return view
    }

    func existing(_ pane: UUID) -> NSView? { views[pane] }

    func release(_ pane: UUID) {
        hosts[pane] = nil
        guard let view = views.removeValue(forKey: pane) else { return }
        if let terminal = view as? TerminalSurfaceView { terminal.onFocusChange = nil }
        view.removeFromSuperview()
    }
}

final class FocusReportingWebView: WKWebView {
    var paneID: UUID?
    override func becomeFirstResponder() -> Bool {
        let ok = super.becomeFirstResponder()
        if ok, let paneID { NativeViews.shared.model?.nativeFocused(pane: paneID) }
        return ok
    }
}

final class TerminalHost: TerminalSurfaceHostDelegate {
    static let shared = TerminalHost()
    func surfaceDidClose(_ view: TerminalSurfaceView) {}
    func surface(_ view: TerminalSurfaceView, didReceiveDrop text: String) { view.sendText(text + " ") }
}

/// Hosts a pane's persistent AppKit view inside the SwiftUI tile.
///
/// SwiftUI may briefly keep two hosts for one pane alive (zoom, moving a tab
/// to another tile) and update or dismantle them in any order. The view always
/// lives in the newest live host, and returns to a surviving host when the one
/// holding it is dismantled.
struct NativePaneHost: NSViewRepresentable {
    let pane: Pane

    func makeNSView(context: Context) -> ContainerView {
        let container = ContainerView()
        container.wantsLayer = true
        return container
    }

    func updateNSView(_ container: ContainerView, context: Context) {
        NativeViews.shared.register(container, for: pane)
    }

    static func dismantleNSView(_ container: ContainerView, coordinator: ()) {
        NativeViews.shared.unregister(container)
    }

    final class ContainerView: NSView {
        var pane: UUID?
        override var isFlipped: Bool { true }
    }
}

extension NativeViews {
    func register(_ container: NativePaneHost.ContainerView, for pane: Pane) {
        if container.pane != pane.id {
            unregister(container)
            container.pane = pane.id
            hosts[pane.id, default: []].append(container)
        }
        place(pane.id)
    }

    func unregister(_ container: NativePaneHost.ContainerView) {
        guard let pane = container.pane else { return }
        container.pane = nil
        hosts[pane]?.removeAll { $0 === container }
        spikeLog("dismantle host; \(hosts[pane]?.count ?? 0) left for pane")
        place(pane)
    }

    /// Puts the pane's view in its newest live host.
    private func place(_ pane: UUID) {
        guard let view = views[pane] ?? model?.pane(pane).map({ self.view(for: $0) }) else { return }
        guard let host = hosts[pane]?.last else {
            view.removeFromSuperview()
            return
        }
        guard view.superview !== host else { return }
        view.removeFromSuperview()
        view.frame = host.bounds
        view.autoresizingMask = [.width, .height]
        host.addSubview(view)
        spikeLog("attach \(model?.pane(pane)?.title ?? "?") to host \(hosts[pane]!.count)")
    }
}

/// Routes libghostty surface actions (cursor shape, links, titles).
final class SurfaceRouter: GhosttyRuntimeDelegate {
    static let shared = SurfaceRouter()
    func surface(_ view: TerminalSurfaceView, didReceive event: SurfaceEvent) {
        switch event {
        case .mouseShape(let shape):
            switch shape {
            case GHOSTTY_MOUSE_SHAPE_TEXT: NSCursor.iBeam.set()
            case GHOSTTY_MOUSE_SHAPE_POINTER: NSCursor.pointingHand.set()
            default: NSCursor.arrow.set()
            }
        case .openURL(let string):
            if let url = URL(string: string), url.scheme != nil { NSWorkspace.shared.open(url) }
        default:
            break
        }
    }
}
