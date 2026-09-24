import Foundation
import Testing
@testable import Kit

private final class PluginCommandResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(request.httpMethod == "POST")
        #expect(url.path == "/v1/sessions/session_one/plugin-commands")
        #expect(request.timeoutInterval == 120)
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        var data = request.httpBody ?? Data()
        if data.isEmpty, let stream = request.httpBodyStream {
            stream.open(); defer { stream.close() }
            var buffer = [UInt8](repeating: 0, count: 1024)
            while stream.hasBytesAvailable { let count = stream.read(&buffer, maxLength: buffer.count); if count <= 0 { break }; data.append(contentsOf: buffer.prefix(count)) }
        }
        let input = try? JSONDecoder().decode(WirePluginCommandInput.self, from: data)
        #expect(input?.id == "demo.echo")
        #expect(input?.instance == "host:1")
        #expect(input?.args == "  literal $()  ")
        let status = url.port == 19121 ? 204 : url.port == 19122 ? 409 : 422
        let code = status == 409 ? "plugin_command_unavailable" : url.port == 19124 ? "plugin_command_unavailable" : "plugin_command_failed"
        var body = status == 204 ? "" : "{\"error\":{\"code\":\"\(code)\",\"message\":\"Safe failure\"}}"
        if url.port == 19125 { body = #"{"error":{"code":"plugin_command_failed","message":""}}"# }
        if url.port == 19126 { body = #"{"error":{"code":"plugin_command_failed","message":"Failed","extra":true}}"# }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        if !body.isEmpty { client?.urlProtocol(self, didLoad: Data(body.utf8)) }
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct PluginCommandTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [PluginCommandResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: config)
    }
    @Test func acceptsNoContentAndSendsGenerationWithExactArguments() async throws {
        try await client(19121).executePluginCommand("session_one", input: .init(id: "demo.echo", instance: "host:1", args: "  literal $()  "))
    }
    @Test func validatesTypedFailureStatusAndDoesNotRetry() async throws {
        for port in [19122, 19123] {
            do {
                try await client(port).executePluginCommand("session_one", input: .init(id: "demo.echo", instance: "host:1", args: "  literal $()  "))
                Issue.record("Expected plugin failure")
            } catch let error as PluginCommandFailure { #expect(error.unavailable == (port == 19122)) }
        }
        for port in [19124, 19125, 19126] {
            await #expect(throws: ClientError.self) {
                try await client(port).executePluginCommand("session_one", input: .init(id: "demo.echo", instance: "host:1", args: "  literal $()  "))
            }
        }
    }
}
