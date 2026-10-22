import Foundation
import Synchronization
@testable import Kit

/// An ssh stand-in. Commands run to completion are answered by `respond`;
/// a master connection runs `master`, which may ask questions through the
/// app's real askpass bridge, as ssh's helper would.
final class FakeSSH: SSHRunner, @unchecked Sendable {
    typealias Respond = @Sendable (_ arguments: [String]) -> SSHOutput
    typealias Master = @Sendable (_ process: FakeSSHProcess, _ environment: [String: String]) async -> Void

    private let lock = Mutex<(runs: [[String]], starts: [[String]])>(([], []))
    let respond: Respond
    let master: Master

    init(respond: @escaping Respond = FakeSSH.standard(), master: @escaping Master = FakeSSH.agent) {
        self.respond = respond
        self.master = master
    }

    var runs: [[String]] { lock.withLock { $0.runs } }
    var starts: [[String]] { lock.withLock { $0.starts } }

    func run(_ arguments: [String], environment: [String: String], timeout: Duration) async throws -> SSHOutput {
        lock.withLock { $0.runs.append(arguments) }
        return respond(arguments)
    }

    func start(_ arguments: [String], environment: [String: String]) throws -> any SSHProcess {
        lock.withLock { $0.starts.append(arguments) }
        let process = FakeSSHProcess()
        let master = master
        Task { await master(process, environment) }
        return process
    }

    /// A master that authenticates without asking, as with ssh-agent.
    static let agent: Master = { process, _ in process.emit(SSHHost.readyMarker) }

    /// A master that asks `prompt` and connects when it is answered with
    /// `expected`, asking again after a wrong answer, up to three times.
    static func asking(_ prompt: String, expected: String, kind: String? = nil) -> Master {
        { process, environment in
            for _ in 0..<3 {
                let answer = await process.ask(prompt, kind: kind, environment: environment)
                if answer == expected {
                    process.emit(SSHHost.readyMarker)
                    return
                }
                if answer == nil { break }
            }
            process.exit(255, diagnostics: "me@devbox: Permission denied (publickey,password).\n")
        }
    }

    /// A master that ssh refuses because the host's key changed.
    static func changedKey(_ diagnostics: String) -> Master {
        { process, _ in process.exit(255, diagnostics: diagnostics) }
    }

    /// Answers configuration, forwards, and status queries. `status` answers
    /// a query, given whether it starts the server.
    static func standard(config: String = "stricthostkeychecking ask\n",
                         status: @escaping @Sendable (_ start: Bool) -> SSHOutput = { _ in SSHOutput(status: 0, stdout: report(), stderr: "") })
        -> Respond {
        { arguments in
            if arguments.contains("-G") { return SSHOutput(status: 0, stdout: config, stderr: "") }
            if let index = arguments.firstIndex(of: "-O") {
                return arguments[index + 1] == "check" ? SSHOutput(status: 255, stdout: "", stderr: "Control socket connect: No such file\n")
                    : SSHOutput(status: 0, stdout: "", stderr: "")
            }
            let remote = arguments.last ?? ""
            if remote.contains("kit server status --json") { return status(remote.contains("kit server start")) }
            return SSHOutput(status: 1, stdout: "", stderr: "unexpected command\n")
        }
    }

    static func report(url: String = "http://127.0.0.1:61342", protocolVersion: Int = kitWireVersion,
                       instance: String = "instance_remote") -> String {
        """
        {"home":"/home/me/.kit","pid":7,"url":"\(url)","kitVersion":"0.42.0","protocolVersion":\(protocolVersion),\
        "instanceId":"\(instance)","databaseReady":true,"token":"secret","future":1}
        """
    }
}

/// A fake long-lived ssh.
final class FakeSSHProcess: SSHProcess, @unchecked Sendable {
    private let state = Mutex<(status: Int32?, diagnostics: String, waiters: [CheckedContinuation<Int32, Never>])>((nil, "", []))
    let lines: AsyncStream<String>
    private let continuation: AsyncStream<String>.Continuation

    init() {
        (lines, continuation) = AsyncStream.makeStream(of: String.self)
    }

    var diagnostics: String { state.withLock { $0.diagnostics } }
    var running: Bool { state.withLock { $0.status == nil } }

    func emit(_ line: String) { continuation.yield(line) }

    /// Asks through the askpass helper's protocol, as ssh's helper does.
    func ask(_ prompt: String, kind: String?, environment: [String: String]) async -> String? {
        guard let socket = environment[Askpass.socketVariable], let nonce = environment[Askpass.nonceVariable] else { return nil }
        return await askOffThePool(socket: socket, request: .init(nonce: nonce, prompt: prompt, kind: kind))
    }

    func exit(_ status: Int32, diagnostics: String = "") {
        let waiters = state.withLock { state -> [CheckedContinuation<Int32, Never>] in
            guard state.status == nil else { return [] }
            state.status = status
            state.diagnostics += diagnostics
            defer { state.waiters = [] }
            return state.waiters
        }
        continuation.finish()
        waiters.forEach { $0.resume(returning: status) }
    }

    func wait() async -> Int32 {
        await withCheckedContinuation { continuation in
            let status = state.withLock { state -> Int32? in
                if let status = state.status { return status }
                state.waiters.append(continuation)
                return nil
            }
            if let status { continuation.resume(returning: status) }
        }
    }

    func stop() { exit(0) }
}

/// SSH hosts reached through a fake ssh, with a private bridge and sockets.
struct FakeSSHHosts {
    let ssh: FakeSSH
    let hosts: SSHHosts
    let passwords: MemorySSHPasswords
    let directory: String
    let added: Recorded<String>

    /// `port` is where forwards lead, as a loopback stub server listens.
    init(ssh: FakeSSH = FakeSSH(), port: Int = 1, passwords: [String: String] = [:]) throws {
        let directory = "/tmp/kt-" + UUID().uuidString.prefix(8)
        try SSHHosts.privateDirectory(directory)
        let bridge = try AskpassBridge(path: directory + "/a")
        let passwords = MemorySSHPasswords(passwords)
        let added = Recorded<String>()
        self.ssh = ssh
        self.passwords = passwords
        self.directory = directory
        self.added = added
        hosts = SSHHosts(environment: SSHHost.Environment(
            runner: ssh, bridge: { bridge }, passwords: passwords, directory: { directory }, freePort: { port },
            addKey: { key, environment in
                // ssh-add asks for the passphrase through the same bridge.
                let answer = await askOffThePool(socket: environment[Askpass.socketVariable] ?? "",
                                                 request: .init(nonce: environment[Askpass.nonceVariable] ?? "",
                                                                prompt: "Enter passphrase for \(key): ", kind: nil))
                added.append(key + "=" + (answer ?? ""))
            },
            timeout: .seconds(5)))
    }

    func remove() { try? FileManager.default.removeItem(atPath: directory) }
}

/// Values recorded from any thread.
final class Recorded<Value: Sendable>: Sendable {
    private let values = Mutex<[Value]>([])
    func append(_ value: Value) { values.withLock { $0.append(value) } }
    var all: [Value] { values.withLock { $0 } }
}

/// Asks as ssh's helper does. The helper blocks until it's answered, so it
/// runs on a dispatch thread rather than holding one of the threads Swift
/// concurrency shares with the code that answers it.
func askOffThePool(socket: String, request: Askpass.Request) async -> String? {
    await withCheckedContinuation { continuation in
        DispatchQueue.global().async { continuation.resume(returning: Askpass.ask(socket: socket, request: request)) }
    }
}
