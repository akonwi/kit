import Foundation

/// A rename or delete a window has been asked to confirm for one session,
/// which needn't be the selected one.
struct SessionCatalogAction: Equatable {
    enum Kind: Equatable { case rename, delete }

    let identity: SessionIdentity
    let kind: Kind
}

/// One item of a session tab's context menu.
enum SessionTabMenuItem: Equatable {
    /// Opens a closed parent's tab again.
    case open
    case rename(enabled: Bool)
    case close
    case divider
    /// Delete…, which disposes of a temporary session.
    case delete(enabled: Bool)
    /// Why Rename… and Delete… are unavailable, as a disabled line.
    case unavailable(String)

    var title: String {
        switch self {
        case .open: "Open Tab"
        case .rename: "Rename…"
        case .close: "Close Tab"
        case .divider: ""
        case .delete: "Delete…"
        case .unavailable(let reason): reason
        }
    }
}

/// The context menus of session tabs and closed-parent rows: Rename… and
/// Close Tab, then Delete… apart from them. A launcher has no session to
/// rename or delete.
enum SessionTabMenu {
    /// A session tab's menu.
    static func items(launcher: Bool, unavailable: String?) -> [SessionTabMenuItem] {
        guard !launcher else { return [.close] }
        return [.rename(enabled: unavailable == nil), .close, .divider, .delete(enabled: unavailable == nil)]
            + (unavailable.map { [.unavailable($0)] } ?? [])
    }

    /// The available renames and deletes of a menu, as assistive technology
    /// actions, by name.
    static func accessibilityActions(_ items: [SessionTabMenuItem]) -> [(name: String, kind: SessionCatalogAction.Kind)] {
        items.compactMap { item in
            switch item {
            case .rename(true): ("Rename", .rename)
            case .delete(true): ("Delete", .delete)
            default: nil
            }
        }
    }

    /// A closed parent's menu.
    static func closedParentItems(unavailable: String?) -> [SessionTabMenuItem] {
        [.open, .divider, .rename(enabled: unavailable == nil), .delete(enabled: unavailable == nil)]
            + (unavailable.map { [.unavailable($0)] } ?? [])
    }

    /// Why a session can't be renamed or deleted from its tab: what its
    /// server needs, a connection still being made, or a session its
    /// server doesn't know.
    static func unavailableReason(status: ServerStatus?, demo: Bool, sessionKnown: Bool,
                                  sessionUnavailable: Bool) -> String? {
        guard let status else { return "Server isn’t saved" }
        if let issue = status.sessionIssue { return issue }
        switch status {
        case .connecting: return "Connecting…"
        case .reconnecting: return "Reconnecting…"
        default: break
        }
        if demo { return "Unavailable for this connection" }
        if sessionUnavailable { return "Session unavailable" }
        if !sessionKnown { return "Loading session…" }
        return nil
    }
}
