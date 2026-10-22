import Foundation
import Testing
@testable import Kit

struct SSHStatePresentationTests {
    private let connector = ServerConnector.ssh("me@devbox")

    @Test func aChangedHostKeyShowsBothFingerprintsAndTheCommandToRun() {
        let change = SSHHostKeyChange(host: "devbox", saved: "SHA256:pL0q1QfT8vX2kR9mN4bW7sJcYhE3uA6dGiZo5nKx1Vw",
                                      now: "SHA256:Zr7Hc2Wm0aQ5yLx9TfB3nVd8KsJ1gPu4eOi6RtY2bMw")
        let presentation = ServerStatePresentation(name: "devbox", connector: connector, status: .hostKeyChanged(change), sessionTab: true)
        #expect(presentation.title == "devbox’s host key changed")
        #expect(presentation.message == "The key devbox sent doesn’t match the one this Mac saved, so Kit stopped connecting. A reinstalled host does this, but so does someone intercepting the connection.")
        #expect(presentation.versions == [
            .init(label: "Saved", value: "SHA256:pL0q1QfT8vX2kR9mN4bW7sJcYhE3uA6dGiZo5nKx1Vw", outdated: false),
            .init(label: "Now", value: "SHA256:Zr7Hc2Wm0aQ5yLx9TfB3nVd8KsJ1gPu4eOi6RtY2bMw", outdated: true),
        ])
        #expect(presentation.action == .removeHostKey(command: "ssh-keygen -R devbox",
                                                      prompt: "If you expected this, remove the old key in Terminal, then try again:"))
        #expect(presentation.footnote == "Your draft and layout stay in this tab. Tabs on other servers keep working.")
        let unknown = ServerStatePresentation(name: "devbox", connector: connector,
                                              status: .hostKeyChanged(SSHHostKeyChange(host: "devbox", saved: nil, now: "SHA256:new")),
                                              sessionTab: true)
        #expect(unknown.versions == [.init(label: "Now", value: "SHA256:new", outdated: true)])
    }

    @Test func aStoppedServerOffersToStartIt() {
        let presentation = ServerStatePresentation(name: "devbox", connector: connector, status: .serverStopped, sessionTab: true)
        #expect(presentation.title == "devbox’s Kit server stopped")
        #expect(presentation.message == "Kit reached devbox over SSH, but its Kit server isn’t running. Kit doesn’t start it again on its own.")
        #expect(presentation.source == "ssh · me@devbox")
        #expect(presentation.reason == "kit server status: not running")
        #expect(presentation.action == .startServer(note: "Runs kit server start on devbox, as kit attach does."))
    }

    @Test func aServerWaitingForSignInOffersIt() {
        let presentation = ServerStatePresentation(name: "devbox", connector: connector, status: .signInRequired, sessionTab: false)
        #expect(presentation.title == "Sign in to devbox")
        #expect(presentation.message == "devbox asked for something only you can answer, such as a password. Kit doesn’t ask while you’re elsewhere; sign in to keep connecting.")
        #expect(presentation.reason == nil)
        #expect(presentation.action == .signIn)
        #expect(presentation.footnote == "Tabs on other servers keep working.")
    }

    @Test func anUnreachableSSHServerSaysSSHAndShowsSSHsError() {
        let presentation = ServerStatePresentation(name: "old-mini", connector: .ssh("akonwi@old-mini.local"),
                                                   status: .unreachable(reason: "ssh: connect to host old-mini.local port 22: Operation timed out", retry: nil),
                                                   sessionTab: true)
        #expect(presentation.message == "Kit couldn’t reach old-mini over SSH. It keeps trying, and this tab picks up where it left off when old-mini is back.")
        #expect(presentation.source == "ssh · akonwi@old-mini.local")
        #expect(presentation.reason == "ssh: connect to host old-mini.local port 22: Operation timed out")
    }

    @Test func rowsAndLauncherSayWhatAnSSHServerNeeds() {
        let change = SSHHostKeyChange(host: "devbox", saved: nil, now: "SHA256:new")
        let rows = [ServerStatus.signInRequired, .hostKeyChanged(change), .serverStopped].map {
            ServerRowPresentation(name: "devbox", connector: connector, status: $0)
        }
        #expect(rows == [
            ServerRowPresentation(name: "devbox", detail: "SSH · me@devbox", status: "Sign in to connect", tone: .warning, action: .signIn),
            ServerRowPresentation(name: "devbox", detail: "SSH · me@devbox", status: "Host key changed", tone: .warning, action: .menu),
            ServerRowPresentation(name: "devbox", detail: "SSH · me@devbox", status: "Server stopped", tone: .hollow, action: .menu),
        ])
        let options = [ServerStatus.signInRequired, .hostKeyChanged(change), .serverStopped].map {
            LauncherServerOption(name: "devbox", connector: connector, status: $0)
        }
        #expect(options == [
            LauncherServerOption(name: "devbox", detail: "Sign in", address: false, available: true, needsAttention: true),
            LauncherServerOption(name: "devbox", detail: "Host key changed", address: false, available: false),
            LauncherServerOption(name: "devbox", detail: "Server stopped", address: false, available: true, needsAttention: true),
        ])
        // A new launcher never opens on a server that can't list sessions.
        #expect(!LauncherServerOption.lists(.signInRequired))
        #expect(!LauncherServerOption.lists(.serverStopped))
    }
}

