import Foundation
import Network

/// A loopback HTTP server that answers every request with one status and
/// body, so code under test reaches a real listener.
final class LoopbackHTTPServer: @unchecked Sendable {
    private let listener: NWListener
    private let queue = DispatchQueue(label: "LoopbackHTTPServer")
    private let response: Data
    private let received = NSLock()
    private var receivedRequests: [String] = []

    /// The requests the server received, as text.
    var requests: [String] { received.withLock { receivedRequests } }

    init(status: String = "200 OK", body: String = "{}") throws {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        listener = try NWListener(using: parameters)
        response = Data("HTTP/1.1 \(status)\r\nContent-Type: application/json\r\nContent-Length: \(body.utf8.count)\r\nConnection: close\r\n\r\n\(body)".utf8)
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
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self, response] data, _, _, _ in
            if let self, let data { self.received.withLock { self.receivedRequests.append(String(decoding: data, as: UTF8.self)) } }
            connection.send(content: response, completion: .contentProcessed { _ in connection.cancel() })
        }
    }
}
