import Foundation

/// Why a session's workspace file can't be shown.
public enum WorkspaceFileError: LocalizedError {
    case unavailable, response(Int)
    public var errorDescription: String? {
        switch self {
        case .unavailable: "Workspace unavailable."
        case .response(415): "Binary files cannot be previewed."
        case .response: "Couldn’t read this file."
        }
    }
}
