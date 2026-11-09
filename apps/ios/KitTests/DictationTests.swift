import Foundation
import KitClient
import KitDictation
import Testing
@testable import Kit

/// The composer's mic: where it sits, how a press is read as a tap or a
/// hold, and what happens to the words.
@MainActor
struct DictationTests {
    // MARK: Where the mic sits

    @Test func anEmptyIdleComposerPutsTheMicInSendsSlot() {
        let composer = SessionComposerTests.composer()
        #expect(composer.canDictate)
        #expect(composer.dictatesInSendSlot)
        composer.draft = "Run the tests"
        #expect(!composer.canDictate)
        #expect(!composer.dictatesInSendSlot)
        composer.draft = " \n"
        #expect(composer.dictatesInSendSlot)
    }

    @Test func aRunningTurnKeepsStopAndPutsThePlainMicBesideIt() {
        let composer = SessionComposerTests.composer(snapshot: SessionComposerTests.snapshot(running: "t1"))
        #expect(composer.canDictate)
        #expect(!composer.dictatesInSendSlot)
        #expect(composer.action == .stop(enabled: true))
    }

    @Test func thereIsNoMicWhileASendIsUnanswered() {
        let composer = SessionComposerTests.composer()
        composer.draft = "Run the tests"
        composer.send()
        composer.draft = ""
        #expect(!composer.editable)
        #expect(!composer.canDictate)
    }

    // MARK: The words

    @Test func dictatedWordsAreSentAtOnce() async {
        let client = FakeComposerClient()
        let composer = SessionComposerTests.composer(client)
        composer.sendDictation("Run the reconnect test 20 times in a row")
        await SessionComposerTests.settle()
        #expect(client.submitted.map(\.text) == ["Run the reconnect test 20 times in a row"])
        #expect(composer.draft == "")
    }

    @Test func dictatedWordsThatCantGoStayInTheDraft() async {
        let client = FakeComposerClient()
        let composer = SessionComposerTests.composer(client, live: false)
        composer.sendDictation("Run the reconnect test")
        await SessionComposerTests.settle()
        #expect(client.submitted.isEmpty)
        #expect(composer.draft == "Run the reconnect test")
    }

    // MARK: Tap or hold

    static let start = Date(timeIntervalSinceReferenceDate: 0)

    @Test func aQuickPressKeepsRecordingUntilSendIsTapped() {
        var press = DictationPress()
        #expect(press.began(at: Self.start, idle: true) == .start)
        #expect(press.mode == .hold)
        #expect(press.ended(at: Self.start + 0.2, recording: true) == .none)
        #expect(press.mode == .tap)
        // ↑ is the next press on the same button.
        #expect(press.began(at: Self.start + 3, idle: false) == .none)
        #expect(press.ended(at: Self.start + 3.1, recording: true) == .send)
        #expect(press.mode == .none)
    }

    @Test func aHeldPressSendsWhenLetGo() {
        var press = DictationPress()
        #expect(press.began(at: Self.start, idle: true) == .start)
        press.moved(width: -20)
        #expect(!press.cancelArmed)
        #expect(press.ended(at: Self.start + 2, recording: true) == .send)
        #expect(press.mode == .none)
    }

    @Test func slidingPastTheThresholdThrowsTheRecordingAway() {
        var press = DictationPress()
        _ = press.began(at: Self.start, idle: true)
        press.moved(width: -71)
        #expect(press.cancelArmed)
        press.moved(width: -40)
        #expect(!press.cancelArmed)
        press.moved(width: -90)
        #expect(press.ended(at: Self.start + 2, recording: true) == .cancel)
        #expect(!press.cancelArmed)
        #expect(press.mode == .none)
    }

    @Test func aPressTakenAwayThrowsAHeldRecordingAwayButNotATappedOne() {
        var held = DictationPress()
        _ = held.began(at: Self.start, idle: true)
        #expect(held.interrupted() == .cancel)
        #expect(held.mode == .none)

        var tapped = DictationPress()
        _ = tapped.began(at: Self.start, idle: true)
        _ = tapped.ended(at: Self.start + 0.1, recording: true)
        _ = tapped.began(at: Self.start + 2, idle: false)
        #expect(tapped.interrupted() == .none)
        #expect(tapped.mode == .tap)
    }

    @Test func aPressAfterItEndedIsNotTakenAway() {
        var press = DictationPress()
        _ = press.began(at: Self.start, idle: true)
        _ = press.ended(at: Self.start + 2, recording: true)
        #expect(press.interrupted() == .none)
    }

    @Test func theButtonShowsWhatLettingGoWillDo() {
        #expect(DictationButtonState(phase: .idle, mode: .none, cancelArmed: false) == .idle)
        #expect(DictationButtonState(phase: .preparing(installing: true), mode: .hold, cancelArmed: false) == .busy)
        #expect(DictationButtonState(phase: .recording, mode: .tap, cancelArmed: false) == .tapped)
        #expect(DictationButtonState(phase: .recording, mode: .hold, cancelArmed: false) == .holding)
        #expect(DictationButtonState(phase: .recording, mode: .hold, cancelArmed: true) == .cancelArmed)
        #expect(DictationButtonState(phase: .transcribing, mode: .none, cancelArmed: false) == .busy)
    }

    // MARK: Dictation

