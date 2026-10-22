import CryptoKit
import Foundation
import Synchronization
import Testing
@testable import Kit

struct SSHStatusReportTests {
    private static let valid = FakeSSH.report()

    @Test func readsAReportAndIgnoresNewFields() throws {
        let report = try SSHStatusReport.decode(Data(Self.valid.utf8))
        #expect(report == SSHStatusReport(home: "/home/me/.kit", pid: 7, url: "http://127.0.0.1:61342", kitVersion: "0.42.0",
                                          protocolVersion: kitWireVersion, instanceId: "instance_remote", databaseReady: true,
                                          token: "secret"))
        let address = try report.address()
        #expect(address.host == "127.0.0.1")
        #expect(address.port == 61342)
        #expect(try SSHStatusReport.decode(Data(FakeSSH.report(url: "http://[::1]:4000").utf8)).address().host == "::1")
    }

    @Test func refusesReportsAServerWouldNeverPrint() {
        let changes: [(String, String)] = [
            (#""http://127.0.0.1:61342""#, #""https://127.0.0.1:61342""#),
            (#""http://127.0.0.1:61342""#, #""http://127.0.0.1""#),
            (#""http://127.0.0.1:61342""#, #""http://127.0.0.1:61342/x""#),
            (#""http://127.0.0.1:61342""#, #""http://-oProxy:1""#),
            (#""http://127.0.0.1:61342""#, #""http://10.0.0.5:61342""#),
            (#""kitVersion":"0.42.0""#, #""kitVersion":"0.42\u0085""#),
            (#""kitVersion":"0.42.0""#, #""kitVersion":"0.42\u001b""#),
            (#""token":"secret""#, #""token":"""#),
            (#""pid":7"#, #""pid":0"#),
        ]
        for (from, to) in changes {
            #expect(throws: SSHReportError.self, "\(to)") {
                try SSHStatusReport.decode(Data(Self.valid.replacingOccurrences(of: from, with: to).utf8))
            }
        }
        #expect(throws: SSHReportError.self) { try SSHStatusReport.decode(Data(repeating: 0x20, count: SSHStatusReport.maxBytes + 1)) }
    }
}

struct SSHPromptTests {
    @Test func classifiesOpenSSHQuestions() {
        let hostKey = """
        The authenticity of host 'devbox (10.0.4.12)' can't be established.
        ED25519 key fingerprint is SHA256:pL0q1QfT8vX2kR9mN4bW7sJcYhE3uA6dGiZo5nKx1Vw.
        This key is not known by any other names.
        Are you sure you want to continue connecting (yes/no/[fingerprint])? 
        """
        #expect(SSHPrompt(hostKey) == .hostKey(SSHHostKey(host: "devbox (10.0.4.12)", type: "ED25519",
                                                         fingerprint: "SHA256:pL0q1QfT8vX2kR9mN4bW7sJcYhE3uA6dGiZo5nKx1Vw")))
        // OpenSSH 10.3, word for word.
        let openSSH10 = """
        The authenticity of host 'kit-ssh-test.exe.xyz (161.210.94.99)' can't be established.
        RSA key fingerprint is: SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo
        This host key is known by the following other names/addresses:
            ~/.ssh/known_hosts:5: exe.dev
        Are you sure you want to continue connecting (yes/no/[fingerprint])? 
        """
        #expect(SSHPrompt(openSSH10) == .hostKey(SSHHostKey(host: "kit-ssh-test.exe.xyz (161.210.94.99)", type: "RSA",
                                                           fingerprint: "SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo")))
        #expect(SSHPrompt("me@devbox's password: ") == .password(account: "me@devbox"))
        #expect(SSHPrompt("(me@devbox) Password: ") == .password(account: "me@devbox"))
        #expect(SSHPrompt("Password:") == .password(account: nil))
        #expect(SSHPrompt("Enter passphrase for key '/Users/me/.ssh/id_ed25519': ") == .passphrase(key: "/Users/me/.ssh/id_ed25519"))
        let code = SSHPrompt("(me@devbox) Verification code: ")
        #expect(code == .other("(me@devbox) Verification code:"))
        #expect(code.display == "Verification code:")
        #expect(SSHPrompt("Allow use of key id_ed25519?", kind: "confirm") == .confirm("Allow use of key id_ed25519?"))
        // A host's text can't move the cursor, reorder, or hide what follows.
        #expect(SSHPrompt("Code\u{1b}[2J: ") == .other("Code[2J:"))
        #expect(SSHPrompt("Code\u{202E}321: ") == .other("Code321:"))
        // A host's keyboard-interactive question can't pass for ssh's host key question.
        let fake = "(me@devbox) The authenticity of host 'bank (1.2.3.4)' can't be established.\nED25519 key fingerprint is SHA256:evil.\nAre you sure you want to continue connecting (yes/no)? "
        #expect(SSHPrompt(fake) == .other(SSHPrompt.cleaned(fake)))
    }

    @Test func onlyPasswordsAndPassphrasesCanBeSaved() {
        #expect(SSHPrompt.password(account: nil).saveable)
        #expect(SSHPrompt.passphrase(key: "k").saveable)
        #expect(!SSHPrompt.other("Verification code:").saveable)
        #expect(!SSHPrompt.hostKey(SSHHostKey(host: "h", type: "ED25519", fingerprint: "SHA256:x")).saveable)
    }

    @Test func readsAChangedHostKeyAndHowToRemoveTheOldOne() {
        let user = FileManager.default.homeDirectoryForCurrentUser.path
        let change = SSHHostKeyChange(diagnostics: Self.changed(host: "devbox", file: user + "/.ssh/known_hosts"))
        #expect(change == SSHHostKeyChange(host: "devbox", saved: nil, now: "SHA256:Zr7Hc2Wm0aQ5yLx9TfB3nVd8KsJ1gPu4eOi6RtY2bMw"))
        #expect(change?.removeCommand == "ssh-keygen -R devbox")
        let other = SSHHostKeyChange(diagnostics: Self.changed(host: "[devbox]:2222", file: "/etc/ssh/known hosts"))
        #expect(other?.removeCommand == "ssh-keygen -f '/etc/ssh/known hosts' -R '[devbox]:2222'")
        // OpenSSH 10.3, word for word, which doesn't say how to remove the key.
        let openSSH10 = SSHHostKeyChange(diagnostics: """
        @@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
        @    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
        @@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
        IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
        Someone could be eavesdropping on you right now (man-in-the-middle attack)!
        It is also possible that a host key has just been changed.
        The fingerprint for the RSA key sent by the remote host is
        SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo.
        Please contact your system administrator.
        Add correct host key in /tmp/kh-wrong to get rid of this message.
        Offending RSA key in /tmp/kh-wrong:1
        Host key for kit-ssh-test.exe.xyz has changed and you have requested strict checking.
        Host key verification failed.

        """)
        #expect(openSSH10 == SSHHostKeyChange(host: "kit-ssh-test.exe.xyz", saved: nil,
                                              now: "SHA256:JJOP/lwiBGOMilfONPWZCXUrfK154cnJFXcqlsi6lPo", file: "/tmp/kh-wrong"))
        #expect(openSSH10?.removeCommand == "ssh-keygen -f /tmp/kh-wrong -R kit-ssh-test.exe.xyz")
        #expect(SSHHostKeyChange(diagnostics: "ssh: connect to host devbox port 22: Operation timed out") == nil)
        #expect(SSHHostKeyChange.savedFingerprint(fromLookup: "# Host devbox found: line 12\ndevbox ED25519 SHA256:pL0q1Qf\n")
                == "SHA256:pL0q1Qf")
    }

    static func changed(host: String, file: String) -> String {
        """
        @@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
        @    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
        @@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
        IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
        Someone could be eavesdropping on you right now (man-in-the-middle attack)!
        It is also possible that a host key has just been changed.
        The fingerprint for the ED25519 key sent by the remote host is
        SHA256:Zr7Hc2Wm0aQ5yLx9TfB3nVd8KsJ1gPu4eOi6RtY2bMw.
        Please contact your system administrator.
        Add correct host key in \(file) to get rid of this message.
        Offending ED25519 key in \(file):12
          remove with:
          ssh-keygen -f '\(file)' -R '\(host)'
        Host key for \(host) has changed and you have requested strict checking.
        Host key verification failed.

        """
    }
}

