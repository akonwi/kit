import Foundation
import KitClient
import KitDictation
import Testing
@testable import Kit

/// A session's dictation outlives the screen that started it: it keeps
/// recording while the session is open, and when it has to end, its words
/// go to the session's draft.
@MainActor
struct SessionDictationTests {
    let session = SessionIdentity(server: "studio", session: "s")
    let other = SessionIdentity(server: "studio", session: "other")
    let drafts = SessionDrafts(defaults: UserDefaults(suiteName: UUID().uuidString)!)

    func dictations(delay: Duration = .milliseconds(10)) -> SessionDictation {
        SessionDictation(engine: SharedMicrophone(ScriptedDictationEngine(transcript: "check the build", delay: delay)),
                         drafts: drafts)
    }

    /// Opens the session with its composer on screen and starts recording.
    func recording(_ dictations: SessionDictation, composer: SessionComposer,
                   closeOver: @escaping () -> Void = {}) async -> Dictation {
        dictations.opened([session])
        dictations.attach(composer, for: session, closeOver: closeOver)
        dictations.composerAppeared(session)
        let dictation = dictations.dictation(for: session)!
        #expect(await dictation.start())
        return dictation
    }

    func idle(_ dictation: Dictation) async {
        while dictation.isActive { try? await Task.sleep(for: .milliseconds(5)) }
    }

    @Test func aRecordingKeepsGoingOverItsSessionAndShowsAPill() async {
        let dictations = dictations()
        let dictation = await recording(dictations, composer: SessionComposerTests.composer())
        #expect(dictations.away == nil)

        dictations.composerDisappeared(session)
        dictations.opened([session, other])
        #expect(dictation.phase == .recording)
        #expect(dictations.away?.session == session)
        #expect(!dictations.canStart(other))
        #expect(dictations.canStart(session))
    }

    @Test func checkPutsTheWordsInTheDraft() async {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        let dictation = await recording(dictations, composer: composer)
        dictations.composerDisappeared(session)
        dictations.stop()
        await idle(dictation)
        #expect(composer.draft == "check the build")
    }

    /// The mic shows only while the draft is empty, so words sent from the
    /// pill join typed words in the draft rather than replace them.
    @Test func sendingOverATypedDraftAddsToIt() async {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        let dictation = await recording(dictations, composer: composer)
        composer.draft = "Then"
        dictations.send()
        await idle(dictation)
        #expect(composer.draft == "Then check the build")
    }

    @Test func closingTheSessionKeepsTheWordsForItsDraft() async {
        let dictations = dictations()
        let dictation = await recording(dictations, composer: SessionComposerTests.composer())
        dictations.composerDisappeared(session)
        dictations.opened([])
        await idle(dictation)
        #expect(dictations.active == nil)

        let reopened = SessionComposerTests.composer()
        dictations.opened([session])
        dictations.attach(reopened, for: session) {}
        #expect(reopened.draft == "check the build")
    }

    @Test func leavingTheAppEndsTheRecordingInTheDraft() async {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        let dictation = await recording(dictations, composer: composer)
        dictations.interrupted()
        await idle(dictation)
        #expect(composer.draft == "check the build")
    }

    /// Leaving the app after ✓ or ↑ lets the words arrive.
    @Test func leavingWhileTranscribingKeepsTheWords() async {
        let dictations = dictations(delay: .milliseconds(150))
        let composer = SessionComposerTests.composer()
        let dictation = await recording(dictations, composer: composer)
        dictations.stop()
        while dictation.phase != .transcribing { await Task.yield() }
        dictations.interrupted()
        dictations.opened([])
        await idle(dictation)
        #expect(dictations.dictation(for: session) == nil)
        let reopened = SessionComposerTests.composer()
        dictations.opened([session])
        dictations.attach(reopened, for: session) {}
        #expect(reopened.draft == "check the build")
        #expect(composer.draft == "")
    }

    /// A session closed while its mic gets ready doesn't start recording.
    @Test func closingWhileGettingReadyDropsTheRecording() async {
        let dictations = dictations(delay: .milliseconds(150))
        dictations.opened([session])
        dictations.attach(SessionComposerTests.composer(), for: session) {}
        let dictation = dictations.dictation(for: session)!
        let started = Task { await dictation.start() }
        while !dictation.isPreparing { await Task.yield() }
        dictations.opened([])
        #expect(await started.value == false)
        #expect(dictations.active == nil)
    }

    /// Every mic shares the microphone: one that starts while another
    /// records is refused, and one that starts while another ends waits.
    @Test func theMicrophoneIsLentToOneRecordingAtATime() async {
        let microphone = SharedMicrophone(ScriptedDictationEngine(transcript: "words", delay: .milliseconds(50)))
        let first = Dictation(engine: microphone), second = Dictation(engine: microphone)
        #expect(await first.start())
        #expect(await second.start() == false)
        #expect(second.problem == .unavailable)

        let finished = Task { await first.finish() }
        while first.phase != .transcribing { await Task.yield() }
        #expect(await second.start())
        #expect(await finished.value == "words")
        second.cancel()
        await idle(second)
    }

