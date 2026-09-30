import Foundation
import OpenAPIRuntime
import Testing
@testable import Kit

private let toastFrame = #"{"pluginId":"demo","instance":"host:1","title":"Notice","variant":"info"}"#

private func toastRecord(_ data: String) -> String { "event: plugin.toast\ndata: " + data + "\n\n" }

/// A `plugin.toast` record padded with JSON whitespace to exactly `size` bytes
/// of field lines (excluding the dispatching blank line).
private func paddedToast(size: Int) -> String {
    let fixed = "event: plugin.toast\ndata: ".utf8.count + toastFrame.utf8.count + 1
    return "event: plugin.toast\ndata: " + toastFrame + String(repeating: " ", count: size - fixed) + "\n\n"
}

private func toastError(_ code: String) -> String { #"{"error":{"code":""# + code + #"","message":"rejected"}}"# }

private struct ToastScript {
    var status = 200
    var contentType = "text/event-stream; charset=utf-8"
    var body = ""
}

private func toastScript(_ port: Int) -> ToastScript {
    switch port {
    case 19201:
        let crlf = "event: plugin.toast\r\ndata: " + toastFrame.replacingOccurrences(of: "Notice", with: "Second") + "\r\n\r\n"
        return ToastScript(body: ": connected\n\n" + toastRecord(toastFrame) + ": heartbeat\r\n\r\n" + crlf)
    case 19202: return ToastScript(status: 401, contentType: "application/json", body: toastError("unauthorized"))
    case 19203: return ToastScript(body: ": connected\n\nevent: plugin.toast\ndata: " + toastFrame + "\n")
    case 19204: return ToastScript(status: 429, contentType: "application/json", body: toastError("capacity_exceeded"))
    case 19205: return ToastScript(status: 503, contentType: "application/json", body: toastError("unavailable"))
    case 19206: return ToastScript(status: 404, contentType: "application/json", body: toastError("not_found"))
    case 19207: return ToastScript(contentType: "application/x-ndjson", body: toastFrame + "\n")
    case 19208: return ToastScript(body: paddedToast(size: 32 * 1024))
    case 19209: return ToastScript(body: paddedToast(size: 32 * 1024 + 1))
    case 19210: return ToastScript(body: "event: plugin.toast\ndata: {\"pluginId\":\"demo\",\ndata: \"instance\":\"host:1\",\"title\":\"Notice\",\"variant\":\"info\"}\n\n")
    case 19211: return ToastScript(body: "event: plugin.notice\ndata: " + toastFrame + "\n\n")
    case 19212: return ToastScript(body: "id: 1\n" + toastRecord(toastFrame))
    case 19213: return ToastScript(body: toastRecord(toastFrame.replacingOccurrences(of: "\"info\"", with: "\"success\"")))
    case 19214: return ToastScript(body: toastRecord(toastFrame.replacingOccurrences(of: "\"variant\"", with: "\"extra\":true,\"variant\"")))
    case 19215: return ToastScript(status: 404, contentType: "text/plain", body: "404 page not found\n")
    case 19216: return ToastScript(status: 429, contentType: "application/json", body: toastError("unavailable"))
    default: return ToastScript(status: 500, contentType: "application/json", body: toastError("internal"))
    }
}

private final class PluginNotificationResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(request.httpMethod == "GET")
        #expect(url.path == "/v1/sessions/session_one/plugin-toasts")
        #expect(url.query == nil)
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Protocol-Version") == String(kitWireVersion))
        if url.port == 19200 {
            client?.urlProtocol(self, didFailWithError: URLError(.networkConnectionLost))
            return
        }
        let script = toastScript(url.port!)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: script.status, httpVersion: nil,
            headerFields: ["Content-Type": script.contentType])!, cacheStoragePolicy: .notAllowed)
        // Deliberately fragment records across transport callbacks.
        let body = Data(script.body.utf8)
        for start in stride(from: 0, to: body.count, by: 20) {
            client?.urlProtocol(self, didLoad: body.subdata(in: start..<min(start + 20, body.count)))
        }
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

private actor NotificationCollector {
    var values: [String] = []
    func append(_ notification: PluginNotification) { values.append(notification.title) }
}

