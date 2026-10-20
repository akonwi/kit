import Foundation
import Network

/// Where a browser login returns after the provider's page approves it: an
/// http address on this Mac's loopback interface, and the state the login's
/// authorization URL carries, which only the real redirect repeats.
struct LoopbackRedirect: Equatable, Sendable {
    let url: URL
    let host: String
    let port: UInt16
    let state: String

    /// The redirect of a login whose authorization page is `authorization`,
    /// or nil when the redirect isn't a loopback http address with a port or
    /// the page carries no state.
    init?(redirect: URL, authorization: URL) {
        guard let url = Self.address(redirect.absoluteString), let host = url.host, let port = url.port,
              let state = URLComponents(url: authorization, resolvingAgainstBaseURL: false)?
                  .queryItems?.first(where: { $0.name == "state" })?.value,
              !state.isEmpty else { return nil }
        self.url = url
        self.host = host.lowercased()
        self.port = UInt16(port)
        self.state = state
    }

    /// `value` as a loopback http redirect address with a port, or nil.
    static func address(_ value: String) -> URL? {
        guard value.utf8.count <= 2048, let url = URL(string: value), url.scheme?.lowercased() == "http",
              url.user == nil, url.password == nil, let host = url.host?.lowercased(),
              let port = url.port, (1...Int(UInt16.max)).contains(port) else { return nil }
        // Any 127.0.0.0/8 address is loopback, as for the terminal client.
        let loopback = host == "localhost" || IPv4Address(host)?.rawValue.first == 127 || IPv6Address(host)?.isLoopback == true
        return loopback ? url : nil
    }

    /// The addresses to listen on, the first required. Browsers may resolve
    /// localhost to either loopback address, so it gets both.
    var listenHosts: [String] { host == "localhost" ? ["127.0.0.1", "::1"] : [host] }

    /// The redirect URL to relay for an HTTP request target, or nil when the
    /// request isn't on the redirect's path or lacks the login's state.
    func relay(_ target: String) -> String? {
        guard let request = URLComponents(string: target),
              Self.path(request.path) == Self.path(url.path),
              request.queryItems?.first(where: { $0.name == "state" })?.value == state,
              var relayed = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        relayed.percentEncodedQuery = request.percentEncodedQuery
        relayed.fragment = nil
        return relayed.string
    }

    private static func path(_ value: String) -> String { value.isEmpty ? "/" : value }
}

/// Receives a browser login's redirect on this Mac, as the terminal client
/// does: it listens on the redirect's loopback address, answers the browser
/// with a short page, and hands over the first redirect carrying the login's
/// state. Any other request is refused.
final class LoopbackRedirectReceiver: @unchecked Sendable {
    /// What the browser shows once Kit has the redirect.
    static let page = "<!doctype html><meta charset=\"utf-8\"><title>Kit</title><p>Kit received the authorization. You can close this window.</p>"
    /// What any other request gets.
    static let refusal = "This is not the redirect Kit is waiting for. If Kit is still waiting, paste this page's address into it."
    /// How long a connection may take to send its request.
    static let requestTimeout: DispatchTimeInterval = .seconds(5)
    /// How long to wait for a just-stopped receiver to let go of the port.
    static let releaseWait = Duration.milliseconds(500)
    /// The largest request head read.
    static let maxRequest = 16 * 1024

    private let redirect: LoopbackRedirect
    private let queue = DispatchQueue(label: "com.akonwi.kit.loopback-redirect")
    private let redirects: AsyncStream<String>
    private let continuation: AsyncStream<String>.Continuation
    // Confined to `queue`.
    private var listeners: [NWListener] = []
    /// The connections being answered, held until they close.
    private var connections: [ObjectIdentifier: NWConnection] = [:]
    private var delivered = false
    private var stopped = false

