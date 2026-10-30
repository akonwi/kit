import Foundation

/// Session activity shown on a session tab.
enum SessionTabStatus: String, Decodable, Sendable {
    case idle, running, awaitingResponse

    var label: String? {
        switch self {
        case .idle: nil
        case .running: "Working"
        case .awaitingResponse: "Awaiting response"
        }
    }
}
