import Darwin
import Foundation
import Synchronization

/// Answers ssh's questions from the app (ADR 0045). ssh runs the app's own
/// executable as its `SSH_ASKPASS` helper; the helper passes each question
/// to the app over a private Unix socket and prints the answer for ssh.
///
/// Each ssh attempt registers a random nonce, which the helper finds in its
/// environment and sends with the question, so a question reaches the
/// attempt that asked it and no other process can ask or answer.
final class AskpassBridge: @unchecked Sendable {
    /// Answers a question, or returns nil to cancel it.
    typealias Answer = @Sendable (SSHPrompt) async -> String?

    /// The largest question or answer passed over the socket.
    static let maxMessage = 64 << 10

    /// The socket's path, in a directory only this user can enter.
    let path: String
    private let listener: Int32
    private let source: any DispatchSourceRead
    private static let queue = DispatchQueue(label: "Kit askpass")
    private let handlers = Mutex<[String: Answer]>([:])
    private let closed = Atomic<Bool>(false)

    /// Listens at `path`, replacing anything left there.
    init(path: String) throws {
        self.path = path
        unlink(path)
        let socket = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard socket >= 0 else { throw POSIXError(.init(rawValue: errno) ?? .EIO) }
        var address = try Self.address(path)
        let bound = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(socket, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard bound == 0, chmod(path, 0o600) == 0, listen(socket, 16) == 0 else {
            let failure = errno
            close(socket)
            unlink(path)
            throw POSIXError(.init(rawValue: failure) ?? .EIO)
        }
        _ = fcntl(socket, F_SETFL, fcntl(socket, F_GETFL) | O_NONBLOCK)
        listener = socket
        source = DispatchSource.makeReadSource(fileDescriptor: socket, queue: Self.queue)
        source.setEventHandler { [weak self] in self?.accept() }
        source.setCancelHandler { close(socket) }
        source.resume()
    }

    deinit { stop() }

    /// Stops listening and removes the socket.
    func stop() {
        guard !closed.exchange(true, ordering: .relaxed) else { return }
        source.cancel()
        unlink(path)
    }

    /// The environment that makes ssh ask the app through this bridge, with
    /// `answer` answering the attempt's questions until it is unregistered.
    func register(_ answer: @escaping Answer) -> (nonce: String, environment: [String: String]) {
        var bytes = [UInt8](repeating: 0, count: 32)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        let nonce = bytes.map { String(format: "%02x", $0) }.joined()
        handlers.withLock { $0[nonce] = answer }
        return (nonce, [
            "SSH_ASKPASS": Askpass.executable,
            "SSH_ASKPASS_REQUIRE": "force",
            Askpass.socketVariable: path,
            Askpass.nonceVariable: nonce,
        ])
    }

    func unregister(_ nonce: String) {
        handlers.withLock { _ = $0.removeValue(forKey: nonce) }
    }

    /// Takes every waiting connection.
    private func accept() {
        while true {
            let connection = Darwin.accept(listener, nil, nil)
            if connection < 0 {
                if errno == EINTR { continue }
                return
            }
            // Accepted sockets inherit non-blocking mode; questions are read blocking.
            _ = fcntl(connection, F_SETFL, fcntl(connection, F_GETFL) & ~O_NONBLOCK)
            // Only this user's processes may ask.
            var uid: uid_t = 0, gid: gid_t = 0
            guard getpeereid(connection, &uid, &gid) == 0, uid == getuid() else {
                close(connection)
                continue
            }
            var on: Int32 = 1
            setsockopt(connection, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
            Task.detached { [weak self] in
                if let self { await self.serve(connection) } else { close(connection) }
            }
        }
    }

    private func serve(_ connection: Int32) async {
        defer { close(connection) }
        guard let line = await Self.readLine(connection),
              let request = try? JSONDecoder().decode(Askpass.Request.self, from: line) else { return }
        let handler = handlers.withLock { handlers in
            handlers.first { Self.equal($0.key, request.nonce) }?.value
        }
        let answer: String? = if let handler { await handler(SSHPrompt(request.prompt, kind: request.kind)) } else { nil }
        let response = Askpass.Response(answer: answer)
        guard var data = try? JSONEncoder().encode(response) else { return }
        data.append(0x0A)
        Self.write(data, to: connection)
    }

    /// Compares nonces in time independent of where they differ.
    private static func equal(_ a: String, _ b: String) -> Bool {
        let x = Array(a.utf8), y = Array(b.utf8)
        guard x.count == y.count else { return false }
        return zip(x, y).reduce(0) { $0 | ($1.0 ^ $1.1) } == 0
    }

    static func address(_ path: String) throws -> sockaddr_un {
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(path.utf8)
        let capacity = MemoryLayout.size(ofValue: address.sun_path)
        guard bytes.count < capacity else { throw POSIXError(.ENAMETOOLONG) }
        withUnsafeMutableBytes(of: &address.sun_path) { buffer in
            buffer.copyBytes(from: bytes)
            buffer[bytes.count] = 0
        }
        address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        return address
    }

    /// Reads one newline-terminated message, bounded by `maxMessage`.
    static func readLine(_ socket: Int32) async -> Data? {
        await withCheckedContinuation { continuation in
            DispatchQueue.global().async {
                var data = Data()
                var buffer = [UInt8](repeating: 0, count: 4096)
                while data.count <= maxMessage {
                    let count = read(socket, &buffer, buffer.count)
                    if count < 0 && errno == EINTR { continue }
                    guard count > 0 else { break }
                    data.append(contentsOf: buffer[0..<count])
                    if let end = data.firstIndex(of: 0x0A) {
                        continuation.resume(returning: data[..<end])
                        return
                    }
                }
                continuation.resume(returning: nil)
            }
        }
    }

    static func write(_ data: Data, to socket: Int32) {
        data.withUnsafeBytes { buffer in
            var offset = 0
            while offset < buffer.count {
                let count = Darwin.write(socket, buffer.baseAddress! + offset, buffer.count - offset)
                if count < 0 && errno == EINTR { continue }
                guard count > 0 else { return }
                offset += count
            }
        }
    }
}

/// The app's executable running as ssh's askpass helper.
enum Askpass {
    static let socketVariable = "KIT_ASKPASS_SOCKET"
    static let nonceVariable = "KIT_ASKPASS_NONCE"

    struct Request: Codable {
        let nonce: String
        let prompt: String
        /// ssh's `SSH_ASKPASS_PROMPT`, such as `confirm`.
        let kind: String?
    }

    struct Response: Codable {
        /// The answer, or nil when the question was cancelled.
        let answer: String?
    }

    /// The helper ssh runs: this executable.
    static var executable: String {
        Bundle.main.executableURL?.path ?? CommandLine.arguments[0]
    }

    /// Runs as ssh's askpass helper when the environment says to, returning
    /// the exit status; returns nil when this process is the app.
    static func runIfRequested(environment: [String: String] = ProcessInfo.processInfo.environment,
                               arguments: [String] = CommandLine.arguments) -> Int32? {
        guard let socket = environment[socketVariable], let nonce = environment[nonceVariable] else { return nil }
        let kind = environment["SSH_ASKPASS_PROMPT"]
        // ssh shows a notice, such as touching a security key, and ends the
        // helper itself; there is nothing to answer.
        if kind == "none" { return 0 }
        let prompt = arguments.count > 1 ? arguments[1] : ""
        guard let answer = ask(socket: socket, request: Request(nonce: nonce, prompt: prompt, kind: kind)) else { return 1 }
        // A confirmation is answered by the exit status alone.
        if kind != "confirm" { FileHandle.standardOutput.write(Data((answer + "\n").utf8)) }
        return 0
    }

    /// Asks the app over `socket`, waiting as long as the user takes. Nil
    /// means cancelled or that the app couldn't be asked.
    static func ask(socket path: String, request: Request) -> String? {
        let socket = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard socket >= 0 else { return nil }
        defer { close(socket) }
        guard var address = try? AskpassBridge.address(path) else { return nil }
        let connected = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { connect(socket, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard connected == 0, var data = try? JSONEncoder().encode(request), data.count < AskpassBridge.maxMessage else { return nil }
        var on: Int32 = 1
        setsockopt(socket, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
        data.append(0x0A)
        AskpassBridge.write(data, to: socket)
        var reply = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while reply.count <= AskpassBridge.maxMessage {
            let count = read(socket, &buffer, buffer.count)
            if count < 0 && errno == EINTR { continue }
            guard count > 0 else { break }
            reply.append(contentsOf: buffer[0..<count])
            if let end = reply.firstIndex(of: 0x0A) {
                return (try? JSONDecoder().decode(Response.self, from: reply[..<end]))?.answer
            }
        }
        return nil
    }
}