struct AskpassBridgeTests {
    @Test func answersOnlyTheAttemptThatAsked() async throws {
        let directory = "/tmp/kt-" + UUID().uuidString.prefix(8)
        try SSHHosts.privateDirectory(directory)
        defer { try? FileManager.default.removeItem(atPath: directory) }
        let bridge = try AskpassBridge(path: directory + "/a")
        defer { bridge.stop() }
        let asked = Mutex<[SSHPrompt]>([])
        let registration = bridge.register { prompt in
            asked.withLock { $0.append(prompt) }
            return "hunter2"
        }
        #expect(registration.environment["SSH_ASKPASS_REQUIRE"] == "force")
        #expect(registration.environment[Askpass.socketVariable] == directory + "/a")
        let socket = directory + "/a"
        let answer = await askOffThePool(socket: socket, request: .init(nonce: registration.nonce, prompt: "me@devbox's password: ", kind: nil))
        #expect(answer == "hunter2")
        #expect(asked.withLock { $0 } == [.password(account: "me@devbox")])
        let stranger = await askOffThePool(socket: socket, request: .init(nonce: String(repeating: "0", count: 64), prompt: "Password:", kind: nil))
        #expect(stranger == nil)
        bridge.unregister(registration.nonce)
        let late = await askOffThePool(socket: socket, request: .init(nonce: registration.nonce, prompt: "Password:", kind: nil))
        #expect(late == nil)
        #expect(asked.withLock { $0.count } == 1)
    }

