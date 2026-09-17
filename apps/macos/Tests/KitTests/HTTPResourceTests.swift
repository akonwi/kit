import Foundation
import Testing
@testable import Kit

private final class RequestCounts: @unchecked Sendable {
    private let lock = NSLock()
    private var counts: [Int: (started: Int, stopped: Int)] = [:]
    func record(_ port: Int, stopped: Bool) {
        lock.lock(); defer { lock.unlock() }
        var value = counts[port] ?? (0, 0)
        if stopped { value.stopped += 1 } else { value.started += 1 }
        counts[port] = value
    }
    func value(_ port: Int) -> (started: Int, stopped: Int) {
        lock.lock(); defer { lock.unlock() }
        return counts[port] ?? (0, 0)
    }
}

/// Headers arrive, but the response body deliberately never finishes.
private final class StalledResponse: URLProtocol, @unchecked Sendable {
    static let counts = RequestCounts()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        Self.counts.record(url.port!, stopped: false)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url,
            statusCode: url.port == 18001 ? 503 : 200, httpVersion: nil,
            headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data("{".utf8))
    }
    override func stopLoading() {
        Self.counts.record(request.url!.port!, stopped: true)
    }
}

struct HTTPResourceTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StalledResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!,
                              token: "test", instance: "test", serverID: "test",
                              configuration: configuration)
    }
    private func waitFor(_ condition: () -> Bool) async throws {
        for _ in 0..<100 {
            if condition() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("HTTP request did not release its transport")
    }

    @Test func errorResponsesCloseBodiesWhileClientIsRetained() async throws {
        let client = try client(18001)
        for _ in 0..<20 {
            await #expect(throws: (any Error).self) { try await client.sessions() }
        }
        try await waitFor { StalledResponse.counts.value(18001).stopped == 20 }
        #expect(StalledResponse.counts.value(18001).started == 20)
        #expect(StalledResponse.counts.value(18001).stopped == 20)
        withExtendedLifetime(client) {}
    }

    @Test func cancellationClosesAnIdleBodyWhileClientIsRetained() async throws {
        let client = try client(18002)
        let task = Task { try await client.sessions() }
        try await waitFor { StalledResponse.counts.value(18002).started == 1 }
        task.cancel()
        try await waitFor { StalledResponse.counts.value(18002).stopped == 1 }
        _ = await task.result
        #expect(StalledResponse.counts.value(18002).started == 1)
        #expect(StalledResponse.counts.value(18002).stopped == 1)
        withExtendedLifetime(client) {}
    }
}
