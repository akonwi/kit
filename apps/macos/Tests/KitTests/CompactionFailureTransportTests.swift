import Foundation
import Testing
@testable import Kit

/// Answers compact and configure requests with a failed compaction, whose
/// reason each test port chooses.
private final class CompactionFailureResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let url = request.url!
        let reason = switch url.port {
        case 19301: "policy_stop"
        case 19302: "summary_truncated"
        default: "disk_full"
        }
        let body = #"{"error":{"code":"compaction_failed","message":"context compaction failed","details":{"reason":"\#(reason)"}}}"#
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 422, httpVersion: nil,
                                                              headerFields: ["Content-Type": "application/json"])!,
                            cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

struct CompactionFailureTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CompactionFailureResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "instance",
                              serverID: "test", configuration: configuration)
    }

    @Test func aFailedCompactionCarriesItsReason() async throws {
        let compact = try client(19301)
        await #expect(throws: CompactionFailed(reason: .policyStop)) {
            _ = try await compact.compactSession("s", input: .init(operationId: "compact_a"))
        }
        do {
            _ = try await compact.compactSession("s", input: .init(operationId: "compact_a"))
        } catch {
            #expect(error.localizedDescription == "The provider stopped the summary under its usage policy.")
        }
    }

    @Test func aModelChangeThatNeededCompactionCarriesItsReason() async throws {
        let configure = try client(19302)
        await #expect(throws: CompactionFailed(reason: .summaryTruncated)) {
            _ = try await configure.configure("s", input: .init(expectedRevision: 1, model: "openai/gpt-5", thinkingLevel: nil))
        }
    }

    @Test func anUndeclaredReasonIsRefused() async throws {
        let compact = try client(19303)
        await #expect {
            _ = try await compact.compactSession("s", input: .init(operationId: "compact_a"))
        } throws: { error in
            !(error is CompactionFailed)
        }
    }
}
