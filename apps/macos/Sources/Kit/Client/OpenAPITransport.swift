import Foundation
import HTTPTypes
import HTTPTypesFoundation
import OpenAPIRuntime

/// Kit-owned transport for generated OpenAPI clients.
///
/// Generated code owns operation serialization while this transport retains the
/// daemon's loopback, bearer-token, no-redirect, and bounded-body policies.
/// Ordinary responses are fully buffered under a total cap. Successful stream
/// operations (ADR 0035) instead return a pull-based body that bounds each
/// record and the idle time between records, because a stream has no end.
final class OpenAPITransport: ClientTransport, @unchecked Sendable {
    /// Per-operation bounds for server-push streams.
    struct StreamBounds: Sendable {
        /// Maximum bytes of one record's field lines, including terminators.
        let maxRecordBytes: Int
        /// Longest silence allowed between record or heartbeat boundaries.
        let idleTimeout: Duration
    }

    /// The contract's stream operations. Record bounds mirror each operation's
    /// `x-kit-stream.maxRecordBytes`; the idle bound allows three missed
    /// 15-second heartbeats.
    static let streamOperations: [String: StreamBounds] = [
        "streamSessionVCS": StreamBounds(maxRecordBytes: 64 * 1024, idleTimeout: .seconds(45)),
    ]

    private let endpoint: URL
    private let token: String
    private let instance: String
    private let session: URLSession
    private let streams: [String: StreamBounds]
    private let maximumRequestBytes = 1 * 1024 * 1024
    private let maximumResponseBytes = 512 * 1024

    init(endpoint: URL, token: String, instance: String, session: URLSession,
         streams: [String: StreamBounds] = OpenAPITransport.streamOperations) {
        self.endpoint = endpoint
        self.token = token
        self.instance = instance
        self.session = session
        self.streams = streams
    }

    func send(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String) async throws -> (HTTPResponse, HTTPBody?) {
        guard baseURL == endpoint, let path = request.path,
              let url = URL(string: path, relativeTo: endpoint)?.absoluteURL,
              url.scheme == endpoint.scheme, url.host == endpoint.host, url.port == endpoint.port else {
            throw ClientError.invalidEndpoint
        }
        let complete = HTTPRequest(method: request.method, url: url, headerFields: request.headerFields)
        guard var urlRequest = URLRequest(httpRequest: complete) else { throw ClientError.invalidEndpoint }
        urlRequest.setValue("Bearer " + token, forHTTPHeaderField: "Authorization")
        urlRequest.setValue(instance, forHTTPHeaderField: "X-Kit-Instance-ID")
        urlRequest.setValue(String(kitWireVersion), forHTTPHeaderField: "X-Kit-Protocol-Version")
        if let body {
            urlRequest.httpBody = try await Data(collecting: body, upTo: maximumRequestBytes)
        }

        let (bytes, response) = try await session.bytes(for: urlRequest)
        guard let response = response as? HTTPURLResponse, let httpResponse = response.httpResponse else {
            bytes.task.cancel()
            throw ClientError.invalidPayload
        }
        if let bounds = streams[operationID], response.statusCode == 200 {
            guard response.mimeType == "text/event-stream" else {
                bytes.task.cancel()
                throw ClientError.invalidPayload
            }
            // Ownership of the request passes to the body, which cancels it
            // when released, canceled, or idle beyond its bound.
            let stream = EventStreamBody(bytes: bytes, bounds: bounds)
            return (httpResponse, HTTPBody(stream.chunks(), length: .unknown, iterationBehavior: .single))
        }
        defer { bytes.task.cancel() }
        if response.expectedContentLength > Int64(maximumResponseBytes) { throw ClientError.oversized }
        var data = Data()
        for try await byte in bytes {
            guard data.count < maximumResponseBytes else { throw ClientError.oversized }
            data.append(byte)
        }
        return (httpResponse, data.isEmpty ? nil : HTTPBody(data))
    }
}

/// A pull-based SSE body that yields one line per chunk. It fails with
/// `ClientError.oversized` when a record's lines exceed the record bound,
/// `ClientError.invalidPayload` for a line that is not UTF-8 or a record
/// truncated by the end of the response, and
/// `ClientError.disconnected` when no record or heartbeat boundary arrives
/// within the idle bound. There is no total length cap.
final class EventStreamBody: @unchecked Sendable {
    private var iterator: URLSession.AsyncBytes.AsyncIterator
    private let task: URLSessionDataTask
    private let bounds: OpenAPITransport.StreamBounds
    private let lock = NSLock()
    private var lastBoundary = ContinuousClock.now
    private var timedOut = false
    private var finished = false
    private var recordBytes = 0
    private var watchdog: Task<Void, Never>?

    init(bytes: URLSession.AsyncBytes, bounds: OpenAPITransport.StreamBounds) {
        iterator = bytes.makeAsyncIterator()
        task = bytes.task
        self.bounds = bounds
        let interval = bounds.idleTimeout / 4
        watchdog = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: interval)
                guard let self, self.expireIfIdle() else { continue }
                return
            }
        }
    }

    deinit {
        watchdog?.cancel()
        task.cancel()
    }

    /// The body's chunks. Iterating once consumes the response.
    func chunks() -> AsyncThrowingStream<ArraySlice<UInt8>, any Error> {
        AsyncThrowingStream(unfolding: { [self] in
            try await withTaskCancellationHandler {
                try await self.nextLine()
            } onCancel: {
                // AsyncBytes may wait on an idle stream without observing
                // cancellation. Close the request immediately.
                self.task.cancel()
            }
        })
    }

    private func expireIfIdle() -> Bool {
        let expired = lock.withLock {
            guard !finished, ContinuousClock.now - lastBoundary > bounds.idleTimeout else { return false }
            timedOut = true
            return true
        }
        if expired { task.cancel() }
        return expired
    }

    private func nextLine() async throws -> ArraySlice<UInt8>? {
        var line: [UInt8] = []
        do {
            while let byte = try await iterator.next() {
                try Task.checkCancellation()
                line.append(byte)
                if byte == 10, line == [10] || line == [13, 10] {
                    // A blank line dispatches a record or ends a heartbeat.
                    recordBytes = 0
                    lock.withLock { lastBoundary = .now }
                    return ArraySlice(line)
                }
                guard recordBytes + line.count <= bounds.maxRecordBytes else { throw ClientError.oversized }
                guard byte == 10 else { continue }
                // Event streams are UTF-8; never let decoding replace bytes.
                guard String(bytes: line, encoding: .utf8) != nil else { throw ClientError.invalidPayload }
                recordBytes += line.count
                return ArraySlice(line)
            }
        } catch {
            if lock.withLock({ timedOut }) { throw ClientError.disconnected }
            throw error
        }
        lock.withLock { finished = true }
        // A record without its dispatching blank line is truncated framing,
        // not an update; SSE decoders would silently drop it.
        guard line.isEmpty, recordBytes == 0 else { throw ClientError.invalidPayload }
        return nil
    }
}