    // MARK: Drafts

    @Test func aTypedDraftIsThereWhenTheSessionOpensAgain() {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        dictations.attach(composer, for: session) {}
        composer.draft = "Half a thought"

        let reopened = SessionComposerTests.composer()
        dictations.attach(reopened, for: session) {}
        #expect(reopened.draft == "Half a thought")

        reopened.draft = ""
        let again = SessionComposerTests.composer()
        dictations.attach(again, for: session) {}
        #expect(again.draft == "")
    }

    /// A draft keeps the attachments uploaded for it, and they come back as
    /// ones attached before.
    @Test func aDraftKeepsItsAttachments() async {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        dictations.attach(composer, for: session) {}
        composer.draft = "See this"
        composer.attach(filename: "notes.txt", data: Data("notes".utf8))
        for _ in 0..<200 where drafts.draft(for: session).attachmentIDs.isEmpty { try? await Task.sleep(for: .milliseconds(10)) }
        #expect(drafts.draft(for: session) == KeptDraft(text: "See this", attachmentIDs: ["attachment_1"]))

        let reopened = SessionComposerTests.composer()
        dictations.attach(reopened, for: session) {}
        #expect(reopened.draft == "See this")
        #expect(reopened.restoredAttachmentIDs == ["attachment_1"])

        reopened.removeRestored("attachment_1")
        #expect(drafts.draft(for: session) == KeptDraft(text: "See this"))
    }

    /// An attachment removed just before the app goes to the background
    /// isn't kept.
    @Test func keepingDraftsSavesWhatTheComposerHasNow() async {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        dictations.opened([session])
        dictations.attach(composer, for: session) {}
        composer.attach(filename: "notes.txt", data: Data("notes".utf8))
        for _ in 0..<200 where drafts.draft(for: session).attachmentIDs.isEmpty { try? await Task.sleep(for: .milliseconds(10)) }
        #expect(drafts.draft(for: session) == KeptDraft(attachmentIDs: ["attachment_1"]))

        // No suspension between removing it and keeping drafts.
        composer.uploads.remove(composer.uploads.items[0].id)
        dictations.keepDrafts()
        #expect(drafts.draft(for: session) == KeptDraft())
    }

    /// A draft of only attachments is kept too.
    @Test func aDraftOfOnlyAttachmentsIsKept() {
        drafts.save(KeptDraft(attachmentIDs: ["attachment_1"]), for: session)
        #expect(drafts.draft(for: session).attachmentIDs == ["attachment_1"])
        drafts.save(KeptDraft(), for: session)
        #expect(drafts.draft(for: session) == KeptDraft())
    }

    /// Drafts kept as text alone, as they first were, are read.
    @Test func draftsKeptAsTextAreRead() {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        defaults.set(["studio\u{1F}s": "Half a thought"], forKey: "sessions.drafts")
        #expect(SessionDrafts(defaults: defaults).draft(for: session) == KeptDraft(text: "Half a thought"))
    }

    /// A composer that starts with words, such as a failed fork's message,
    /// keeps them over the kept draft.
    @Test func aComposerThatStartsWithWordsKeepsThem() {
        let dictations = dictations()
        drafts.save("Older draft", for: session)
        let composer = SessionComposerTests.composer()
        composer.draft = "The fork's first message"
        dictations.attach(composer, for: session) {}
        #expect(composer.draft == "The fork's first message")
        #expect(drafts.text(for: session) == "The fork's first message")
    }

    /// Words for a closed session follow its typed draft.
    @Test func wordsForAClosedSessionFollowItsDraft() async {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        let dictation = await recording(dictations, composer: composer)
        composer.draft = "Then"
        dictations.opened([])
        await idle(dictation)
        #expect(drafts.text(for: session) == "Then check the build")
    }

    @Test func draftsGoWithTheirSessions() {
        drafts.listed("studio", sessions: ["s", "other"])
        drafts.save("kept", for: session)
        drafts.save("gone", for: other)
        drafts.save("elsewhere", for: SessionIdentity(server: "vps", session: "other"))
        drafts.listed("studio", sessions: ["s"])
        #expect(drafts.text(for: session) == "kept")
        #expect(drafts.text(for: other) == "")
        #expect(drafts.text(for: SessionIdentity(server: "vps", session: "other")) == "elsewhere")
        drafts.forget("studio")
        #expect(drafts.text(for: session) == "")
    }

