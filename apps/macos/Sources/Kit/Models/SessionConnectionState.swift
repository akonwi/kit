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
