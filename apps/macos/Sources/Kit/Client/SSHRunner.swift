import Darwin
import Foundation
import Synchronization

/// The output of an ssh command that ran to completion.
struct SSHOutput: Equatable, Sendable {
    /// The exit status; 255 is ssh's own failure.
    let status: Int32
    let stdout: String
    let stderr: String
}

/// A long-lived ssh process, such as a host's master connection.
protocol SSHProcess: AnyObject, Sendable {
    /// Lines the process prints, ending when it exits.
    var lines: AsyncStream<String> { get }
    /// The end of what the process wrote to stderr.
    var diagnostics: String { get }
    var running: Bool { get }
    /// Waits for the process to exit and returns its status.
    func wait() async -> Int32
    /// Closes its input, then terminates and finally kills it if it lingers.
    func stop()
    /// Terminates it now, as when a question it asked can't be answered.
    func kill()
}

/// Runs ssh. Tests substitute a fake.
protocol SSHRunner: Sendable {
    /// Runs ssh with `arguments` to completion, terminating it after `timeout`.
    func run(_ arguments: [String], environment: [String: String], timeout: Duration) async throws -> SSHOutput
    /// Starts a long-lived ssh with `arguments`, whose input stays open
    /// until it is stopped.
    func start(_ arguments: [String], environment: [String: String]) throws -> any SSHProcess
}

/// Why ssh couldn't run or finish.
enum SSHRunError: LocalizedError, Equatable {
    case launch(String)
    case timedOut

    var errorDescription: String? {
        switch self {
        case .launch(let reason): "Couldn’t run ssh: \(reason)"
        case .timedOut: "ssh didn’t finish in time."
        }
    }
}

/// Runs the system ssh, so the user's SSH configuration, agent, and jump
/// hosts apply.
struct SystemSSH: SSHRunner {
    /// The largest output kept from one stream.
    static let outputLimit = 64 << 10
    /// How long ssh has to exit after its input closes, and again after SIGTERM.
    static let stopGrace: Duration = .seconds(2)

    var executable = URL(fileURLWithPath: "/usr/bin/ssh")

    func run(_ arguments: [String], environment: [String: String], timeout: Duration) async throws -> SSHOutput {
        let process = Process()
        process.executableURL = executable
        process.arguments = arguments
        process.environment = Self.environment(environment)
        process.standardInput = FileHandle.nullDevice
        let stdout = Pipe(), stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr
        let out = OutputTail(), err = OutputTail()
        out.collect(stdout.fileHandleForReading)
        err.collect(stderr.fileHandleForReading)
        let exited = ExitSignal(process)
        do { try process.run() } catch { throw SSHRunError.launch(error.localizedDescription) }
        let status: Int32? = await withTaskCancellationHandler {
            await withTaskGroup(of: Int32?.self) { group in
                group.addTask { await exited.wait() }
                group.addTask {
                    try? await Task.sleep(for: timeout)
                    return nil
                }
                let first = await group.next() ?? nil
                group.cancelAll()
                return first
            }
        } onCancel: {
            Self.terminate(process)
        }
        guard let status else {
            Self.terminate(process)
            _ = await exited.wait()
            throw Task.isCancelled ? CancellationError() : SSHRunError.timedOut
        }
        try Task.checkCancellation()
        await out.finished()
        await err.finished()
        return SSHOutput(status: status, stdout: out.text, stderr: err.text)
    }

    func start(_ arguments: [String], environment: [String: String]) throws -> any SSHProcess {
        try SystemSSHProcess(executable: executable, arguments: arguments, environment: Self.environment(environment))
    }

    /// The app's environment with `extra`. ssh never inherits another
    /// attempt's askpass settings.
    static func environment(_ extra: [String: String]) -> [String: String] {
        var environment = ProcessInfo.processInfo.environment
        for key in ["SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", Askpass.socketVariable, Askpass.nonceVariable] {
            environment.removeValue(forKey: key)
        }
        return environment.merging(extra) { $1 }
    }

    /// Asks ssh to exit, so it can end what it started on the host, then
    /// kills it if it lingers.
    static func terminate(_ process: Process) {
        guard process.isRunning else { return }
        process.terminate()
        let pid = process.processIdentifier
        DispatchQueue.global().asyncAfter(deadline: .now() + .seconds(2)) {
            if process.isRunning { kill(pid, SIGKILL) }
        }
    }
}

