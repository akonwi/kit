import Foundation

/// The server rejected annotation evidence from an out-of-date file or diff view.
public enum AnnotationEvidenceConflict: String, LocalizedError {
    case staleFile = "stale_file"
    case staleWorkspace = "stale_workspace"
    case staleTarget = "stale_target"

    public var errorDescription: String? {
        switch self {
        case .staleFile: "The file changed. Refresh it and select the range again."
        case .staleWorkspace: "The workspace changed. Reopen the file or diff and select the range again."
        case .staleTarget: "The diff changed. Refresh it and select the range again."
        }
    }
}

public struct AnnotationEvidenceConflictEnvelope: Decodable {
    public let error: Detail
    public struct Detail: Decodable {
        public let code: String

        public init(code: String) {
            self.code = code
        }
    }

    public init(error: Detail) {
        self.error = error
    }
}

public protocol AnnotationClient: SessionClient {
    func annotations(_ session: String) async throws -> [FileAnnotation]
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation
    func deleteAnnotation(_ session: String, id: UInt64) async throws
}

extension ServerClient: AnnotationClient {
    public func annotations(_ session: String) async throws -> [FileAnnotation] {
        try await resolve().annotations(session)
    }
    public func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation {
        try await resolve().createAnnotation(session, input: input)
    }
    public func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation {
        try await resolve().updateAnnotation(session, input: input)
    }
    public func deleteAnnotation(_ session: String, id: UInt64) async throws {
        try await resolve().deleteAnnotation(session, id: id)
    }
}
