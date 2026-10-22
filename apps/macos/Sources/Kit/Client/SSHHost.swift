import CryptoKit
import Darwin
import Foundation
import LocalAuthentication
import Security
import Synchronization

/// What a user answered to one of ssh's questions.
struct SSHAnswer: Equatable, Sendable {
    let text: String
    /// Whether to keep the answer in the Keychain. Only passwords and key
    /// passphrases are kept.
    var save = false
}

/// A question for the user, with whether an earlier answer to it failed.
struct SSHPromptRequest: Equatable, Sendable {
    let prompt: SSHPrompt
    /// An earlier answer to this question was wrong, as a mistyped password.
    var retry = false
}

/// Asks the user one of ssh's questions; nil cancels connecting.
typealias SSHPrompter = @Sendable (SSHPromptRequest) async -> SSHAnswer?

/// Keeps the passwords users chose to save for SSH hosts.
protocol SSHPasswordStore: Sendable {
    func password(for destination: String) -> String?
    func save(_ password: String, for destination: String) throws
    func delete(_ destination: String) throws
}

/// SSH passwords as generic passwords in the login Keychain, one per destination.
struct KeychainSSHPasswords: SSHPasswordStore {
    static let service = "com.akonwi.kit.ssh-password"

    private func query(_ destination: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: Self.service,
         kSecAttrAccount as String: destination, kSecUseDataProtectionKeychain as String: false]
    }

    /// Reads the password without ever showing a Keychain dialog, which a
    /// connection in the background must not do; one that would needs the
    /// user to sign in instead.
    func password(for destination: String) -> String? {
        var query = query(destination)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        let context = LAContext()
        context.interactionNotAllowed = true
        query[kSecUseAuthenticationContext as String] = context
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let data = result as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    func save(_ password: String, for destination: String) throws {
        let data = Data(password.utf8)
        let status = SecItemUpdate(query(destination) as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecItemNotFound {
            var item = query(destination)
            item[kSecValueData as String] = data
            item[kSecAttrLabel as String] = "Kit SSH password for \(destination)"
            item[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlocked
            let added = SecItemAdd(item as CFDictionary, nil)
            guard added == errSecSuccess else { throw DeviceTokenError(status: added) }
        } else if status != errSecSuccess {
            throw DeviceTokenError(status: status)
        }
    }

    func delete(_ destination: String) throws {
        let status = SecItemDelete(query(destination) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw DeviceTokenError(status: status) }
    }
}

/// SSH passwords in memory, as tests keep them.
final class MemorySSHPasswords: SSHPasswordStore {
    private let passwords: Mutex<[String: String]>
    init(_ passwords: [String: String] = [:]) { self.passwords = Mutex(passwords) }
    var all: [String: String] { passwords.withLock { $0 } }
    func password(for destination: String) -> String? { passwords.withLock { $0[destination] } }
    func save(_ password: String, for destination: String) throws { passwords.withLock { $0[destination] = password } }
    func delete(_ destination: String) throws { passwords.withLock { _ = $0.removeValue(forKey: destination) } }
}

/// One SSH host's connection, shared by every server saved with its
/// destination and every window (ADR 0045). The app runs one master ssh per
/// host, which authenticates once; status queries and port forwards share
/// it, so they never ask again. The master's remote command ends when its
/// input closes, so nothing is left on the host when the app quits.
///
/// Only an explicit sign-in asks the user. A background connection answers
/// with saved passwords alone, and a question it can't answer leaves the
/// host waiting for sign-in until someone opens it.
actor SSHHost {
    /// A forward to the host's server, as one status report described it.
    struct Endpoint: Equatable, Sendable {
        /// The forward's loopback origin on this Mac.
        let url: URL
        let report: SSHStatusReport
        /// The `-L` specification, to cancel the forward.
        let spec: String
    }

    /// What the host needs from outside: what ssh runs, where it keeps its
    /// control socket, and where answers come from and go.
    struct Environment: Sendable {
        var runner: any SSHRunner = SystemSSH()
        /// The bridge ssh asks the app's questions through.
        var bridge: @Sendable () throws -> AskpassBridge = { try SSHHosts.bridge() }
        var passwords: any SSHPasswordStore = KeychainSSHPasswords()
        /// The directory for control sockets, which only this user can enter.
        var directory: @Sendable () throws -> String = { try SSHHosts.directory() }
        /// A free loopback port for a forward.
        var freePort: @Sendable () throws -> Int = { try SSHHost.freePort() }
        /// Adds a key to the agent and keeps its passphrase in the Keychain,
        /// as `ssh-add --apple-use-keychain` does.
        var addKey: @Sendable (_ key: String, _ environment: [String: String]) async -> Void = { key, environment in
            _ = try? await SystemSSH(executable: URL(fileURLWithPath: "/usr/bin/ssh-add"))
                .run(["--apple-use-keychain", "--", key], environment: environment, timeout: .seconds(30))
        }
        /// How long a background connection, or one between questions, may take.
        var timeout: Duration = .seconds(30)
    }

    nonisolated let destination: String
    private let environment: Environment
    private var master: (any SSHProcess)?
    private var forward: Endpoint?
    /// A background connection met a question only the user can answer.
    private var waitingForSignIn = false
    /// The host was closed for good; it connects no more.
    private var closed = false
    private var tail: Task<Void, Never>?
    private var observers: [UUID: @Sendable () -> Void] = [:]

    init(destination: String, environment: Environment = Environment()) {
        self.destination = destination
        self.environment = environment
    }

    /// The forward to the host's server, connecting first if needed. It never
    /// asks the user or starts the server.
    func resolve() async throws -> Endpoint {
        try await serialized { try await $0.connect(prompter: nil, start: false) }
    }

    /// Connects, asking the user through `prompter`, and starts the host's
    /// Kit server first when `start` is set, as Add Server and Start Server do.
    @discardableResult
    func signIn(start: Bool = false, prompter: @escaping SSHPrompter) async throws -> Endpoint {
        try await serialized { try await $0.connect(prompter: prompter, start: start) }
    }

    /// Drops the forward that carried `endpoint` when its server refused or
    /// no longer answers it, so the next resolve finds the server again.
    func invalidate(_ endpoint: Endpoint) async {
        guard forward == endpoint else { return }
        forward = nil
        await cancel(endpoint.spec)
    }

    /// Calls `handler` whenever the host's connection drops.
    func observe(_ handler: @escaping @Sendable () -> Void) -> UUID {
        let id = UUID()
        observers[id] = handler
        return id
    }

    func unobserve(_ id: UUID) { observers.removeValue(forKey: id) }

    /// Ends the connection for good, as when no saved server uses the host.
    /// A master that connects meanwhile is ended as soon as it's ready.
    func close() {
        closed = true
        forward = nil
        master?.stop()
        master = nil
        waitingForSignIn = false
    }

    /// Whether the host waits for someone to sign in.
    var needsSignIn: Bool { waitingForSignIn }

    /// Runs one connection at a time, in order.
    private func serialized<T: Sendable>(_ work: @escaping @Sendable (isolated SSHHost) async throws -> T) async throws -> T {
        let previous = tail
        let task = Task { () async throws -> T in
            await previous?.value
            return try await work(self)
        }
        tail = Task { _ = try? await task.value }
        return try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
    }

    private func connect(prompter: SSHPrompter?, start: Bool) async throws -> Endpoint {
        try await ensureMaster(prompter: prompter)
        let report = try await query(start: start)
        if let reason = DaemonCompatibility.mismatch(clientProtocol: kitWireVersion, daemonProtocol: report.protocolVersion) {
            throw ClientError.incompatibleDaemon(daemonVersion: report.kitVersion, appProtocol: kitWireVersion,
                                                 daemonProtocol: report.protocolVersion, reason: reason)
        }
        if let forward, forward.report.instanceId == report.instanceId, forward.report.url == report.url {
            return forward
        }
        if let old = forward {
            forward = nil
            await cancel(old.spec)
        }
        let endpoint = try await open(report)
        forward = endpoint
        return endpoint
    }

    // MARK: - The master connection

    private var control: String {
        get throws {
            let digest = SHA256.hash(data: Data(destination.utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
            return try environment.directory() + "/" + digest
        }
    }

    private func ensureMaster(prompter: SSHPrompter?) async throws {
        if closed { throw CancellationError() }
        if let master, master.running { return }
        if master != nil {
            // The master ended, and its forwards with it.
            master = nil
            forward = nil
        }
        let control = try control
        switch await check(control) {
        case .running:
            // Another master holds the socket, as another copy of the app's
            // may; its forward is kept until it stops working.
            return
        case .busy:
            throw ClientError.ssh("Another connection to \(destination) isn’t answering. Kit will try again.")
        case .stale:
            // A socket left by a master that died is in the way of a new one.
            unlink(control)
        case .absent:
            break
        }
        forward = nil
        if prompter == nil && waitingForSignIn { throw ClientError.sshSignInRequired }
        let configuration = try await configuration()
        let attempt = SSHAttempt(destination: destination, account: configuration.account, passwords: environment.passwords,
                                 prompter: prompter)
        let bridge = try environment.bridge()
        let registration = bridge.register { await attempt.answer($0) }
        defer { bridge.unregister(registration.nonce) }
        let arguments = ["-M", "-S", control, "-o", "ControlPersist=no", "-T"] + Self.common + configuration.options
            + ["--", destination, Self.shell("echo \(Self.readyMarker); read line")]
        let process = try environment.runner.start(arguments, environment: registration.environment)
        let ready = await Self.ready(process, attempt: attempt, timeout: environment.timeout)
        guard ready, !closed else {
            // ssh is ended before a question it asked is refused, so it never
            // sends an empty password or passphrase in place of an answer.
            process.kill()
            _ = await process.wait()
            attempt.finish()
            if closed { throw CancellationError() }
            throw await failure(process.diagnostics, attempt: attempt)
        }
        attempt.finish()
        master = process
        waitingForSignIn = false
        await attempt.keep(destination: destination, bridge: bridge, addKey: environment.addKey)
        Task { [weak self] in
            _ = await process.wait()
            await self?.ended(process)
        }
    }

    /// Whether `process` printed the ready marker: it authenticated and
    /// holds the connection. It may take as long as the user needs to answer
    /// questions, and `timeout` otherwise.
    private static func ready(_ process: any SSHProcess, attempt: SSHAttempt, timeout: Duration) async -> Bool {
        await withTaskGroup(of: Bool.self) { group in
            group.addTask {
                for await line in process.lines where line.trimmingCharacters(in: .whitespaces) == readyMarker { return true }
                return false
            }
            group.addTask {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .milliseconds(50))
                    if attempt.gaveUp || attempt.idle > timeout { return false }
                }
                return false
            }
            let first = await group.next() ?? false
            group.cancelAll()
            return first
        }
    }

    private func ended(_ process: any SSHProcess) {
        guard master === process else { return }
        master = nil
        forward = nil
        for observer in observers.values { observer() }
    }

    /// Why the master couldn't connect.
    private func failure(_ diagnostics: String, attempt: SSHAttempt) async -> any Error {
        // Questions left unanswered wait for the next sign-in.
        if attempt.cancelled {
            waitingForSignIn = true
            return CancellationError()
        }
        if attempt.unanswered {
            waitingForSignIn = true
            return ClientError.sshSignInRequired
        }
        if var change = SSHHostKeyChange(diagnostics: diagnostics) {
            change.saved = await savedFingerprint(change)
            return ClientError.sshHostKeyChanged(change)
        }
        return ClientError.ssh(Self.lastLine(diagnostics) ?? "ssh couldn’t connect to \(destination).")
    }

    private func savedFingerprint(_ change: SSHHostKeyChange) async -> String? {
        var arguments = ["-l", "-F", change.host]
        if let file = change.file { arguments += ["-f", file] }
        let keygen = SystemSSH(executable: URL(fileURLWithPath: "/usr/bin/ssh-keygen"))
        guard let output = try? await keygen.run(arguments, environment: [:], timeout: .seconds(10)), output.status == 0 else { return nil }
        return SSHHostKeyChange.savedFingerprint(fromLookup: output.stdout)
    }

    private enum MasterState { case absent, running, stale, busy }

    /// Whether a master already holds the control socket, as another copy
    /// of the app's may. A socket no master answers on is stale; one that
    /// doesn't answer in time is busy and left alone.
    private func check(_ control: String) async -> MasterState {
        guard FileManager.default.fileExists(atPath: control) else { return .absent }
        guard let output = try? await environment.runner.run(["-S", control, "-O", "check", "--", destination], environment: [:],
                                                             timeout: .seconds(5)) else { return .busy }
        if output.status == 0 { return .running }
        let refused = ["No such file", "Connection refused"].contains { output.stderr.contains($0) }
        return refused ? .stale : .busy
    }

    /// What Kit takes from the user's configuration for one master: the
    /// account it logs in to, and options Kit overrides. A host whose key
    /// isn't checked is checked, so Kit never trusts a changed key. It is
    /// read for every master, so edits apply to the next connection.
    private func configuration() async throws -> (options: [String], account: String?) {
        let output = try await environment.runner.run(["-G", "--", destination], environment: [:], timeout: .seconds(10))
        guard output.status == 0 else {
            throw ClientError.ssh(Self.lastLine(output.stderr) ?? "ssh can’t read its configuration for \(destination).")
        }
        var values: [String: String] = [:]
        for line in output.stdout.split(separator: "\n") {
            let parts = line.split(separator: " ", maxSplits: 1)
            guard parts.count == 2 else { continue }
            let key = parts[0].lowercased()
            if values[key] == nil { values[key] = String(parts[1]) }
        }
        let options = values["stricthostkeychecking"]?.lowercased() == "false" ? ["-o", "StrictHostKeyChecking=ask"] : []
        let account = values["user"].flatMap { user in values["hostname"].map { user + "@" + $0 } }
        return (options, account)
    }

    // MARK: - The server

    /// Asks the host's Kit for its server's status report, starting the
    /// server first when `start` is set.
    private func query(start: Bool) async throws -> SSHStatusReport {
        // A failed start still reports a running server, such as an older one
        // left after Kit was upgraded on the host.
        let remote = start ? "kit server start >/dev/null; kit server status --json" : "kit server status --json"
        let output = try await environment.runner.run(
            ["-S", try control, "-o", "ControlMaster=no", "-o", "BatchMode=yes", "-n", "-T"] + Self.common
                + ["--", destination, Self.shell(remote)],
            environment: [:], timeout: start ? .seconds(90) : environment.timeout)
        switch output.status {
        case 0:
            do { return try SSHStatusReport.decode(Data(Self.lastOutputLine(output.stdout).utf8)) }
            catch { throw ClientError.ssh("The Kit on \(destination): \(error.localizedDescription).") }
        case 255:
            throw ClientError.ssh(Self.lastLine(output.stderr) ?? "ssh lost the connection to \(destination).")
        case 127:
            throw ClientError.ssh("kit isn’t on PATH on \(destination) for commands run over SSH. Add its directory to PATH in a file your shell reads for non-interactive commands, such as ~/.zshenv or ~/.bashrc.")
        case 2:
            throw ClientError.ssh("The Kit on \(destination) is too old to connect over SSH. Update Kit there.")
        case 1 where !start:
            throw ClientError.sshServerStopped
        default:
            let detail = Self.allLines(output.stderr) ?? "exit status \(output.status)"
            throw ClientError.ssh("Couldn’t start the Kit server on \(destination): \(detail)")
        }
    }

    /// Forwards a free loopback port to the server the report describes.
    private func open(_ report: SSHStatusReport) async throws -> Endpoint {
        let (host, port) = try report.address()
        let remote = host.contains(":") ? "[" + host + "]" : host
        var lastFailure = "ssh couldn’t forward a port to the Kit server on \(destination)."
        for _ in 0..<3 {
            let local = try environment.freePort()
            let spec = "127.0.0.1:\(local):\(remote):\(port)"
            let output = try await environment.runner.run(["-S", try control, "-O", "forward", "-L", spec, "--", destination],
                                                          environment: [:], timeout: .seconds(15))
            if output.status == 0 {
                return Endpoint(url: URL(string: "http://127.0.0.1:\(local)")!, report: report, spec: spec)
            }
            lastFailure = Self.lastLine(output.stderr) ?? lastFailure
            // Another program may have taken the port first.
            guard output.stderr.contains("forwarding failed") || output.stderr.contains("Address already in use") else { break }
        }
        throw ClientError.ssh(lastFailure)
    }

    private func cancel(_ spec: String) async {
        guard let control = try? control else { return }
        _ = try? await environment.runner.run(["-S", control, "-O", "cancel", "-L", spec, "--", destination], environment: [:],
                                              timeout: .seconds(5))
    }

    // MARK: - Helpers

    /// What the master prints once it holds the connection.
    static let readyMarker = "kit-ssh-ready"

    /// Options every connection uses. Kit runs its own commands, whatever
    /// command the user's configuration names for the host.
    static let common = ["-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
                         "-o", "RemoteCommand=none"]

    /// Runs `script` with sh on the host, whatever the user's login shell.
    static func shell(_ script: String) -> String {
        "sh -c '" + script.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    static func freePort() throws -> Int {
        let socket = Darwin.socket(AF_INET, SOCK_STREAM, 0)
        guard socket >= 0 else { throw ClientError.ssh("Kit couldn’t choose a local port.") }
        defer { Darwin.close(socket) }
        var address = sockaddr_in()
        address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        address.sin_family = sa_family_t(AF_INET)
        address.sin_addr.s_addr = inet_addr("127.0.0.1")
        address.sin_port = 0
        var length = socklen_t(MemoryLayout<sockaddr_in>.size)
        let bound = withUnsafeMutablePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { pointer in
                bind(socket, pointer, length) == 0 && getsockname(socket, pointer, &length) == 0
            }
        }
        guard bound else { throw ClientError.ssh("Kit couldn’t choose a local port.") }
        return Int(UInt16(bigEndian: address.sin_port))
    }

    /// The last line a command printed.
    static func lastOutputLine(_ text: String) -> String {
        text.split(separator: "\n", omittingEmptySubsequences: true).last.map { $0.trimmingCharacters(in: .whitespaces) } ?? ""
    }

    /// The last line of ssh's diagnostics, without control characters.
    static func lastLine(_ text: String) -> String? {
        text.split(separator: "\n").map { SSHPrompt.cleaned(String($0)) }.last { !$0.isEmpty }
    }

    /// ssh's diagnostic lines joined, without control characters.
    static func allLines(_ text: String) -> String? {
        let lines = text.split(separator: "\n").map { SSHPrompt.cleaned(String($0)) }.filter { !$0.isEmpty }
        return lines.isEmpty ? nil : lines.joined(separator: "; ")
    }
}

/// One attempt to connect a host's master, answering ssh's questions.
/// A question no one can answer, or one the user cancels, ends the attempt:
/// its answer waits until ssh has been ended, so ssh never sends an empty
/// answer in its place.
final class SSHAttempt: Sendable {
    private struct State {
        var usedSaved = false
        /// How many times each kind of question was asked.
        var asked: [String: Int] = [:]
        var asking = 0
        var lastActivity = ContinuousClock.now
        var unanswered = false
        var cancelled = false
        var finished = false
        /// The user's answer being awaited, cancelled when the attempt ends.
        var prompting: Task<SSHAnswer?, Never>?
        var finishWaiters: [CheckedContinuation<Void, Never>] = []
        /// A password the user chose to save, or nil to leave the saved one.
        var password: SSHAnswer?
        var savedFailed = false
        /// Keys whose passphrases the user chose to keep.
        var keys: Set<String> = []
        var passphrases: [String: String] = [:]
    }

    let destination: String
    /// The account ssh logs in to, `user@hostname`, whose saved password
    /// may be sent; a jump host's question never gets it.
    let account: String?
    private let passwords: any SSHPasswordStore
    private let prompter: SSHPrompter?
    private let state = Mutex(State())

    init(destination: String, account: String? = nil, passwords: any SSHPasswordStore, prompter: SSHPrompter?) {
        (self.destination, self.account, self.passwords, self.prompter) = (destination, account, passwords, prompter)
    }

    /// How long since the last question was answered, or zero while one is open.
    var idle: Duration {
        state.withLock { $0.asking > 0 ? .zero : ContinuousClock.now - $0.lastActivity }
    }
    /// A question went unanswered because no one could be asked.
    var unanswered: Bool { state.withLock { $0.unanswered } }
    /// The user cancelled a question.
    var cancelled: Bool { state.withLock { $0.cancelled } }
    /// The attempt can't go on, so ssh should be ended.
    var gaveUp: Bool { state.withLock { $0.unanswered || $0.cancelled } }

    /// Answers one question: a saved password first, then the user, if
    /// someone can be asked. Nil refuses it.
    func answer(_ prompt: SSHPrompt) async -> String? {
        enum Step { case done, giveUp, ask(retry: Bool, trySaved: Bool) }
        let key = Self.kind(prompt)
        let step: Step = state.withLock { state in
            if state.finished { return .done }
            if state.cancelled || state.unanswered { return .giveUp }
            state.asked[key, default: 0] += 1
            var trySaved = false
            if case .password(let account) = prompt, account == nil || account == self.account {
                if state.usedSaved { state.savedFailed = true } else { trySaved = true }
            }
            return .ask(retry: state.asked[key]! > 1, trySaved: trySaved)
        }
        guard case .ask(let retry, let trySaved) = step else {
            if case .giveUp = step { await finished() }
            return nil
        }
        if trySaved, let saved = passwords.password(for: destination) {
            state.withLock { $0.usedSaved = true }
            return saved
        }
        guard let prompter else { return await giveUp(\.unanswered) }
        let request = SSHPromptRequest(prompt: prompt, retry: retry)
        let task = Task { await prompter(request) }
        let open = state.withLock { state -> Bool in
            guard !state.finished else { return false }
            state.prompting = task
            state.asking += 1
            return true
        }
        guard open else {
            task.cancel()
            return nil
        }
        let answer = await task.value
        let ended = state.withLock { state -> Bool in
            state.asking -= 1
            state.prompting = nil
            state.lastActivity = .now
            return state.finished
        }
        if ended { return nil }
        guard let answer else { return await giveUp(\.cancelled) }
        state.withLock { state in
            switch prompt {
            case .password: state.password = answer
            case .passphrase(let key):
                state.passphrases[key] = answer.text
                if answer.save { state.keys.insert(key) } else { state.keys.remove(key) }
            case .hostKey, .confirm, .other: break
            }
        }
        return answer.text
    }

    /// Ends the attempt for want of an answer, then refuses the question
    /// once ssh has been ended.
    private func giveUp(_ reason: WritableKeyPath<State, Bool>) async -> String? {
        state.withLock { $0[keyPath: reason] = true }
        await finished()
        return nil
    }

    /// Marks the attempt over, as ssh connected or was ended. A question
    /// still on screen is withdrawn, and later questions are refused.
    func finish() {
        let (prompting, waiters) = state.withLock { state in
            state.finished = true
            defer { state.finishWaiters = []; state.prompting = nil }
            return (state.prompting, state.finishWaiters)
        }
        prompting?.cancel()
        waiters.forEach { $0.resume() }
    }

    private func finished() async {
        await withCheckedContinuation { continuation in
            let done = state.withLock { state -> Bool in
                if state.finished { return true }
                state.finishWaiters.append(continuation)
                return false
            }
            if done { continuation.resume() }
        }
    }

    /// Keeps what the user chose to save, now that the answers worked: the
    /// password in Kit's Keychain item, and passphrases where ssh-add keeps them.
    func keep(destination: String, bridge: AskpassBridge,
              addKey: @Sendable (_ key: String, _ environment: [String: String]) async -> Void) async {
        let (password, savedFailed, keys) = state.withLock { state in
            (state.password, state.savedFailed, state.keys.compactMap { key in state.passphrases[key].map { (key, $0) } })
        }
        if let password {
            if password.save { try? passwords.save(password.text, for: destination) }
            else if savedFailed { try? passwords.delete(destination) }
        }
        for (key, passphrase) in keys {
            // ssh-add asks only for this key's passphrase, in its own words,
            // and gets it once: a passphrase it refuses isn't kept.
            let given = Mutex(false)
            let registration = bridge.register { _ in
                given.withLock { given in
                    defer { given = true }
                    return given ? nil : passphrase
                }
            }
            await addKey(key, registration.environment)
            bridge.unregister(registration.nonce)
        }
    }

    private static func kind(_ prompt: SSHPrompt) -> String {
        switch prompt {
        case .hostKey: "hostKey"
        case .password: "password"
        case .passphrase(let key): "passphrase:" + key
        case .confirm(let text): "confirm:" + text
        case .other(let text): "other:" + text
        }
    }
}

/// The app's SSH hosts, one per destination, and what they share.
final class SSHHosts: Sendable {
    static let shared = SSHHosts()

    private let hosts = Mutex<[String: SSHHost]>([:])
    let environment: SSHHost.Environment

    init(environment: SSHHost.Environment = SSHHost.Environment()) {
        self.environment = environment
    }

    /// The host for `destination`, made on first use.
    func host(_ destination: String) -> SSHHost {
        hosts.withLock { hosts in
            if let host = hosts[destination] { return host }
            let host = SSHHost(destination: destination, environment: environment)
            hosts[destination] = host
            return host
        }
    }

    /// Ends the connections to hosts other than `destinations`, as when no
    /// saved server uses them any more.
    func keep(_ destinations: Set<String>) {
        let dropped = hosts.withLock { hosts in
            let dropped = hosts.filter { !destinations.contains($0.key) }
            for key in dropped.keys { hosts.removeValue(forKey: key) }
            return Array(dropped.values)
        }
        for host in dropped { Task { await host.close() } }
    }


    private static let sharedBridge = Mutex<AskpassBridge?>(nil)

    /// The app's askpass bridge, listening once it is first needed.
    static func bridge() throws -> AskpassBridge {
        try sharedBridge.withLock { bridge in
            if let bridge { return bridge }
            // Each copy of the app has its own socket.
            let made = try AskpassBridge(path: try directory() + "/askpass-\(getpid())")
            bridge = made
            return made
        }
    }

    /// A directory for the app's control sockets that only this user can
    /// enter, with a path short enough for a Unix socket.
    static func directory() throws -> String {
        let caches = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first!
        let bundle = Bundle.main.bundleIdentifier ?? "com.akonwi.kit"
        let preferred = caches.appendingPathComponent(bundle).appendingPathComponent("ssh").path
        // A socket path holds 104 bytes, and ssh adds a 17-byte suffix while
        // it creates one beside the 16-character name.
        if preferred.utf8.count <= 64, (try? privateDirectory(preferred)) != nil { return preferred }
        let fallback = "/tmp/kit-ssh-\(getuid())"
        try privateDirectory(fallback)
        return fallback
    }

    /// Makes `path` a directory this user owns and only this user can enter,
    /// refusing one someone else made.
    static func privateDirectory(_ path: String) throws {
        try FileManager.default.createDirectory(atPath: path, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        var info = stat()
        guard lstat(path, &info) == 0, (info.st_mode & S_IFMT) == S_IFDIR, info.st_uid == getuid() else {
            throw ClientError.ssh("Kit can’t use \(path) for SSH connections: another user owns it.")
        }
        if info.st_mode & 0o077 != 0, chmod(path, 0o700) != 0 {
            throw ClientError.ssh("Kit can’t make \(path) private for SSH connections.")
        }
    }
}
