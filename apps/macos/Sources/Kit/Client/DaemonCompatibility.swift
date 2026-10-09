import Foundation

/// Match the server's protocol-45 stable-release compatibility promise after
/// authenticating health and verifying it against the discovered registry.
enum DaemonCompatibility {
    enum Mismatch: String {
        case daemonProtocolOlder = "daemon_protocol_older"
        case clientProtocolOlder = "client_protocol_older"
        case releaseMismatch = "release_mismatch"

        var recovery: String {
            switch self {
            case .daemonProtocolOlder: "Update the server to a compatible protocol."
            case .clientProtocolOlder: "Update the app to a compatible protocol."
            case .releaseMismatch: "Use compatible stable app and server releases."
            }
        }
    }

    static func mismatch(clientVersion: String, daemonVersion: String, clientProtocol: Int, daemonProtocol: Int) -> Mismatch? {
        if daemonProtocol < clientProtocol { return .daemonProtocolOlder }
        if daemonProtocol > clientProtocol { return .clientProtocolOlder }
        if clientVersion == daemonVersion { return nil }
        if clientProtocol == 45 && stableRelease(clientVersion, atLeast: [0, 42, 0]) &&
            stableRelease(daemonVersion, atLeast: [0, 42, 0]) { return nil }
        return .releaseMismatch
    }

    static func accepts(clientVersion: String, daemonVersion: String, clientProtocol: Int, daemonProtocol: Int) -> Bool {
        mismatch(clientVersion: clientVersion, daemonVersion: daemonVersion,
                 clientProtocol: clientProtocol, daemonProtocol: daemonProtocol) == nil
    }

    static func clientRelease(_ bundle: Bundle = .main) -> String {
        // The app's own version is independent of the Kit protocol release whose
        // stable compatibility promise it implements. Development builds use dev.
        bundle.object(forInfoDictionaryKey: "KitClientRelease") as? String ?? "dev"
    }

    private static func stableRelease(_ version: String, atLeast minimum: [UInt64]) -> Bool {
        guard !version.isEmpty, version.utf8.count <= 64 else { return false }
        let parts = version.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 3 else { return false }
        var numbers: [UInt64] = []
        for part in parts {
            guard !part.isEmpty, (part.count == 1 || part.first != "0"),
                  part.utf8.allSatisfy({ (48...57).contains($0) }), let number = UInt64(part) else { return false }
            numbers.append(number)
        }
        return numbers.lexicographicallyPrecedes(minimum) == false
    }
}
