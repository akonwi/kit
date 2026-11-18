import Foundation
import KitClient
import Testing
@testable import Kit

/// A shell command typed in the composer: ! makes it one, !! hides its
/// output from the agent, and Send runs it in the session's folder.
@MainActor
struct ShellComposerTests {
    private func type(_ text: String, into composer: SessionComposer) {
        composer.draft = text
        composer.takeShellPrefix()
    }

    @Test func anExclamationMarkMakesTheDraftAShellCommand() {
        let composer = SessionComposerTests.composer()
        type("!", into: composer)
        #expect(composer.shell == .seen)
        #expect(composer.draft == "")
        #expect(composer.action == .send(enabled: false))
        #expect(!composer.canDictate)
        // A second ! hides the output, as !! does.
        type("!", into: composer)
        #expect(composer.shell == .hidden)
        #expect(composer.draft == "")
        type("git status", into: composer)
        #expect(composer.action == .send(enabled: true))
    }

    @Test func pastedDoubleExclamationHidesTheOutput() {
        let composer = SessionComposerTests.composer()
        type("!!du -sh .build", into: composer)
        #expect(composer.shell == .hidden)
        #expect(composer.draft == "du -sh .build")
    }

    @Test func aBangInsideAMessageStaysText() {
        let composer = SessionComposerTests.composer()
        type("Run it", into: composer)
        type("Run it!", into: composer)
        #expect(composer.shell == nil)
        #expect(composer.draft == "Run it!")
    }

    @Test func removingTheChipLeavesAMessage() {
        let composer = SessionComposerTests.composer()
        type("!git status", into: composer)
        composer.shell = nil
        #expect(composer.draft == "git status")
        #expect(composer.canDictate == false)
        #expect(composer.action == .send(enabled: true))
    }

    @Test func aKeptShellCommandComesBackAsOne() {
        let composer = SessionComposerTests.composer()
        type("!!make test", into: composer)
        #expect(composer.kept.text == "!!make test")
        let reopened = SessionComposerTests.composer()
        reopened.restore(kept: composer.kept)
        #expect(reopened.shell == .hidden)
        #expect(reopened.draft == "make test")
    }

    @Test func sendRunsTheCommandAndClearsTheDraft() async {
        let client = FakeComposerClient()
        let composer = SessionComposerTests.composer(client)
        type("!!go vet ./...", into: composer)
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.shells.map(\.command) == ["go vet ./..."])
        #expect(client.shells.map(\.excludeFromContext) == [true])
        #expect(client.submitted.isEmpty)
        #expect(composer.shell == nil)
        #expect(composer.draft == "")
        // It runs until the stream says otherwise; the button stops it.
        #expect(composer.shellRunning == client.shells.first?.executionId)
        #expect(composer.action == .stop(enabled: true))
    }

    @Test func oneShellCommandRunsAtATime() {
        var running = SessionComposerTests.snapshot()
        running.activeBashID = "bash_1"
        let composer = SessionComposerTests.composer(snapshot: running)
        type("!ls", into: composer)
        #expect(composer.action == .send(enabled: false))
    }

    @Test func textTheComposerPutsInTheDraftIsntTyping() {
        let composer = SessionComposerTests.composer()
        composer.place("!ls was queued as a message")
        #expect(!composer.typed("!ls was queued as a message"))
        #expect(composer.typed("!ls was queued as a message."))
        // Deleting everything is typing too.
        #expect(composer.typed(""))
    }

    @Test func aShareMakesAShellCommandAMessage() {
        let composer = SessionComposerTests.composer()
        type("!git status", into: composer)
        composer.receive(sharedText: "https://example.com", attachmentIDs: [])
        #expect(composer.shell == nil)
        #expect(composer.draft == SharedItem.join("git status", "https://example.com"))
        // A closed session's kept shell command loses its ! the same way.
        #expect(SessionComposer.messageText("!!make test") == "make test")
        #expect(SessionComposer.messageText("!ls") == "ls")
        #expect(SessionComposer.messageText("Run it!") == "Run it!")
    }

    @Test func aDraftWithAnAttachmentStaysAMessage() async {
        let composer = SessionComposerTests.composer()
        composer.attach(filename: "notes.txt", data: Data("hi".utf8))
        await SessionComposerTests.settle()
        #expect(!composer.canStartShell)
        type("!ls", into: composer)
        #expect(composer.shell == nil)
        #expect(composer.draft == "!ls")
    }

    @Test func aStopThatFailsSaysSo() async {
        let client = FakeComposerClient()
        client.abortBashError = ClientError.http(409)
        var running = SessionComposerTests.snapshot()
        running.activeBashID = "bash_1"
        let composer = SessionComposerTests.composer(client, snapshot: running)
        composer.stop()
        #expect(composer.action == .waiting)
        await SessionComposerTests.settle()
        #expect(composer.action == .stop(enabled: true))
        #expect(composer.notice?.hasPrefix("Couldn’t stop. ") == true)
    }

    @Test func shellCommandsTakeNoAttachments() async {
        let client = FakeComposerClient()
        let composer = SessionComposerTests.composer(client)
        type("!ls", into: composer)
        // Shared in while the chip was up, say.
        composer.attach(filename: "notes.txt", data: Data("hi".utf8))
        await SessionComposerTests.settle()
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.shells.isEmpty)
        #expect(composer.notice == "Didn’t send. Shell commands don’t take attachments or comments.")
    }
}
