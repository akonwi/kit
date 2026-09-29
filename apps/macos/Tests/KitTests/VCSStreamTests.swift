import Foundation
import HTTPTypes
import OpenAPIRuntime
import Testing
@testable import Kit

private let vcsFrame = #"{"sessionId":"session_one","cwd":"/repo","status":{"root":"/repo","head":{"kind":"branch","name":"main"},"dirty":false,"pullRequest":{"number":42,"url":"https://github.com/a/b/pull/42"}}}"#

private func vcsRecord(_ data: String) -> String { "event: vcs.status\ndata: " + data + "\n\n" }

/// A `vcs.status` record padded with JSON whitespace to exactly `size` bytes
/// of field lines (excluding the dispatching blank line).
private func paddedRecord(size: Int) -> String {
    let fixed = "event: vcs.status\ndata: ".utf8.count + vcsFrame.utf8.count + 1
    return "event: vcs.status\ndata: " + vcsFrame + String(repeating: " ", count: size - fixed) + "\n\n"
}

private func errorBody(_ code: String) -> String { #"{"error":{"code":""# + code + #"","message":"rejected"}}"# }

private struct VCSScript {
    var status = 200
    var contentType = "text/event-stream; charset=utf-8"
    /// Chunks delivered after the given millisecond delay from the previous one.
    var chunks: [(Int, String)] = []
    var finish = true
}

private func vcsScript(_ port: Int) -> VCSScript {
    let fragmented = vcsRecord(vcsFrame)
    switch port {
    case 19601:
        return VCSScript(chunks: [(0, ": connected\n\n"), (0, String(fragmented.prefix(20))), (0, String(fragmented.dropFirst(20))),
                                  (0, ": heartbeat\r\n\r\n")])
    case 19602: return VCSScript(status: 401, contentType: "application/json", chunks: [(0, errorBody("unauthorized"))])
    case 19603: return VCSScript(chunks: [(0, ": connected\n\n"), (0, "event: vcs.status\ndata: " + vcsFrame + "\n")])
    case 19604: return VCSScript(status: 429, contentType: "application/json", chunks: [(0, errorBody("capacity_exceeded"))])
    case 19605: return VCSScript(status: 503, contentType: "application/json", chunks: [(0, errorBody("unavailable"))])
    case 19606: return VCSScript(status: 404, contentType: "application/json", chunks: [(0, errorBody("not_found"))])
    case 19607: return VCSScript(contentType: "application/x-ndjson", chunks: [(0, vcsFrame + "\n")])
    case 19608: return VCSScript(chunks: [(0, paddedRecord(size: 64 * 1024))])
    case 19609: return VCSScript(chunks: [(0, paddedRecord(size: 64 * 1024 + 1))])
    case 19610: return VCSScript(chunks: Array(repeating: (0, paddedRecord(size: 60 * 1024)), count: 12))
    case 19611, 19613: return VCSScript(chunks: [(0, ": connected\n\n")], finish: false)
    case 19612:
        return VCSScript(chunks: [(0, ": connected\n\n")] + Array(repeating: (100, ": heartbeat\n\n"), count: 8) + [(100, vcsRecord(vcsFrame))])
    case 19614: return VCSScript(chunks: [(0, "event: vcs.status\ndata: {\"sessionId\":\"session_one\",\ndata: \"cwd\":\"/repo\"}\n\n")])
    case 19615: return VCSScript(chunks: [(0, "event: vcs.other\ndata: " + vcsFrame + "\n\n")])
    case 19616: return VCSScript(chunks: [(0, vcsRecord(vcsFrame.replacingOccurrences(of: "\"number\":42", with: "\"number\":42,\"nu\\u006dber\":43")))])
    case 19620: return VCSScript(contentType: "application/json", chunks: [(0, vcsFrame)])
    case 19621:
        return VCSScript(contentType: "application/json", chunks: [(0, vcsFrame.replacingOccurrences(of: "https://github.com/a/b/pull/42", with: "file:///tmp/a"))])
    case 19622: return VCSScript(status: 503, contentType: "application/json", chunks: [(0, errorBody("unavailable"))])
    case 19623: return VCSScript(contentType: "application/json", chunks: [(0, vcsFrame.replacingOccurrences(of: "\"dirty\":false", with: "\"dirty\":false,\"extra\":true"))])
    default: return VCSScript(status: 500, contentType: "application/json", chunks: [(0, errorBody("internal"))])
    }
}

private final class VCSResponse: URLProtocol, @unchecked Sendable {
    static let stopped = StopCounts()
    private let lock = NSLock()
    private var halted = false

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let port = url.port!
        #expect(url.path == (port >= 19620 ? "/v1/sessions/session_one/vcs" : "/v1/sessions/session_one/vcs/events"))
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Protocol-Version") == String(kitWireVersion))
        let script = vcsScript(port)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: script.status, httpVersion: nil,
            headerFields: ["Content-Type": script.contentType])!, cacheStoragePolicy: .notAllowed)
        play(script.chunks[...], finish: script.finish)
    }
    /// Delivers chunks strictly in order, each after its delay, then finishes.
    private func play(_ chunks: ArraySlice<(Int, String)>, finish: Bool) {
        guard let (after, chunk) = chunks.first else {
            if finish { client?.urlProtocolDidFinishLoading(self) }
            return
        }
        deliver(after: after) { this in
            this.client?.urlProtocol(this, didLoad: Data(chunk.utf8))
            this.play(chunks.dropFirst(), finish: finish)
        }
    }
    override func stopLoading() {
        lock.withLock { halted = true }
        Self.stopped.record(request.url!.port!)
    }
    private func deliver(after milliseconds: Int, _ action: @escaping @Sendable (VCSResponse) -> Void) {
        let work = { [self] in if !lock.withLock({ halted }) { action(self) } }
        if milliseconds == 0 { work() } else { DispatchQueue.global().asyncAfter(deadline: .now() + .milliseconds(milliseconds), execute: work) }
    }
}