struct SSHPromptPresentationTests {
    @Test func aNewHostKeyAsksToConnect() {
        let key = SSHHostKey(host: "devbox (10.0.4.12)", type: "ED25519", fingerprint: "SHA256:pL0q1QfT8vX2kR9mN4bW7sJcYhE3uA6dGiZo5nKx1Vw")
        let sheet = SSHPromptPresentation(server: "devbox", destination: "me@devbox", request: .init(prompt: .hostKey(key)))
        #expect(sheet.title == "Connect to devbox?")
        #expect(sheet.message == "This Mac hasn’t connected to devbox before. Check that its key matches before you connect.")
        #expect(sheet.key?.label == "ED25519 key · devbox (10.0.4.12)")
        #expect(sheet.key?.fingerprint == "SHA256:pL0q1QfT8vX2kR9mN4bW7sJcYhE3uA6dGiZo5nKx1Vw")
        #expect(sheet.field == .none)
        #expect(sheet.save == nil)
        #expect(sheet.action == "Connect")
        #expect(sheet.fixedAnswer == "yes")
    }

    @Test func aPasswordIsntSavedUnlessChosen() {
        let sheet = SSHPromptPresentation(server: "devbox", destination: "me@devbox", request: .init(prompt: .password(account: "me@devbox")))
        #expect(sheet.title == "Password for me@devbox")
        #expect(sheet.message == "devbox asked for a password. Kit sends it to ssh and doesn’t keep it unless you choose to.")
        #expect(sheet.field == .secure)
        #expect(sheet.save == false)
        #expect(sheet.action == "Connect")
        #expect(sheet.error == nil)
        let again = SSHPromptPresentation(server: "devbox", destination: "me@devbox",
                                          request: .init(prompt: .password(account: nil), retry: true))
        #expect(again.title == "Password for me@devbox")
        #expect(again.error == "That password didn’t work. Try again.")
    }

    @Test func aPassphraseIsSavedUnlessDeclined() {
        let user = FileManager.default.homeDirectoryForCurrentUser.path
        let sheet = SSHPromptPresentation(server: "devbox", destination: "me@devbox",
                                          request: .init(prompt: .passphrase(key: user + "/.ssh/id_ed25519")))
        #expect(sheet.title == "Unlock your SSH key")
        #expect(sheet.message == "devbox needs ~/.ssh/id_ed25519, which is locked with a passphrase.")
        #expect(sheet.field == .secure)
        #expect(sheet.save == true)
        #expect(sheet.action == "Unlock")
    }

    @Test func otherQuestionsShowTheHostsWordsAndAreNeverSaved() {
        let code = SSHPromptPresentation(server: "devbox", destination: "me@devbox",
                                         request: .init(prompt: SSHPrompt("(me@devbox) Verification code: ")))
        #expect(code.title == "devbox is asking")
        #expect(code.message == "Answer the host’s question to keep connecting.")
        #expect(code.label == "Verification code:")
        #expect(code.field == .plain)
        #expect(code.save == nil)
        #expect(code.action == "Continue")
        let pin = SSHPromptPresentation(server: "devbox", destination: "me@devbox",
                                        request: .init(prompt: .other("Enter PIN for ECDSA-SK key /Users/me/.ssh/id_ecdsa_sk:")))
        #expect(pin.field == .secure)
    }
}

struct SSHPairingTests {
    @Test func anSSHServersLinkUsesTheURLTheUserGave() {
        #expect(PairDeviceDialog.servedURL(" https://Devbox.tail1a2b.ts.net/ ") == "https://devbox.tail1a2b.ts.net")
        #expect(PairDeviceDialog.servedURL("http://127.0.0.1:4517") == nil)
        #expect(PairDeviceDialog.servedURL("devbox") == nil)
        let addresses = PairingAddresses(connector: .ssh("me@devbox"), reported: ["http://127.0.0.1:4517", "http://100.64.0.7:4517"],
                                         given: "https://devbox.tail1a2b.ts.net")
        #expect(addresses.addresses.map(\.url) == ["https://devbox.tail1a2b.ts.net", "http://100.64.0.7:4517"])
    }

    @Test func addServerSaysKitStartsAnSSHHostsServer() {
        #expect(AddServerHint("me@gpu-01.lab", location: .ssh("me@gpu-01.lab"))?.text
                == "SSH host. Kit starts its server there if it isn’t running.")
    }
}
