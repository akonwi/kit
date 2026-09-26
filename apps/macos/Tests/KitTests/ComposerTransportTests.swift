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
        case "/v1/sessions/session_test/messages":
            #expect(request.httpMethod == "GET")
            let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            #expect(query.contains(URLQueryItem(name: "limit", value: "100")))
            #expect(query.contains(URLQueryItem(name: "role", value: "user")))
            status = 200
            if url.port == 19311 {
                body = query.contains(URLQueryItem(name: "before", value: "2"))
                    ? #"{"SessionID":"session_test","Messages":[{"id":"message_1","turnId":"turn_1","sequence":1,"role":"user","content":[{"kind":"text","text":"older"}],"createdAt":"2026-09-13T00:00:00Z"}],"HasMore":false}"#
                    : #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"text","text":"newer"}],"createdAt":"2026-09-13T00:00:00Z"}],"nextCursor":"2","HasMore":true}"#
            } else if url.port == 19304 {
                body = #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"text","text":"repeat"}],"createdAt":"2026-09-13T00:00:00Z"}],"nextCursor":"1","HasMore":true}"#
            } else if url.port == 19306 {
                body = #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"thinking","text":"hidden"}],"stopReason":"stop","createdAt":"2026-09-13T00:00:00Z"}],"HasMore":false}"#
            } else if url.port == 19307 {
                body = #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"image","mediaType":"text/plain"}],"createdAt":"2026-09-13T00:00:00Z"}],"HasMore":false}"#
            } else if url.port == 19308 {
                body = #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"file","filename":"  ","mediaType":"application/pdf"}],"createdAt":"2026-09-13T00:00:00Z"}],"HasMore":false}"#
            } else if url.port == 19309 {
                body = #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"text","text":"with image"},{"kind":"image","mediaType":"image/png; name=\"a;b\""}],"createdAt":"2026-09-13T00:00:00Z"}],"HasMore":false}"#
            } else if url.port == 19310 {
                body = #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"image","mediaType":"image/png; name=\"a\nb\""}],"createdAt":"2026-09-13T00:00:00Z"}],"HasMore":false}"#
            } else {
                body = #"{"SessionID":"session_test","Messages":[{"id":"message_2","turnId":"turn_2","sequence":2,"role":"user","content":[{"kind":"text","text":"repeat"}],"createdAt":"2026-09-13T00:00:00Z"},{"id":"message_1","turnId":"turn_1","sequence":1,"role":"user","content":[{"kind":"text","text":"repeat"},{"kind":"text","text":" older"}],"createdAt":"2026-09-13T00:00:00Z"}],"HasMore":false}"#
            }
        case "/v1/sessions/session_test/bash-history":
            #expect(request.httpMethod == "GET")
            #expect(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems == [URLQueryItem(name: "limit", value: "10")])
            status = 200
            body = url.port == 19305
                ? #"{"sessionId":"session_test","hasMore":true,"nextCursor":"1"}"#
                : #"{"sessionId":"session_test","entries":[{"id":"bash_11111111111111111111111111111111","sequence":1,"command":"echo secret","status":"completed","excludeFromContext":true,"startedAt":"2026-09-13T00:00:00Z","completedAt":"2026-09-13T00:00:01Z"}],"hasMore":false}"#
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
    @Test func composerHistoryUsesDurableFilteredEndpoints() async throws {
        let history = try await client().messageHistory("session_test", before: nil)
        #expect(history.entries == [
            .init(id: "message_2", text: "repeat"),
            .init(id: "message_1", text: "repeat\n\n older")
        ])
        let first = try await client(19311).messageHistory("session_test", before: nil)
        #expect(first.entries == [.init(id: "message_2", text: "newer")])
        #expect(first.hasMore)
        let older = try await client(19311).messageHistory("session_test", before: first.nextCursor)
        #expect(older.entries == [.init(id: "message_1", text: "older")])
        #expect(!older.hasMore)
        let bash = try await client().bashHistory("session_test", before: nil, limit: 10)
        #expect(bash.entries == [.init(id: "bash_11111111111111111111111111111111", sequence: 1, command: "echo secret", excluded: true)])
        #expect(!bash.hasMore)
        await #expect(throws: ClientError.self) { try await client(19304).messageHistory("session_test", before: nil) }
        await #expect(throws: ClientError.self) { try await client(19305).bashHistory("session_test", before: nil, limit: 10) }
        await #expect(throws: ClientError.self) { try await client(19306).messageHistory("session_test", before: nil) }
        await #expect(throws: ClientError.self) { try await client(19307).messageHistory("session_test", before: nil) }
        await #expect(throws: ClientError.self) { try await client(19308).messageHistory("session_test", before: nil) }
        #expect(try await client(19309).messageHistory("session_test", before: nil).entries == [.init(id: "message_2", text: "with image")])
        await #expect(throws: ClientError.self) { try await client(19310).messageHistory("session_test", before: nil) }
    }
    @Test func attachmentUploadUsesMultipartAndReturnsTheServerIdentity() async throws {
        let info = try await client().upload("session_test", filename: "note.txt", data: Data("hello".utf8))
        #expect(info.id == "attachment_test")
        #expect(info.filename == "note.txt")
        #expect(info.size == 5)
    }
}