private final class StopCounts: @unchecked Sendable {
    private let lock = NSLock()
    private var counts: [Int: Int] = [:]
    func record(_ port: Int) { lock.withLock { counts[port, default: 0] += 1 } }
    func value(_ port: Int) -> Int { lock.withLock { counts[port] ?? 0 } }
}

private actor VCSCollector {
    var numbers: [Int] = []
    var count = 0
    func append(_ value: WireSessionVCSStatus) {
        count += 1
        if let number = value.status?.pullRequest?.number { numbers.append(number) }
    }
}

@Suite(.serialized)
struct VCSStreamTests {
    private func configuration() -> URLSessionConfiguration {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [VCSResponse.self]
        return config
    }
    private func client(_ port: Int) throws -> HTTPClient {
        try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: configuration())
    }
    private func watch(_ port: Int, _ collector: VCSCollector) async -> (any Error)? {
        do { try await client(port).watchVCS("session_one") { await collector.append($0) } }
        catch { return error }
        Issue.record("watchVCS returned without an error")
        return nil
    }
    /// Reads the stream through the transport directly with custom bounds.
    private func transportRecords(_ port: Int, idle: Duration) async throws -> [ServerSentEvent] {
        let endpoint = URL(string: "http://127.0.0.1:\(port)")!
        let transport = OpenAPITransport(endpoint: endpoint, token: "test", instance: "server",
            session: URLSession(configuration: configuration()),
            streams: ["streamSessionVCS": .init(maxRecordBytes: 64 * 1024, idleTimeout: idle)])
        let request = HTTPRequest(method: .get, scheme: nil, authority: nil, path: "/v1/sessions/session_one/vcs/events",
                                  headerFields: [HTTPField.Name("X-Kit-Protocol-Version")!: String(kitWireVersion)])
        let (_, body) = try await transport.send(request, body: nil, baseURL: endpoint, operationID: "streamSessionVCS")
        var events: [ServerSentEvent] = []
        for try await event in try #require(body).asDecodedServerSentEvents() { events.append(event) }
        return events
    }

    @Test func decodesGeneratedPayloadsAndRejectsMalformedRecords() throws {
        let state = try #require(try VCSStatusRecords.decode(ServerSentEvent(event: "vcs.status", data: vcsFrame), session: "session_one"))
        #expect(state.status?.head.name == "main")
        #expect(state.status?.head.kind == .value0)
        #expect(state.status?.pullRequest?.number == 42)
        #expect(try VCSStatusRecords.decode(ServerSentEvent(), session: "session_one") == nil)
        let invalid = [
            vcsFrame.replacingOccurrences(of: "\"number\":42", with: "\"number\":42,\"nu\\u006dber\":43"),
            vcsFrame.replacingOccurrences(of: "\"name\":\"main\"", with: "\"name\":\"\""),
            vcsFrame.replacingOccurrences(of: "\"dirty\":false", with: "\"dirty\":false,\"extra\":true"),
            vcsFrame.replacingOccurrences(of: ",\"dirty\":false", with: ""),
            vcsFrame.replacingOccurrences(of: "https://github.com/a/b/pull/42", with: "file:///tmp/a"),
            vcsFrame.replacingOccurrences(of: "session_one", with: "session_other"),
            vcsFrame.replacingOccurrences(of: "\"branch\"", with: "\"tag\""),
            "{", "[]", vcsFrame + vcsFrame,
        ]
        for data in invalid {
            #expect(throws: (any Error).self) { try VCSStatusRecords.decode(ServerSentEvent(event: "vcs.status", data: data), session: "session_one") }
        }
        for event in [ServerSentEvent(event: "vcs.other", data: vcsFrame), ServerSentEvent(event: "vcs.status"),
                      ServerSentEvent(data: vcsFrame), ServerSentEvent(id: "1", event: "vcs.status", data: vcsFrame),
                      ServerSentEvent(event: "vcs.status", data: vcsFrame + "\n{}")] {
            #expect(throws: Kit.ClientError.self) { try VCSStatusRecords.decode(event, session: "session_one") }
        }
    }

    @Test func streamsFragmentedRecordsAndIgnoresHeartbeats() async throws {
        let collector = VCSCollector()
        let error = await watch(19601, collector)
        guard case Kit.ClientError.disconnected? = error as? Kit.ClientError else { Issue.record("unexpected \(String(describing: error))"); return }
        #expect(await collector.numbers == [42])
    }

    @Test func mapsPreStreamErrorsByStatus() async throws {
        for (port, status) in [(19602, 401), (19604, 429), (19605, 503), (19606, 404)] {
            let collector = VCSCollector()
            let error = await watch(port, collector)
            guard case Kit.ClientError.http(let code)? = error as? Kit.ClientError else { Issue.record("\(port): \(String(describing: error))"); continue }
            #expect(code == status)
            #expect(await collector.count == 0)
        }
    }

    @Test func rejectsProtocolViolationsAsTerminal() async throws {
        let expectations: [(Int, (Kit.ClientError) -> Bool)] = [
            (19603, { if case .invalidPayload = $0 { true } else { false } }),
            (19607, { if case .invalidPayload = $0 { true } else { false } }),
            (19609, { if case .oversized = $0 { true } else { false } }),
            (19614, { if case .invalidPayload = $0 { true } else { false } }),
            (19615, { if case .invalidPayload = $0 { true } else { false } }),
            (19616, { if case .invalidPayload = $0 { true } else { false } }),
        ]
        for (port, matches) in expectations {
            let collector = VCSCollector()
            let error = await watch(port, collector)
            guard let failure = error as? Kit.ClientError, matches(failure) else { Issue.record("\(port): \(String(describing: error))"); continue }
            #expect(await collector.count == 0)
        }
    }

    @Test func boundsEachRecordButNotTheTotalStream() async throws {
        let exact = VCSCollector()
        guard case Kit.ClientError.disconnected? = await watch(19608, exact) as? Kit.ClientError else { Issue.record("record at bound rejected"); return }
        #expect(await exact.count == 1)
        // 12 records of 60 KiB exceed the 512 KiB buffered-response cap.
        let many = VCSCollector()
        guard case Kit.ClientError.disconnected? = await watch(19610, many) as? Kit.ClientError else { Issue.record("long stream rejected"); return }
        #expect(await many.count == 12)
    }

    @Test func idleBoundEndsSilentStreamsAndHeartbeatsResetIt() async throws {
        let started = ContinuousClock.now
        do {
            _ = try await transportRecords(19611, idle: .milliseconds(300))
            Issue.record("silent stream did not time out")
        } catch Kit.ClientError.disconnected {
            #expect(ContinuousClock.now - started >= .milliseconds(300))
        }
        // Heartbeats every 100 ms keep a 300 ms idle bound open for 900 ms.
        let events = try await transportRecords(19612, idle: .milliseconds(300))
        #expect(events.filter { $0.event == "vcs.status" }.count == 1)
    }

    @Test func streamBodyAdmitsOnlyOneConsumer() async throws {
        let session = URLSession(configuration: configuration())
        var request = URLRequest(url: URL(string: "http://127.0.0.1:19608/v1/sessions/session_one/vcs/events")!)
        request.setValue("Bearer test", forHTTPHeaderField: "Authorization")
        request.setValue("server", forHTTPHeaderField: "X-Kit-Instance-ID")
        request.setValue(String(kitWireVersion), forHTTPHeaderField: "X-Kit-Protocol-Version")
        let (bytes, _) = try await session.bytes(for: request)
        let body = EventStreamBody(bytes: bytes, bounds: .init(maxRecordBytes: 64 * 1024, idleTimeout: .seconds(5)))
        let first = body.chunks()
        do {
            for try await _ in body.chunks() {}
            Issue.record("second consumer was admitted")
        } catch Kit.ClientError.invalidPayload {}
        var lines = 0
        for try await _ in first { lines += 1 }
        #expect(lines == 3)
        session.invalidateAndCancel()
    }

    @Test func cancellationClosesAnIdleStream() async throws {
        let client = try client(19613)
        let task = Task { try await client.watchVCS("session_one") { _ in Issue.record("unexpected record") } }
        try await Task.sleep(for: .milliseconds(200))
        task.cancel()
        for _ in 0..<100 where VCSResponse.stopped.value(19613) == 0 { try await Task.sleep(for: .milliseconds(10)) }
        _ = await task.result
        #expect(VCSResponse.stopped.value(19613) == 1)
        withExtendedLifetime(client) {}
    }

    @Test func generatedClientReadsStatus() async throws {
        let read = try await client(19620).vcs("session_one")
        #expect(read.status?.pullRequest?.number == 42)
        #expect(read.status?.head.kind == .value0)
        let sanitized = try await client(19621).vcs("session_one")
        #expect(sanitized.status?.head.name == "main")
        #expect(sanitized.status?.pullRequest == nil)
        do { _ = try await client(19622).vcs("session_one"); Issue.record("expected unavailable") }
        catch Kit.ClientError.http(503) {}
        await #expect(throws: (any Error).self) { try await client(19623).vcs("session_one") }
    }
}
