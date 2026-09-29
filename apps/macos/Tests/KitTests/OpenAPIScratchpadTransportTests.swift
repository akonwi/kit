import Foundation
import Testing
@testable import Kit

private final class ScratchpadOpenAPIResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let url = request.url!
        #expect(url.path == "/v1/sessions/s/scratchpad")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "instance")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Protocol-Version") == "41")
        var status = 200
        var headers = ["Content-Type": "application/json"]
        let body: String
        switch url.port {
        case 19201:
            #expect(request.httpMethod == "GET")
            body = #"{"ownerSessionId":"root","content":"shared","revision":"1","updatedAt":"2026-03-23T12:34:56Z"}"#
        case 19202:
            #expect(request.httpMethod == "PUT")
            let payload = Self.bodyData(request).flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: String] }
            #expect(payload == ["content": "changed", "expectedRevision": "1"])
            body = #"{"ownerSessionId":"root","content":"changed","revision":"2","updatedAt":"2026-03-23T12:34:57Z"}"#
        case 19203:
            status = 409
            body = #"{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{"scratchpad":{"ownerSessionId":"root","content":"remote","revision":"2","updatedAt":"2026-03-23T12:34:57Z"}}}}"#
        case 19204:
            body = #"{"ownerSessionId":"root","content":"shared","revision":"1","updatedAt":"2026-03-23T12:34:56Z","unexpected":true}"#
        case 19205:
            let content = String(repeating: #"\u003c"#, count: 65_536)
            body = #"{"ownerSessionId":"root","content":"\#(content)","revision":"1","updatedAt":"2026-03-23T12:34:56Z"}"#
        default:
            headers["Content-Length"] = "524289"
            body = ""
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: headers)!, cacheStoragePolicy: .notAllowed)
        if !body.isEmpty { client?.urlProtocol(self, didLoad: Data(body.utf8)) }
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private static func bodyData(_ request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open(); defer { stream.close() }
        var result = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            result.append(buffer, count: count)
        }
        return result
    }
}

struct OpenAPIScratchpadTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ScratchpadOpenAPIResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "instance", serverID: "test", configuration: configuration)
    }

    @Test func generatedClientReadsAndUpdatesScratchpad() async throws {
        let read = try await client(19201).scratchpad(session: "s")
        #expect(read.content == "shared")
        let updated = try await client(19202).updateScratchpad(session: "s", content: "changed", expectedRevision: 1)
        #expect(updated.revision == 2)
    }

    @Test func generatedClientPreservesConflictDetails() async throws {
        do {
            _ = try await client(19203).updateScratchpad(session: "s", content: "changed", expectedRevision: 1)
            Issue.record("Expected conflict")
        } catch ScratchpadFailure.conflict(let current) {
            #expect(current.content == "remote")
            #expect(current.revision == 2)
        }
    }

    @Test func generatedClientAcceptsMaximumEscapedContent() async throws {
        let result = try await client(19205).scratchpad(session: "s")
        #expect(result.content == String(repeating: "<", count: 65_536))
    }

    @Test func generatedClientRejectsDriftAndOversizedResponses() async throws {
        await #expect(throws: (any Error).self) { try await client(19204).scratchpad(session: "s") }
        await #expect(throws: (any Error).self) { try await client(19206).scratchpad(session: "s") }
    }
}
