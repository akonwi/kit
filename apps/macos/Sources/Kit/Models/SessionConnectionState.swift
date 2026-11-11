import Foundation

enum SessionConnectionState: Equatable {
    case unavailable
    case disconnected
    case loading
    case connected
    case retrying(message: String, delay: Int)
    case authenticationFailed
    case failed(String)

    var label: String {
        switch self {
        case .unavailable: "Session unavailable"
        case .disconnected: "Disconnected"
        case .loading: "Connecting…"
        case .connected: "Connected"
        case .retrying(let message, let delay): "\(message) · Retrying in \(delay)s"
        case .authenticationFailed: "Authentication failed · Reconnect to refresh credentials"
        case .failed(let message): message
        }
    }
}

/// How far a tab's session has opened.
enum SessionOpening: Equatable {
    /// The server has sent the session.
    case open
    /// Waiting for the server to send the session.
    case loading
    /// Opening stopped or is waiting to retry, for this reason.
    case failed(String)
    /// The server has no such session.
    case missing
}
