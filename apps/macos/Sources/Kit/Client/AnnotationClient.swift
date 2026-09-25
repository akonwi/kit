import Foundation

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
