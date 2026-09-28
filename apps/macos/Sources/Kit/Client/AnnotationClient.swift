import Foundation

/// The server rejected annotation evidence from an out-of-date file or diff view.
enum AnnotationEvidenceConflict: String, LocalizedError {
    case staleFile = "stale_file"
    case staleWorkspace = "stale_workspace"
    case staleTarget = "stale_target"

    var errorDescription: String? {
        switch self {
        case .staleFile: "The file changed. Refresh it and select the range again."
        case .staleWorkspace: "The workspace changed. Reopen the file or diff and select the range again."
        case .staleTarget: "The diff changed. Refresh it and select the range again."
        }
    }
}

struct AnnotationEvidenceConflictEnvelope: Decodable {
    let error: Detail
    struct Detail: Decodable { let code: String }
}

protocol AnnotationClient: SessionClient {
    func annotations(_ session: String) async throws -> [FileAnnotation]
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation
    func deleteAnnotation(_ session: String, id: UInt64) async throws
}

extension LocalClient: AnnotationClient {
    func annotations(_ session: String) async throws -> [FileAnnotation] {
        try await HTTPClient.local().annotations(session)
    }
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation {
        try await HTTPClient.local().createAnnotation(session, input: input)
    }
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation {
        try await HTTPClient.local().updateAnnotation(session, input: input)
    }
    func deleteAnnotation(_ session: String, id: UInt64) async throws {
        try await HTTPClient.local().deleteAnnotation(session, id: id)
    }
}