@Suite(.serialized)
struct PluginNotificationTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [PluginNotificationResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: config)
    }
    private func watch(_ port: Int, _ collector: NotificationCollector) async -> (any Error)? {
        do { try await client(port).watchPluginNotifications("session_one") { await collector.append($0) } }
        catch { return error }
        Issue.record("watchPluginNotifications returned without an error")
        return nil
    }

    @Test func declaresContractStreamBounds() throws {
        let bounds = try #require(OpenAPITransport.streamOperations["streamPluginToasts"])
        #expect(bounds.maxRecordBytes == 32 * 1024)
        #expect(bounds.idleTimeout == .seconds(45))
    }

    @Test func decodesRecordsStrictly() throws {
        let decoded = try #require(try PluginToastRecords.decode(ServerSentEvent(event: "plugin.toast", data: toastFrame)))
        #expect(decoded.title == "Notice")
        #expect(decoded.variant == "info")
        #expect(try PluginToastRecords.decode(ServerSentEvent()) == nil)
        for event in [ServerSentEvent(event: "plugin.notice", data: toastFrame), ServerSentEvent(event: "plugin.toast"),
                      ServerSentEvent(data: toastFrame), ServerSentEvent(id: "1", event: "plugin.toast", data: toastFrame),
                      ServerSentEvent(event: "plugin.toast", data: toastFrame + "\n{}"),
                      ServerSentEvent(event: "plugin.toast", data: toastFrame.replacingOccurrences(of: "\"title\"", with: "\"title\":\"First\",\"ti\\u0074le\""))] {
            #expect(throws: Kit.ClientError.self) { try PluginToastRecords.decode(event) }
        }
    }

    @Test func streamsFragmentedRecordsAcrossLineEndingsAndIgnoresHeartbeats() async throws {
        let collector = NotificationCollector()
        let error = await watch(19201, collector)
        guard case Kit.ClientError.disconnected? = error as? Kit.ClientError else { Issue.record("unexpected \(String(describing: error))"); return }
        #expect(await collector.values == ["Notice", "Second"])
    }

    @Test func mapsPreStreamErrorsByStatus() async throws {
        for (port, status) in [(19202, 401), (19204, 429), (19205, 503), (19206, 404)] {
            let collector = NotificationCollector()
            let error = await watch(port, collector)
            guard case Kit.ClientError.http(let code)? = error as? Kit.ClientError else { Issue.record("\(port): \(String(describing: error))"); continue }
            #expect(code == status)
            #expect(await collector.values.isEmpty)
        }
    }

    @Test func rejectsProtocolViolationsAsTerminal() async throws {
        let expectations: [(Int, (Kit.ClientError) -> Bool)] = [
            (19203, { if case .invalidPayload = $0 { true } else { false } }), // truncated record
            (19207, { if case .invalidPayload = $0 { true } else { false } }), // wrong content type
            (19209, { if case .oversized = $0 { true } else { false } }),      // one byte over the bound
            (19210, { if case .invalidPayload = $0 { true } else { false } }), // multiple data lines
            (19211, { if case .invalidPayload = $0 { true } else { false } }), // unknown record name
            (19212, { if case .invalidPayload = $0 { true } else { false } }), // id on a live-only stream
            (19213, { if case .invalidPayload = $0 { true } else { false } }), // unknown variant
            (19214, { if case .invalidPayload = $0 { true } else { false } }), // unknown field
            // Pre-stream responses outside the contract: plain text and an
            // undeclared code for a declared status.
            (19215, { if case .invalidPayload = $0 { true } else { false } }),
            (19216, { if case .invalidPayload = $0 { true } else { false } }),
        ]
        for (port, matches) in expectations {
            let collector = NotificationCollector()
            let error = await watch(port, collector)
            guard let failure = error as? Kit.ClientError, matches(failure) else { Issue.record("\(port): \(String(describing: error))"); continue }
            #expect(await collector.values.isEmpty)
        }
    }

    @Test func acceptsARecordExactlyAtTheBound() async throws {
        let collector = NotificationCollector()
        guard case Kit.ClientError.disconnected? = await watch(19208, collector) as? Kit.ClientError else { Issue.record("record at bound rejected"); return }
        #expect(await collector.values == ["Notice"])
    }

    @Test func connectionFailuresBeforeAResponseStayTransient() async throws {
        let error = await watch(19200, NotificationCollector())
        #expect(error != nil)
        #expect(!(error is Kit.ClientError), "a connection failure must not be classified as a protocol violation: \(String(describing: error))")
    }
}
