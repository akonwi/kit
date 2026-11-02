import Foundation
import Network

/// A loopback HTTP server that answers every request with one status and
/// body, or each path with its own response, so code under test reaches a
/// real listener.
final class LoopbackHTTPServer: @unchecked Sendable {
    /// A response to a path.
    struct Response {
        var status = "200 OK"
        var headers: [String: String] = ["Content-Type": "text/html; charset=utf-8"]
        var body: String

        fileprivate var data: Data {
            let lines = ["HTTP/1.1 \(status)"] + headers.sorted { $0.key < $1.key }.map { "\($0.key): \($0.value)" }
                + ["Content-Length: \(body.utf8.count)", "Connection: close"]
            return Data((lines.joined(separator: "\r\n") + "\r\n\r\n" + body).utf8)
        }
    }

    private let listener: NWListener
    private let queue = DispatchQueue(label: "LoopbackHTTPServer")
    private let respond: @Sendable (String) -> Data
    private let received = NSLock()
    private var receivedRequests: [String] = []

    /// The requests the server received, as text.
    var requests: [String] { received.withLock { receivedRequests } }

    convenience init(status: String = "200 OK", body: String = "{}") throws {
        let response = Response(status: status, headers: ["Content-Type": "application/json"], body: body).data
        try self.init { _ in response }
    }

    /// A server answering each path in `routes`, ignoring any query, and
    /// others with Not Found.
    convenience init(routes: [String: Response]) throws {
        let responses = routes.mapValues(\.data)
        let missing = Response(status: "404 Not Found", body: "").data
        try self.init { request in
            let target = request.split(separator: " ", maxSplits: 2).dropFirst().first.map(String.init) ?? ""
            let path = target.split(separator: "?", maxSplits: 1).first.map(String.init) ?? target
            return responses[path] ?? missing
        }
    }

    private init(respond: @escaping @Sendable (String) -> Data) throws {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        listener = try NWListener(using: parameters)
        self.respond = respond
    }

    /// Starts listening and returns the server's URL.
    func start() async throws -> URL {
        listener.newConnectionHandler = { [weak self] connection in self?.serve(connection) }
        let port: UInt16 = try await withCheckedThrowingContinuation { continuation in
            listener.stateUpdateHandler = { [listener] state in
                switch state {
                case .ready: continuation.resume(returning: listener.port!.rawValue); listener.stateUpdateHandler = nil
                case .failed(let error): continuation.resume(throwing: error); listener.stateUpdateHandler = nil
                default: break
                }
            }
            listener.start(queue: queue)
        }
        return URL(string: "http://127.0.0.1:\(port)")!
    }

    func stop() { listener.cancel() }

    private func serve(_ connection: NWConnection) {
        connection.start(queue: queue)
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self, respond] data, _, _, _ in
            let request = data.map { String(decoding: $0, as: UTF8.self) } ?? ""
            if let self, data != nil { self.received.withLock { self.receivedRequests.append(request) } }
            connection.send(content: respond(request), completion: .contentProcessed { _ in connection.cancel() })
        }
    }
}
