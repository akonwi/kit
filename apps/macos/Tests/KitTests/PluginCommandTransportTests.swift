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
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Protocol-Version") == String(kitWireVersion))
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json; charset=utf-8")
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
        let (status, code): (Int, String?) = switch url.port {
        case 19121: (204, nil)
        case 19122: (409, "plugin_command_unavailable")
        case 19123: (422, "plugin_command_failed")
        case 19124: (422, "plugin_command_unavailable")
        case 19127: (409, "instance_mismatch")
        case 19128: (409, "conflict")
        case 19129: (503, "unavailable")
        case 19130: (418, "internal")
        default: (500, "internal")
        }
        var body = code.map { #"{"error":{"code":""# + $0 + #"","message":"Safe failure"}}"# } ?? ""
        if url.port == 19125 { body = #"{"error":{"code":"plugin_command_failed"}}"# }
        if url.port == 19126 { body = #"{"error":{"message":"Failed"}}"# }
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
    private func execute(_ port: Int) async throws {
        try await client(port).executePluginCommand("session_one", input: .init(id: "demo.echo", instance: "host:1", args: "  literal $()  "))
    }
    @Test func acceptsNoContentAndSendsGenerationWithExactArguments() async throws {
        try await execute(19121)
    }
    @Test func mapsDomainFailuresByCode() async throws {
        for (port, unavailable) in [(19122, true), (19123, false)] {
            do {
                try await execute(port)
                Issue.record("\(port): expected plugin failure")
            } catch let error as PluginCommandFailure { #expect(error.unavailable == unavailable) }
        }
    }
    @Test func mapsGenericFailuresWithoutClaimingPluginFailure() async throws {
        for (port, status) in [(19127, 409), (19128, 409), (19129, 503), (19131, 500)] {
            do {
                try await execute(port)
                Issue.record("\(port): expected failure")
            } catch Kit.ClientError.http(let code) { #expect(code == status, "\(port)") }
        }
    }
    @Test func rejectsResponsesOutsideTheContract() async throws {
        // An undeclared code for a declared status, missing required members,
        // and an undeclared status.
        for port in [19124, 19125, 19126, 19130] {
            do {
                try await execute(port)
                Issue.record("\(port): expected invalid payload")
            } catch Kit.ClientError.invalidPayload {}
        }
    }
}
