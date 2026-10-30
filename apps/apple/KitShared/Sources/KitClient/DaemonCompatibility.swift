import Foundation

/// Match the Go client's compatibility policy after authenticating health and
/// verifying it against the discovered registry: the app and server are
/// compatible exactly when they speak the same session protocol.
public enum DaemonCompatibility {
    public enum Mismatch: String, Sendable {
        case daemonProtocolOlder = "daemon_protocol_older"
        case clientProtocolOlder = "client_protocol_older"

        public var recovery: String {
            switch self {
            case .daemonProtocolOlder: "Update the server to a compatible protocol."
            case .clientProtocolOlder: "Update the app to a compatible protocol."
            }
        }
    }

    public static func mismatch(clientProtocol: Int, daemonProtocol: Int) -> Mismatch? {
        if daemonProtocol < clientProtocol { return .daemonProtocolOlder }
        if daemonProtocol > clientProtocol { return .clientProtocolOlder }
        return nil
    }
}