    @Test func theHelperRunsOnlyWhenSSHAsks() {
        #expect(Askpass.runIfRequested(environment: [:], arguments: ["Kit"]) == nil)
        // A notice, such as touching a security key, needs no answer.
        #expect(Askpass.runIfRequested(environment: [Askpass.socketVariable: "/nonexistent", Askpass.nonceVariable: "n",
                                                     "SSH_ASKPASS_PROMPT": "none"], arguments: ["Kit", "Touch your key"]) == 0)
        #expect(Askpass.runIfRequested(environment: [Askpass.socketVariable: "/nonexistent", Askpass.nonceVariable: "n"],
                                       arguments: ["Kit", "Password:"]) == 1)
    }
}

struct SSHHostTests {
    static let health = """
    {"instanceId":"instance_remote","pid":7,"kitVersion":"0.42.0","protocolVersion":\(kitWireVersion),"databaseReady":true,
     "providers":[],"serverId":"server_\(String(repeating: "0123456789abcdef", count: 2))","machineId":"\(String(repeating: "ab", count: 32))",
     "home":"/home/me"}
    """

    @Test func aBackgroundConnectionForwardsToTheServerWithoutAsking() async throws {
        let server = try LoopbackHTTPServer(body: Self.health)
        let url = try await server.start()
        defer { server.stop() }
        let fake = try FakeSSHHosts(port: url.port!)
        defer { fake.remove() }
        let (client, identity) = try await ServerConnector.ssh("me@devbox").handshake(serverID: "devbox", hosts: fake.hosts)
        #expect(client.endpoint == url)
        #expect(identity.instance == "instance_remote")
        #expect(identity.home == "/home/me")
        let request = try #require(server.requests.first)
        #expect(request.hasPrefix("GET /v1/health HTTP/1.1\r\n"))
        #expect(request.contains("\r\nAuthorization: Bearer secret\r\n"))
        // One master, which runs nothing on the host but waits for its input to close.
        let master = try #require(fake.ssh.starts.first)
        #expect(fake.ssh.starts.count == 1)
        #expect(Array(master.prefix(6)) == ["-M", "-S", fake.directory + "/" + Self.digest("me@devbox"), "-o", "ControlPersist=no", "-T"])
        #expect(Array(master.suffix(3)) == ["--", "me@devbox", "sh -c 'echo kit-ssh-ready; read line'"])
        // The query shares the master, never starts the server, and never prompts.
        let query = try #require(fake.ssh.runs.first { $0.last?.contains("kit server status") == true })
        #expect(query.contains("BatchMode=yes"))
        #expect(query.suffix(2) == ["me@devbox", "sh -c 'kit server status --json'"])
        let forward = try #require(fake.ssh.runs.first { $0.contains("forward") })
        #expect(forward == ["-S", fake.directory + "/" + Self.digest("me@devbox"), "-O", "forward",
                            "-L", "127.0.0.1:\(url.port!):127.0.0.1:61342", "--", "me@devbox"])
        // Checking again reuses the master and the forward.
        _ = try await ServerConnector.ssh("me@devbox").handshake(serverID: "devbox", hosts: fake.hosts)
        #expect(fake.ssh.starts.count == 1)
        #expect(fake.ssh.runs.filter { $0.contains("forward") }.count == 1)
    }

    @Test func aBackgroundConnectionNeverAsksAndThenWaitsForSignIn() async throws {
        let sent = Recorded<String>()
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@devbox's password: ", expected: "hunter2", sent: sent)))
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        await expectClientError(.sshSignInRequired) { _ = try await host.resolve() }
        #expect(await host.needsSignIn)
        // ssh was ended before it could send an empty password for the question.
        #expect(sent.all == [])
        // Until someone signs in, checks don't run ssh again.
        await expectClientError(.sshSignInRequired) { _ = try await host.resolve() }
        #expect(fake.ssh.starts.count == 1)
    }

    @Test func signingInAsksAndKeepsOnlyWhatTheUserChoseToSave() async throws {
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@devbox's password: ", expected: "hunter2")))
        defer { fake.remove() }
        let asked = Mutex<[SSHPromptRequest]>([])
        let answers = Mutex(["wrong", "hunter2"])
        let endpoint = try await fake.hosts.host("me@devbox").signIn { request in
            asked.withLock { $0.append(request) }
            return SSHAnswer(text: answers.withLock { $0.removeFirst() }, save: true)
        }
        #expect(endpoint.report.instanceId == "instance_remote")
        #expect(asked.withLock { $0 } == [SSHPromptRequest(prompt: .password(account: "me@devbox")),
                                          SSHPromptRequest(prompt: .password(account: "me@devbox"), retry: true)])
        #expect(fake.passwords.all == ["me@devbox": "hunter2"])
    }

    @Test func aSavedPasswordConnectsInTheBackground() async throws {
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@devbox's password: ", expected: "hunter2")),
                                    passwords: ["me@devbox": "hunter2"])
        defer { fake.remove() }
        let endpoint = try await fake.hosts.host("me@devbox").resolve()
        #expect(endpoint.url.absoluteString == "http://127.0.0.1:1")
    }

    @Test func aPasswordNotSavedIsForgottenAndAWrongSavedOneIsDropped() async throws {
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@devbox's password: ", expected: "hunter2")),
                                    passwords: ["me@devbox": "stale"])
        defer { fake.remove() }
        let asked = Mutex<[SSHPromptRequest]>([])
        try await fake.hosts.host("me@devbox").signIn { request in
            asked.withLock { $0.append(request) }
            return SSHAnswer(text: "hunter2")
        }
        // The saved password was tried first, so the user hears it failed.
        #expect(asked.withLock { $0 } == [SSHPromptRequest(prompt: .password(account: "me@devbox"), retry: true)])
        #expect(fake.passwords.all.isEmpty)
    }

    @Test func aKeptPassphraseGoesToTheAgentAndTheKeychainThroughSSHAdd() async throws {
        let key = "/Users/me/.ssh/id_ed25519"
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("Enter passphrase for key '\(key)': ", expected: "open sesame")))
        defer { fake.remove() }
        try await fake.hosts.host("me@devbox").signIn { _ in SSHAnswer(text: "open sesame", save: true) }
        #expect(fake.added.all == [key + "=open sesame"])
        #expect(fake.passwords.all.isEmpty)
    }

    @Test func otherAnswersAreNeverKept() async throws {
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("(me@devbox) Verification code: ", expected: "482913")))
        defer { fake.remove() }
        try await fake.hosts.host("me@devbox").signIn { _ in SSHAnswer(text: "482913", save: true) }
        #expect(fake.passwords.all.isEmpty)
        #expect(fake.added.all.isEmpty)
    }

    @Test func cancellingAQuestionStopsConnectingAndLeavesTheHostWaiting() async throws {
        let sent = Recorded<String>()
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@devbox's password: ", expected: "hunter2", sent: sent)))
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        let asked = Recorded<SSHPromptRequest>()
        await #expect(throws: CancellationError.self) {
            try await host.signIn { request in
                asked.append(request)
                return nil
            }
        }
        #expect(await host.needsSignIn)
        #expect(asked.all.count == 1)
        #expect(sent.all == [])
    }

    @Test func aQuestionOnScreenIsWithdrawnWhenSSHGivesUp() async throws {
        // ssh exits on its own, as when the host's login grace time runs out.
        let asked = Recorded<Bool>()
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: { process, environment in
            async let answer = process.ask("me@devbox's password: ", kind: nil, environment: environment)
            while asked.all.isEmpty { try? await Task.sleep(for: .milliseconds(10)) }
            process.exit(255, diagnostics: "Connection closed by 10.0.4.12 port 22\n")
            _ = await answer
        }))
        defer { fake.remove() }
        let withdrawn = Recorded<Bool>()
        await expectClientError(.ssh("Connection closed by 10.0.4.12 port 22")) {
            _ = try await fake.hosts.host("me@devbox").signIn { _ in
                asked.append(true)
                // The user never answers; the question waits until it's withdrawn.
                try? await Task.sleep(for: .seconds(60))
                withdrawn.append(Task.isCancelled)
                return nil
            }
        }
        try await until { !withdrawn.all.isEmpty }
        #expect(withdrawn.all == [true])
    }

    @Test func socketsOfExitedCopiesAreRemoved() throws {
        let fake = try FakeSSHHosts()
        defer { fake.remove() }
        // A pid no process has: pids stay below 100000 on macOS.
        let exited = fake.directory + "/askpass-99999999"
        let running = fake.directory + "/askpass-\(getppid())"
        let control = fake.directory + "/d0e68efce23aa758"
        for path in [exited, running, control] { FileManager.default.createFile(atPath: path, contents: nil) }
        SSHHosts.removeAbandonedBridges(in: fake.directory)
        #expect(try FileManager.default.contentsOfDirectory(atPath: fake.directory).sorted()
                == ["a", "askpass-\(getppid())", "d0e68efce23aa758"])
    }

    @Test func aSavedPasswordGoesOnlyToItsAccount() async throws {
        let sent = Recorded<String>()
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@bastion's password: ", expected: "jump", sent: sent)),
                                    passwords: ["me@devbox": "hunter2"])
        defer { fake.remove() }
        // A jump host asking in the background gets nothing.
        await expectClientError(.sshSignInRequired) { _ = try await fake.hosts.host("me@devbox").resolve() }
        #expect(sent.all == [])
    }

    @Test func sshAddIsGivenThePassphraseOnce() async throws {
        let key = "/Users/me/.ssh/id_ed25519"
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("Enter passphrase for key '\(key)': ", expected: "open sesame")),
                                    keyAsks: 2)
        defer { fake.remove() }
        try await fake.hosts.host("me@devbox").signIn { _ in SSHAnswer(text: "open sesame", save: true) }
        // ssh-add refused it, and isn't given it again.
        #expect(fake.added.all == [key + "=open sesame", key + "="])
    }

    @Test func aBackgroundConnectionSkipsALockedKeyAndSignsInAnotherWay() async throws {
        let key = "/Users/me/.ssh/id_work"
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: { process, environment in
            // ssh skips a key whose passphrase is refused and tries the next.
            _ = await process.ask("Enter passphrase for key '\(key)': ", kind: nil, environment: environment)
            process.emit(SSHHost.readyMarker)
        }))
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        _ = try await host.resolve()
        #expect(!(await host.needsSignIn))
    }

    @Test func aBackgroundHostKeyQuestionWaitsForSignIn() async throws {
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: { process, environment in
            let question = "The authenticity of host 'devbox (10.0.4.12)' can't be established.\nED25519 key fingerprint is SHA256:abc.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? "
            _ = await process.ask(question, kind: nil, environment: environment)
            process.exit(255, diagnostics: "Host key verification failed.\n")
        }))
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        await expectClientError(.sshSignInRequired) { _ = try await host.resolve() }
        #expect(await host.needsSignIn)
    }

    @Test func closingAHostWithdrawsItsQuestion() async throws {
        let sent = Recorded<String>()
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@devbox's password: ", expected: "hunter2", sent: sent)))
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        let asked = Recorded<Bool>()
        let withdrawn = Recorded<Bool>()
        async let signingIn: Void = {
            await #expect(throws: CancellationError.self) {
                try await host.signIn { _ in
                    asked.append(true)
                    try? await Task.sleep(for: .seconds(60))
                    withdrawn.append(Task.isCancelled)
                    return nil
                }
            }
        }()
        try await until { !asked.all.isEmpty }
        await host.close()
        await signingIn
        try await until { !withdrawn.all.isEmpty }
        #expect(withdrawn.all == [true])
        // ssh was ended before it could send an empty password.
        #expect(sent.all == [])
    }

    @Test func closingAHostWhileItConnectsEndsTheNewMaster() async throws {
        let gate = Recorded<FakeSSHProcess>()
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: { process, _ in
            gate.append(process)
            try? await Task.sleep(for: .milliseconds(200))
            process.emit(SSHHost.readyMarker)
        }))
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        async let connecting: Void = { _ = try? await host.resolve() }()
        try await Task.sleep(for: .milliseconds(50))
        await host.close()
        await connecting
        let master = try #require(gate.all.first)
        #expect(!master.running)
        await #expect(throws: CancellationError.self) { _ = try await host.resolve() }
    }

    @Test func aChangedHostKeyIsReportedWithBothFingerprintsAndNeverTrusted() async throws {
        let diagnostics = SSHPromptTests.changed(host: "devbox", file: "/tmp/kit-test-known-hosts")
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.changedKey(diagnostics)))
        defer { fake.remove() }
        do {
            _ = try await fake.hosts.host("me@devbox").signIn { _ in
                Issue.record("A changed key is never asked about")
                return SSHAnswer(text: "yes")
            }
            Issue.record("Expected a changed host key")
        } catch ClientError.sshHostKeyChanged(let change) {
            #expect(change.host == "devbox")
            #expect(change.now == "SHA256:Zr7Hc2Wm0aQ5yLx9TfB3nVd8KsJ1gPu4eOi6RtY2bMw")
            #expect(change.removeCommand == "ssh-keygen -f /tmp/kit-test-known-hosts -R devbox")
        }
    }

    @Test func aHostThatDoesntCheckKeysIsCheckedForKit() async throws {
        let fake = try FakeSSHHosts(ssh: FakeSSH(respond: FakeSSH.standard(config: "user me\nstricthostkeychecking false\n")))
        defer { fake.remove() }
        _ = try await fake.hosts.host("me@devbox").resolve()
        let master = try #require(fake.ssh.starts.first)
        #expect(master.contains("StrictHostKeyChecking=ask"))
    }

    @Test func aStoppedServerIsReportedAndStartedOnlyWhenAsked() async throws {
        let ssh = FakeSSH(respond: FakeSSH.standard { start in
            start ? SSHOutput(status: 0, stdout: "Kit server started\n" + FakeSSH.report(), stderr: "")
                : SSHOutput(status: 1, stdout: "", stderr: "")
        })
        let fake = try FakeSSHHosts(ssh: ssh)
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        await expectClientError(.sshServerStopped) { _ = try await host.resolve() }
        #expect(!ssh.runs.contains { $0.last?.contains("kit server start") == true })
        let endpoint = try await host.signIn(start: true) { _ in nil }
        #expect(endpoint.report.pid == 7)
        #expect(ssh.runs.contains { $0.last == "sh -c 'kit server start >/dev/null; kit server status --json'" })
    }

    @Test func explainsAHostWithoutAUsableKit() async throws {
        for (status, message) in [
            (Int32(127), "kit isn’t on PATH on me@devbox for commands run over SSH. Add its directory to PATH in a file your shell reads for non-interactive commands, such as ~/.zshenv or ~/.bashrc."),
            (2, "The Kit on me@devbox is too old to connect over SSH. Update Kit there."),
            (255, "ssh: connect to host devbox port 22: Operation timed out"),
        ] {
            let fake = try FakeSSHHosts(ssh: FakeSSH(respond: FakeSSH.standard { _ in
                SSHOutput(status: status, stdout: "", stderr: "debug\nssh: connect to host devbox port 22: Operation timed out\n")
            }))
            defer { fake.remove() }
            await expectClientError(.ssh(message)) { _ = try await fake.hosts.host("me@devbox").resolve() }
        }
    }

    @Test func anIncompatibleServerIsNotForwarded() async throws {
        let ssh = FakeSSH(respond: FakeSSH.standard { _ in
            SSHOutput(status: 0, stdout: FakeSSH.report(protocolVersion: kitWireVersion - 1), stderr: "")
        })
        let fake = try FakeSSHHosts(ssh: ssh)
        defer { fake.remove() }
        await expectClientError(.incompatibleDaemon(daemonVersion: "0.42.0", appProtocol: kitWireVersion,
                                                    daemonProtocol: kitWireVersion - 1, reason: .daemonProtocolOlder)) {
            _ = try await fake.hosts.host("me@devbox").resolve()
        }
        #expect(!ssh.runs.contains { $0.contains("forward") })
    }

    @Test func aRestartedServerGetsANewForward() async throws {
        let instance = Mutex("instance_one")
        let ssh = FakeSSH(respond: FakeSSH.standard { _ in
            SSHOutput(status: 0, stdout: FakeSSH.report(instance: instance.withLock { $0 }), stderr: "")
        })
        let fake = try FakeSSHHosts(ssh: ssh)
        defer { fake.remove() }
        let host = fake.hosts.host("me@devbox")
        let first = try await host.resolve()
        instance.withLock { $0 = "instance_two" }
        let second = try await host.resolve()
        #expect(second.report.instanceId == "instance_two")
        #expect(ssh.runs.contains { $0.contains("cancel") && $0.contains(first.spec) })
        #expect(ssh.runs.filter { $0.contains("forward") }.count == 2)
    }

    private static func digest(_ destination: String) -> String {
        SHA256Hex.prefix(destination)
    }
}