    @Test func aRecordingReturnsItsWordsTrimmed() async {
        let engine = FakeDictationEngine(transcript: "  Also check the 409 path.  ")
        let dictation = Dictation(engine: engine)
        #expect(await dictation.start())
        #expect(dictation.phase == .recording)
        #expect(dictation.startedAt != nil)
        #expect(await dictation.finish() == "Also check the 409 path.")
        #expect(dictation.phase == .idle)
        #expect(dictation.problem == nil)
    }

    @Test func theFirstPressOnlyAsksForAccess() async {
        let dictation = Dictation(engine: FakeDictationEngine(access: .justGranted))
        #expect(!(await dictation.start()))
        #expect(dictation.phase == .idle)
        #expect(dictation.problem == nil)
    }

    @Test func refusedAccessIsSaid() async {
        let dictation = Dictation(engine: FakeDictationEngine(access: .denied))
        #expect(!(await dictation.start()))
        #expect(dictation.problem == .notAllowed)
        dictation.dismissProblem()
        #expect(dictation.problem == nil)
    }

    @Test func silenceGivesNothingAndSaysSo() async {
        let dictation = Dictation(engine: FakeDictationEngine(transcript: " "))
        #expect(await dictation.start())
        #expect(await dictation.finish() == nil)
        #expect(dictation.problem == .nothingHeard)
        // The next attempt clears it.
        #expect(await dictation.start())
        #expect(dictation.problem == nil)
    }

    @Test func aFailedStartSaysWhy() async {
        let dictation = Dictation(engine: FakeDictationEngine(prepareError: DictationProblem.unsupportedLanguage))
        #expect(!(await dictation.start()))
        #expect(dictation.phase == .idle)
        #expect(dictation.problem == .unsupportedLanguage)
    }

    @Test func aRecordingThatCantStartIsCanceled() async {
        let engine = FakeDictationEngine(transcript: "Hello")
        engine.startError = DictationProblem.unavailable
        let dictation = Dictation(engine: engine)
        #expect(!(await dictation.start()))
        #expect(dictation.phase == .idle)
        #expect(dictation.problem == .unavailable)
        #expect(engine.recordings.first?.canceled == true)
    }

    @Test func cancelingDropsTheRecording() async {
        let engine = FakeDictationEngine(transcript: "Never mind")
        let dictation = Dictation(engine: engine)
        #expect(await dictation.start())
        dictation.cancel()
        #expect(dictation.phase == .idle)
        #expect(await dictation.finish() == nil)
        await SessionComposerTests.settle()
        #expect(engine.recordings.first?.canceled == true)
    }

    @Test func cancelingWhileGettingReadyDropsTheRecordingItMakes() async {
        let engine = FakeDictationEngine(transcript: "Too late")
        engine.prepareDelay = .milliseconds(50)
        let dictation = Dictation(engine: engine)
        let started = Task { await dictation.start() }
        // Cancel once the engine is making the recording.
        for _ in 0..<100 where engine.recordings.isEmpty { try? await Task.sleep(for: .milliseconds(1)) }
        #expect(dictation.isPreparing)
        dictation.cancel()
        #expect(!(await started.value))
        #expect(dictation.phase == .idle)
        #expect(engine.recordings.first?.started == false)
        #expect(engine.recordings.first?.canceled == true)
    }

    @Test func aModelDownloadIsShownWhileGettingReady() async {
        let engine = FakeDictationEngine(transcript: "Hello")
        engine.installs = true
        engine.prepareDelay = .milliseconds(500)
        let dictation = Dictation(engine: engine)
        let started = Task { await dictation.start() }
        for _ in 0..<200 where dictation.phase != .preparing(installing: true) {
            try? await Task.sleep(for: .milliseconds(1))
        }
        #expect(dictation.phase == .preparing(installing: true))
        #expect(await started.value)
        #expect(dictation.phase == .recording)
    }

    @Test func wordsJoinWithOneSpace() {
        #expect(DictationText.join("", "Hello") == "Hello")
        #expect(DictationText.join("Hello", "there") == "Hello there")
        #expect(DictationText.join("Hello ", "there") == "Hello there")
        #expect(DictationText.join("Hello", " there") == "Hello there")
        #expect(DictationText.join("Hello", "") == "Hello")
    }
}

/// A dictation engine that "hears" `transcript`.
final class FakeDictationEngine: DictationEngine, @unchecked Sendable {
    let transcript: String
    let access: DictationAccess
    let prepareError: Error?
    var prepareDelay: Duration = .zero
    var installs = false
    var startError: Error?
    private(set) var recordings: [FakeRecording] = []

    init(transcript: String = "", access: DictationAccess = .granted, prepareError: Error? = nil) {
        self.transcript = transcript
        self.access = access
        self.prepareError = prepareError
    }

    func requestAccess() async -> DictationAccess { access }

    func prepare(installing: @escaping @Sendable () -> Void,
                 level: @escaping @Sendable (Double) -> Void) async throws -> any DictationRecording {
        if let prepareError { throw prepareError }
        if installs { installing() }
        let recording = FakeRecording(transcript: transcript, startError: startError)
        recordings.append(recording)
        if prepareDelay > .zero { try await Task.sleep(for: prepareDelay) }
        return recording
    }
}

final class FakeRecording: DictationRecording, @unchecked Sendable {
    let transcript: String
    let startError: Error?
    private(set) var started = false
    private(set) var canceled = false

    init(transcript: String, startError: Error? = nil) {
        self.transcript = transcript
        self.startError = startError
    }

    func start() throws {
        if let startError { throw startError }
        started = true
    }
    func finish() async throws -> String { transcript }
    func cancel() async { canceled = true }
}