    private init(_ redirect: LoopbackRedirect) {
        self.redirect = redirect
        (redirects, continuation) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(1))
    }

    deinit {
        // Nothing else holds the receiver, so its listeners are safe to read.
        continuation.finish()
        let listeners = listeners
        queue.async { listeners.forEach { $0.cancel() } }
    }

    /// Listens on the redirect's address. Throws when its required address
    /// can't be bound, such as when another client already listens there,
    /// or when cancelled.
    static func start(_ redirect: LoopbackRedirect) async throws -> LoopbackRedirectReceiver {
        let receiver = LoopbackRedirectReceiver(redirect)
        do {
            try await withTaskCancellationHandler {
                for (index, host) in redirect.listenHosts.enumerated() {
                    do {
                        try await receiver.listen(host: host, required: index == 0)
                    } catch where index > 0 && !(error is CancellationError) {
                        // A Mac without IPv6 loopback can't have the browser use it.
                    }
                }
            } onCancel: {
                receiver.stop()
            }
        } catch {
            receiver.stop()
            throw error
        }
        return receiver
    }

    /// The first redirect carrying the login's state, or nil once stopped or
    /// cancelled. Only one caller may wait.
    func received() async -> String? {
        for await url in redirects { return url }
        return nil
    }

    /// Stops listening. A request already being answered finishes.
    func stop() {
        continuation.finish()
        queue.async {
            self.stopped = true
            self.listeners.forEach { $0.cancel() }
            self.listeners = []
        }
    }

    /// Binds `host`, retrying briefly while a receiver that just stopped
    /// still holds the port.
    private func listen(host: String, required: Bool) async throws {
        let deadline = ContinuousClock.now + Self.releaseWait
        while true {
            do {
                return try await bind(host: host, required: required)
            } catch NWError.posix(.EADDRINUSE) where ContinuousClock.now < deadline {
                try await Task.sleep(for: .milliseconds(50))
            }
        }
    }

    private func bind(host: String, required: Bool) async throws {
        let parameters = NWParameters.tcp
        // Lets a new sign-in reuse the port while the last one's connections
        // linger; a socket still listening there keeps it refused.
        parameters.allowLocalEndpointReuse = true
        parameters.requiredLocalEndpoint = .hostPort(host: .init(host), port: .init(rawValue: redirect.port)!)
        let listener = try NWListener(using: parameters)
        listener.newConnectionHandler = { [weak self] connection in self?.accept(connection) }
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            // Both closures run on `queue`.
            let resumed = Flag()
            @Sendable func finish(_ result: Result<Void, Error>) {
                guard !resumed.value else { return }
                resumed.value = true
                continuation.resume(with: result)
            }
            listener.stateUpdateHandler = { [weak self] state in
                switch state {
                case .ready: finish(.success(()))
                case let .failed(error), let .waiting(error):
                    // Without its required address, nothing can arrive.
                    if resumed.value && required { self?.continuation.finish() }
                    listener.cancel()
                    finish(.failure(error))
                case .cancelled:
                    listener.stateUpdateHandler = nil
                    listener.newConnectionHandler = nil
                    finish(.failure(CancellationError()))
                default: break
                }
            }
            queue.async {
                if self.stopped { finish(.failure(CancellationError())); return }
                self.listeners.append(listener)
                listener.start(queue: self.queue)
            }
        }
    }

    private final class Flag: @unchecked Sendable { var value = false }

    private func accept(_ connection: NWConnection) {
        let id = ObjectIdentifier(connection)
        connections[id] = connection
        connection.stateUpdateHandler = { [weak self] state in
            switch state {
            case .failed, .cancelled: self?.connections[id] = nil
            default: break
            }
        }
        connection.start(queue: queue)
        queue.asyncAfter(deadline: .now() + Self.requestTimeout) { connection.cancel() }
        read(connection, Data())
    }

    private func read(_ connection: NWConnection, _ head: Data) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: Self.maxRequest - head.count) { [weak self] data, _, complete, error in
            guard let self else { connection.cancel(); return }
            var head = head
            if let data { head.append(data) }
            if head.range(of: Data("\r\n\r\n".utf8)) != nil || complete || error != nil || head.count >= Self.maxRequest {
                self.answer(connection, head)
            } else {
                self.read(connection, head)
            }
        }
    }

    private func answer(_ connection: NWConnection, _ head: Data) {
        let line = head.split(separator: UInt8(ascii: "\r"), maxSplits: 1).first.flatMap { String(data: Data($0), encoding: .utf8) }
        let parts = line?.split(separator: " ") ?? []
        guard parts.count >= 2, let url = redirect.relay(String(parts[1])) else {
            respond(connection, status: "400 Bad Request", type: "text/plain; charset=utf-8", body: Self.refusal)
            return
        }
        // A request that arrives as listening stops can't be relayed.
        if stopped && !delivered {
            respond(connection, status: "409 Conflict", type: "text/plain; charset=utf-8",
                    body: "Kit is no longer waiting for this sign-in.")
            return
        }
        respond(connection, status: "200 OK", type: "text/html; charset=utf-8", body: Self.page) { [weak self] in
            guard let self, !self.delivered else { return }
            self.delivered = true
            self.continuation.yield(url)
        }
    }

    private func respond(_ connection: NWConnection, status: String, type: String, body: String, sent: @escaping @Sendable () -> Void = {}) {
        let body = Data(body.utf8)
        var response = Data(("HTTP/1.1 \(status)\r\nContent-Type: \(type)\r\nContent-Length: \(body.count)\r\n"
            + "Cache-Control: no-store\r\nConnection: close\r\n\r\n").utf8)
        response.append(body)
        connection.send(content: response, completion: .contentProcessed { _ in
            sent()
            connection.cancel()
        })
    }
}