    /// A session just started isn't in a listing that began before it, so
    /// its draft waits until a listing has shown it.
    @Test func aNewSessionsDraftOutlastsAnEarlierListing() {
        drafts.listed("studio", sessions: ["s"])
        drafts.save("First words", for: other)
        drafts.listed("studio", sessions: ["s"])
        #expect(drafts.text(for: other) == "First words")
        drafts.listed("studio", sessions: ["s", "other"])
        drafts.listed("studio", sessions: ["s"])
        #expect(drafts.text(for: other) == "")
    }

    /// Drafts are written once typing settles, and are read back after a relaunch.
    @Test func draftsAreWrittenOnceTypingSettles() async throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        let drafts = SessionDrafts(defaults: defaults)
        drafts.save("Half", for: session)
        drafts.save("Half a thought", for: session)
        #expect(SessionDrafts(defaults: defaults).text(for: session) == "")
        for _ in 0..<100 where SessionDrafts(defaults: defaults).text(for: session).isEmpty {
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(SessionDrafts(defaults: defaults).text(for: session) == "Half a thought")
    }

    // MARK: Background time

    /// When the app's background time runs out mid-transcription, the
    /// words transcribed so far are kept.
    @Test func runningOutOfTimeKeepsTheWordsSoFar() async throws {
        let time = FakeBackgroundTime()
        let microphone = SharedMicrophone(SlowEngine()) { expired in
            time.expired = expired
            return { time.ended += 1 }
        }
        let recording = try await microphone.prepare(installing: {}, level: { _ in })
        try recording.start()
        let finished = Task { try await recording.finish() }
        while time.expired == nil { await Task.yield() }
        time.expired?()
        #expect(try await finished.value == "What was said so far")
        // The recording stops, then the time ends and the microphone is free.
        while time.ended == 0 { await Task.yield() }
        let next = try await microphone.prepare(installing: {}, level: { _ in })
        await next.cancel()
    }

    /// When the time runs out, the words so far are in the draft, and
    /// written, before the time ends; the late answer isn't added again.
    @Test func runningOutOfTimeKeepsTheWordsSoFarInTheDraft() async throws {
        let time = FakeBackgroundTime()
        let dictations = SessionDictation(engine: SharedMicrophone(SlowEngine()) { _ in { } }, drafts: drafts) { expired in
            time.expired = expired
            return { time.ended += 1 }
        }
        let composer = SessionComposerTests.composer()
        let dictation = await recording(dictations, composer: composer)
        dictations.composerDisappeared(session)
        dictations.opened([])
        while time.expired == nil { await Task.yield() }
        time.expired?()
        #expect(drafts.text(for: session) == "What was said so far")
        dictation.cancel()
        await idle(dictation)
        while time.ended == 0 { await Task.yield() }
        #expect(drafts.text(for: session) == "What was said so far")
    }

    @Test func cancelThrowsTheWordsAway() async {
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        let dictation = await recording(dictations, composer: composer)
        dictations.cancel()
        await idle(dictation)
        #expect(composer.draft == "")
    }

    @Test func theComposerIsAskedForBack() async {
        let dictations = dictations()
        var closed = 0
        _ = await recording(dictations, composer: SessionComposerTests.composer()) { closed += 1 }
        dictations.returnToComposer()
        #expect(closed == 1)
        #expect(dictations.returning?.session == session)
        let first = dictations.returning?.count
        dictations.returnToComposer()
        #expect(dictations.returning?.count != first)
    }

    /// A composer that appears while the session records, such as the one
    /// in Changes, takes it as a tapped recording: ↑ sends it.
    @Test func aMicThatAppearsOverARecordingTakesItAsTapped() {
        var press = DictationPress()
        press.adopt(recording: true)
        #expect(press.mode == .tap)
        #expect(press.ended(at: .now, recording: true) == .none)
        #expect(press.began(at: .now, idle: false) == .none)
        #expect(press.ended(at: .now, recording: true) == .send)
    }
}

/// Background time a test runs out of.
@MainActor private final class FakeBackgroundTime: Sendable {
    var expired: (@MainActor @Sendable () -> Void)?
    var ended = 0
}

/// A recording whose finishing never ends on its own, with some words
/// transcribed.
private struct SlowEngine: DictationEngine {
    func requestAccess() async -> DictationAccess { .granted }
    func prepare(installing: @escaping @Sendable () -> Void,
                 level: @escaping @Sendable (Double) -> Void) async throws -> any DictationRecording {
        SlowRecording()
    }

    final class SlowRecording: DictationRecording, @unchecked Sendable {
        private let canceled = AsyncStream<Void>.makeStream()
        func start() throws {}
        func finish() async throws -> String {
            for await _ in canceled.stream {}
            throw CancellationError()
        }
        func cancel() async { canceled.continuation.finish() }
        func transcribedSoFar() -> String { "What was said so far" }
    }
}
