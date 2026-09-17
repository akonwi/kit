import Foundation
import Testing
@testable import Kit

private final class ComposerResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Protocol-Version") == String(kitWireVersion))
        let body: String, status: Int
        switch url.path {
        case "/v1/sessions/session_test/files":
            #expect(request.httpMethod == "GET")
            if url.port == 19303 { #expect(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems == [URLQueryItem(name: "refresh", value: "true")]) }
            status = 200
            body = url.port == 19302
                ? #"{"sessionId":"session_test","cwd":"/tmp","truncated":true,"entries":[{"path":"../outside"}]}"#
                : #"{"sessionId":"session_test","cwd":"/tmp","truncated":true,"entries":[{"path":"src/","isDir":true},{"path":"src/main.swift"},{"path":"README.md"}]}"#
        case "/v1/sessions/session_test/attachments":
            #expect(request.httpMethod == "POST")
            let type = request.value(forHTTPHeaderField: "Content-Type") ?? ""
            #expect(type.hasPrefix("multipart/form-data; boundary=Kit-"))
            var data = request.httpBody ?? Data()
            if let stream = request.httpBodyStream {
                stream.open(); defer { stream.close() }
                var buffer = [UInt8](repeating: 0, count: 1024)
                while stream.hasBytesAvailable {
                    let count = stream.read(&buffer, maxLength: buffer.count)
                    if count <= 0 { break }
                    data.append(contentsOf: buffer.prefix(count))
                }
            }
            let encoded = String(decoding: data, as: UTF8.self)
            #expect(encoded.contains("name=\"file\"; filename=\"note.txt\"\r\nContent-Type: application/octet-stream\r\n\r\nhello\r\n--Kit-"))
            status = 201
            body = #"{"id":"attachment_test","sessionId":"session_test","filename":"note.txt","mediaType":"text/plain","size":5,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-09-13T00:00:00Z"}"#
        default:
            Issue.record("Unexpected composer request: \(url.path)")
            status = 404; body = "{}"
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil,
            headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct ComposerTransportTests {
    private func client(_ port: Int = 19301) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ComposerResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test", serverID: "test", configuration: configuration)
    }
    @Test func fileIndexUsesTheSessionHostAndRejectsEscapingPaths() async throws {
        let index = try await client().fileIndex("session_test", refresh: false)
        #expect(index.truncated)
        #expect(index.cwd == "/tmp")
        #expect(index.notice == "File index is incomplete; some paths may be missing.")
        #expect(try await client().files("session_test") == ["src/", "src/main.swift", "README.md"])
        await #expect(throws: ClientError.self) { try await client(19302).files("session_test") }
        #expect(try await client(19303).files("session_test", refresh: true) == ["src/", "src/main.swift", "README.md"])
    }
    @Test func attachmentUploadUsesMultipartAndReturnsTheServerIdentity() async throws {
        let info = try await client().upload("session_test", filename: "note.txt", data: Data("hello".utf8))
        #expect(info.id == "attachment_test")
        #expect(info.filename == "note.txt")
        #expect(info.size == 5)
    }
}
