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

    func dictations() -> SessionDictation {
        SessionDictation(engine: ScriptedDictationEngine(transcript: "check the build", delay: .milliseconds(10)))
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
