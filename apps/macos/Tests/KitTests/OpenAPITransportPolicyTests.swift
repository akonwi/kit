import Foundation
import Network
import HTTPTypes
import OpenAPIRuntime
import Testing
@testable import Kit

/// Serves transport-policy fixtures and records every URL the session loads.
private final class TransportPolicyResponse: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var requested: [String] = []
    private static let lock = NSLock()

    static func record(_ url: URL) { lock.withLock { requested.append(url.absoluteString) } }
    static func requests() -> [String] { lock.withLock { requested } }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let url = request.url!
        Self.record(url)
        switch (url.port, url.path) {
        case (19302, _):
            // No Content-Length: the complete body is buffered while streaming.
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            let chunk = Data(repeating: 0x20, count: 64 * 1024)
            for _ in 0..<9 { client?.urlProtocol(self, didLoad: chunk) }
            client?.urlProtocolDidFinishLoading(self)
        default:
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: Data("{}".utf8))
            client?.urlProtocolDidFinishLoading(self)
        }
    }

    override func stopLoading() {}
}

@Suite(.serialized)
struct OpenAPITransportPolicyTests {
    private func transport(_ port: Int) -> (OpenAPITransport, URL) {
        let endpoint = URL(string: "http://127.0.0.1:\(port)")!
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [TransportPolicyResponse.self]
        let session = URLSession(configuration: configuration)
        return (OpenAPITransport(endpoint: endpoint, token: "test", instance: "instance", session: session), endpoint)
    }

    private func expectFailure(_ expected: (Kit.ClientError) -> Bool, _ operation: () async throws -> Void) async {
        do {
            try await operation()
            Issue.record("Expected a transport failure")
        } catch let error as Kit.ClientError {
            #expect(expected(error), "unexpected error \(error)")
        } catch {
            Issue.record("Unexpected error \(error)")
        }
    }

    private func get(_ path: String) -> HTTPRequest {
        HTTPRequest(method: .get, scheme: nil, authority: nil, path: path)
    }

    @Test func sessionEventStreamUsesTheContractRecordBound() throws {
        let bounds = try #require(OpenAPITransport.streamOperations[Operations.StreamSessionEvents.id])
        #expect(bounds.maxRecordBytes == 540_672)
        #expect(bounds.idleTimeout == .seconds(45))
    }

    @Test func rejectsRequestsOutsideTheConfiguredEndpoint() async throws {
        let (transport, endpoint) = transport(19303)
        let before = TransportPolicyResponse.requests().count
        let invalidEndpoint: (Kit.ClientError) -> Bool = { if case .invalidEndpoint = $0 { true } else { false } }
        await expectFailure(invalidEndpoint) {
            _ = try await transport.send(get("/v1/health"), body: nil, baseURL: URL(string: "http://127.0.0.1:1")!, operationID: "test")
        }
        await expectFailure(invalidEndpoint) {
            _ = try await transport.send(get("//example.com/v1/health"), body: nil, baseURL: endpoint, operationID: "test")
        }
        #expect(TransportPolicyResponse.requests().count == before)
    }

    @Test func rejectsOversizedRequestBodiesBeforeSending() async throws {
        let (transport, endpoint) = transport(19304)
        let before = TransportPolicyResponse.requests().count
        let body = HTTPBody(Data(repeating: 0x61, count: 1024 * 1024 + 1))
        await #expect(throws: (any Error).self) {
            _ = try await transport.send(HTTPRequest(method: .put, scheme: nil, authority: nil, path: "/v1/x"),
                                         body: body, baseURL: endpoint, operationID: "test")
        }
        #expect(TransportPolicyResponse.requests().count == before)
    }

    @Test func buffersCompleteResponsesWithoutContentLength() async throws {
        let (transport, endpoint) = transport(19302)
        let (_, body) = try await transport.send(get("/v1/sessions/s/transcript"), body: nil, baseURL: endpoint, operationID: "test")
        let data = try await Data(collecting: try #require(body), upTo: .max)
        #expect(data == Data(repeating: 0x20, count: 9 * 64 * 1024))
    }

    @Test func httpClientDoesNotFollowRedirects() async throws {
        let server = try RedirectServer()
        defer { server.stop() }
        let port = try await server.start()
        let client = try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "instance", serverID: "test")
        await #expect(throws: (any Error).self) { _ = try await client.scratchpad(session: "s") }
        #expect(server.paths() == ["/v1/sessions/s/scratchpad"])
    }
}

/// A loopback HTTP server that answers every request with a redirect to
/// `/elsewhere` and records requested paths, so redirect handling is exercised
/// by the real URL loading system.
private final class RedirectServer: @unchecked Sendable {
    private let listener: NWListener
    private let queue = DispatchQueue(label: "RedirectServer")
    private let lock = NSLock()
    private var requested: [String] = []

    init() throws {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        listener = try NWListener(using: parameters)
    }

    func start() async throws -> UInt16 {
        listener.newConnectionHandler = { [weak self] connection in self?.serve(connection) }
        return try await withCheckedThrowingContinuation { continuation in
            listener.stateUpdateHandler = { [listener] state in
                switch state {
                case .ready: continuation.resume(returning: listener.port!.rawValue); listener.stateUpdateHandler = nil
                case .failed(let error): continuation.resume(throwing: error); listener.stateUpdateHandler = nil
                default: break
                }
            }
            listener.start(queue: queue)
        }
    }

    func stop() { listener.cancel() }
    func paths() -> [String] { lock.withLock { requested } }

    private func serve(_ connection: NWConnection) {
        connection.start(queue: queue)
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, _, _ in
            guard let self, let data, let head = String(data: data, encoding: .utf8)?.split(separator: "\r\n").first else {
                connection.cancel(); return
            }
            let parts = head.split(separator: " ")
            if parts.count >= 2 { self.lock.withLock { self.requested.append(String(parts[1])) } }
            let response = "HTTP/1.1 307 Temporary Redirect\r\nLocation: /elsewhere\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
            connection.send(content: Data(response.utf8), completion: .contentProcessed { _ in connection.cancel() })
        }
    }
}