private enum SHA256Hex {
    static func prefix(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
    }
}

@MainActor struct SSHServerConnectionTests {
    @Test func aServerWaitingForSignInIsntCheckedUntilSomeoneSignsIn() async throws {
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: FakeSSH.asking("me@devbox's password: ", expected: "hunter2")))
        defer { fake.remove() }
        let server = ServerConnection(name: "devbox", connector: .ssh("me@devbox"), tokens: MemoryDeviceTokens(), hosts: fake.hosts,
                                      schedule: .init(firstRetry: .milliseconds(10), maximumRetry: .milliseconds(10), check: .milliseconds(10)))
        server.start()
        try await until { server.status == .signInRequired }
        try await Task.sleep(for: .milliseconds(100))
        #expect(fake.ssh.starts.count == 1)
        // Cancelling the question leaves it waiting.
        await server.signIn { _ in nil }
        #expect(server.status == .signInRequired)
        server.stop()
    }

    @Test func aDroppedConnectionForgetsItsForwardAndReconnectsWithoutAsking() async throws {
        let server = try LoopbackHTTPServer(body: SSHHostTests.health)
        let url = try await server.start()
        defer { server.stop() }
        let masters = Recorded<FakeSSHProcess>()
        let fake = try FakeSSHHosts(ssh: FakeSSH(master: { process, _ in
            masters.append(process)
            process.emit(SSHHost.readyMarker)
        }), port: url.port!)
        defer { fake.remove() }
        let connection = ServerConnection(name: "devbox", connector: .ssh("me@devbox"), tokens: MemoryDeviceTokens(), hosts: fake.hosts,
                                          schedule: .init(firstRetry: .seconds(60), maximumRetry: .seconds(60), check: .seconds(60)))
        connection.start()
        try await until { connection.status.isConnected }
        #expect(connection.client.address.verifiedClient(for: connection.connector) != nil)
        try #require(masters.all.first).exit(255, diagnostics: "Connection to devbox closed by remote host.\n")
        try await until { masters.all.count == 2 }
        try await until { connection.status.isConnected }
        connection.stop()
    }

    @Test func aStoppedServerIsCheckedButNotStarted() async throws {
        let ssh = FakeSSH(respond: FakeSSH.standard { _ in SSHOutput(status: 1, stdout: "", stderr: "") })
        let fake = try FakeSSHHosts(ssh: ssh)
        defer { fake.remove() }
        let server = ServerConnection(name: "devbox", connector: .ssh("me@devbox"), tokens: MemoryDeviceTokens(), hosts: fake.hosts,
                                      schedule: .init(firstRetry: .milliseconds(10), maximumRetry: .milliseconds(10), check: .milliseconds(10)))
        server.start()
        try await until { server.status == .serverStopped }
        try await until { ssh.runs.filter { $0.last?.contains("kit server status") == true }.count >= 3 }
        #expect(!ssh.runs.contains { $0.last?.contains("kit server start") == true })
        server.stop()
    }

}