/// Waits for a process to exit.
private final class ExitSignal: Sendable {
    private let state = Mutex<(status: Int32?, waiters: [CheckedContinuation<Int32, Never>])>((nil, []))

    init(_ process: Process) {
        process.terminationHandler = { [self] process in
            let status = process.terminationReason == .uncaughtSignal ? 255 : process.terminationStatus
            let waiters = state.withLock { state in
                state.status = status
                defer { state.waiters = [] }
                return state.waiters
            }
            waiters.forEach { $0.resume(returning: status) }
        }
    }

    var status: Int32? { state.withLock { $0.status } }

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
}

/// Keeps the last `SystemSSH.outputLimit` bytes a pipe carries.
private final class OutputTail: Sendable {
    private let state = Mutex<(data: Data, done: Bool, waiters: [CheckedContinuation<Void, Never>])>((Data(), false, []))
    let onLine: (@Sendable (String) -> Void)?

    init(onLine: (@Sendable (String) -> Void)? = nil) { self.onLine = onLine }

    func collect(_ handle: FileHandle) {
        let onLine = onLine
        DispatchQueue.global().async { [self] in
            var pending = Data()
            while true {
                let chunk = handle.availableData
                if chunk.isEmpty { break }
                state.withLock { state in
                    state.data.append(chunk)
                    if state.data.count > SystemSSH.outputLimit {
                        state.data = state.data.suffix(SystemSSH.outputLimit)
                    }
                }
                if let onLine {
                    pending.append(chunk)
                    while let end = pending.firstIndex(of: 0x0A) {
                        onLine(String(decoding: pending[..<end], as: UTF8.self))
                        pending = Data(pending[(end + 1)...])
                    }
                    if pending.count > SystemSSH.outputLimit { pending = Data() }
                }
            }
            if let onLine, !pending.isEmpty { onLine(String(decoding: pending, as: UTF8.self)) }
            let waiters = state.withLock { state in
                state.done = true
                defer { state.waiters = [] }
                return state.waiters
            }
            waiters.forEach { $0.resume() }
        }
    }

    var text: String { state.withLock { String(decoding: $0.data, as: UTF8.self) } }

    func finished() async {
        await withCheckedContinuation { continuation in
            let done = state.withLock { state -> Bool in
                if state.done { return true }
                state.waiters.append(continuation)
                return false
            }
            if done { continuation.resume() }
        }
    }
}

/// A long-lived system ssh process.
private final class SystemSSHProcess: SSHProcess {
    private let process: Process
    private let input: Pipe
    private let exit: ExitSignal
    private let errors: OutputTail
    let lines: AsyncStream<String>

    init(executable: URL, arguments: [String], environment: [String: String]) throws {
        process = Process()
        process.executableURL = executable
        process.arguments = arguments
        process.environment = environment
        input = Pipe()
        let output = Pipe(), stderr = Pipe()
        process.standardInput = input
        process.standardOutput = output
        process.standardError = stderr
        let (lines, continuation) = AsyncStream.makeStream(of: String.self)
        self.lines = lines
        let reader = OutputTail { continuation.yield($0) }
        errors = OutputTail()
        exit = ExitSignal(process)
        reader.collect(output.fileHandleForReading)
        errors.collect(stderr.fileHandleForReading)
        do { try process.run() } catch { throw SSHRunError.launch(error.localizedDescription) }
        let exit = exit
        Task.detached {
            _ = await exit.wait()
            await reader.finished()
            continuation.finish()
        }
    }

    var diagnostics: String { errors.text }
    var running: Bool { exit.status == nil }

    func wait() async -> Int32 { await exit.wait() }

    func kill() {
        try? input.fileHandleForWriting.close()
        SystemSSH.terminate(process)
    }

    func stop() {
        try? input.fileHandleForWriting.close()
        guard running else { return }
        let process = process, exit = exit
        Task.detached {
            try? await Task.sleep(for: SystemSSH.stopGrace)
            if exit.status == nil { SystemSSH.terminate(process) }
        }
    }
}
