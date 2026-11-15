import KitClient
import KitDictation
import Observation
import UIKit

/// The composer's dictation for each open session, kept outside its screen
/// so a recording keeps going while the session's changes, files,
/// scratchpad, or a linked session are open over it. One session records at
/// a time. A recording ends when its session is closed, when the Sessions
/// tab is left, or when the app goes to the background, and its words go to
/// the session's draft; nothing is sent without the user.
@MainActor @Observable
final class SessionDictation {
    private let engine: any DictationEngine
    private var dictations: [SessionIdentity: Dictation] = [:]
    /// The sessions open in the Sessions tab, so words for one that was
    /// closed wait for it to open again.
    private var open: Set<SessionIdentity> = []
    /// Composers on screen, counted, since a linked session can be pushed
    /// over another of the same session.
    private var showing: [SessionIdentity: Int] = [:]
    /// Each open session's composer, to put words in its draft or send them.
    @ObservationIgnored private var composers: [SessionIdentity: WeakComposer] = [:]
    /// Closes what each open session's screen shows over its composer.
    @ObservationIgnored private var closers: [SessionIdentity: () -> Void] = [:]
    /// Words for a session that was closed while they were transcribed.
    @ObservationIgnored private var waiting: [SessionIdentity: String] = [:]
    /// Asks the screens to go back to a session's composer; the number
    /// changes with each ask.
    private(set) var returning: (session: SessionIdentity, count: Int)?

    init(engine: any DictationEngine = DictationSupport.engine) {
        self.engine = engine
    }

    /// The session's composer dictation, once its screen has appeared.
    func dictation(for id: SessionIdentity) -> Dictation? {
        dictations[id]
    }

    /// The session recording or transcribing, if one is.
    var active: SessionIdentity? {
        dictations.first { $0.value.isActive }?.key
    }

    /// Whether the session's mic can start: no other session records.
    func canStart(_ id: SessionIdentity) -> Bool {
        active.map { $0 == id } ?? true
    }

    /// The recording to show a pill for: one whose composer isn't on screen.
    var away: (session: SessionIdentity, dictation: Dictation)? {
        guard let id = active, showing[id, default: 0] == 0, let dictation = dictations[id] else { return nil }
        return (id, dictation)
    }

    // MARK: Screens

    /// A session's screen appeared with its composer, and how to close what
    /// it shows over the composer. Words for the session that waited while it
    /// was closed go in the draft.
    func attach(_ composer: SessionComposer, for id: SessionIdentity, closeOver: @escaping () -> Void) {
        composers[id] = WeakComposer(composer: composer)
        closers[id] = closeOver
        if dictations[id] == nil { dictations[id] = Dictation(engine: engine) }
        if let words = waiting.removeValue(forKey: id) { add(words, to: composer) }
    }

    func composerAppeared(_ id: SessionIdentity) {
        showing[id, default: 0] += 1
    }

    func composerDisappeared(_ id: SessionIdentity) {
        guard let count = showing[id] else { return }
        showing[id] = count > 1 ? count - 1 : nil
    }

    /// The sessions open in the Sessions tab changed. A recording whose
    /// session closed ends, and its words wait in its draft.
    func opened(_ sessions: Set<SessionIdentity>) {
        open = sessions
        for id in dictations.keys where !sessions.contains(id) {
            if dictations[id]?.phase == .recording { stop() }
            else if dictations[id]?.isActive == false { dictations[id] = nil }
        }
        composers = composers.filter { sessions.contains($0.key) && $0.value.composer != nil }
        closers = closers.filter { sessions.contains($0.key) }
    }

    /// The Sessions tab was left or the app went to the background: a
    /// recording ends and its words go to the draft.
    func interrupted() {
        guard let id = active, dictations[id]?.phase == .recording else {
            // Not yet recording: nothing was said.
            if let id = active { dictations[id]?.cancel() }
            return
        }
        stop()
    }

    // MARK: The pill

    /// Ends the recording and puts its words in the session's draft.
    func stop() {
        finish { words, id, composer in
            if let composer { self.add(words, to: composer) } else { self.wait(words, for: id) }
        }
    }

    /// Ends the recording and sends its words, as ↑ in the composer does.
    func send() {
        finish { words, id, composer in
            if let composer {
                // The mic is only there while the draft has no words.
                if composer.canDictate { composer.sendDictation(words) } else { self.add(words, to: composer) }
            } else {
                self.wait(words, for: id)
            }
        }
    }

    /// Throws the recording away.
    func cancel() {
        guard let id = active else { return }
        dictations[id]?.cancel()
    }

    /// Shows the session's composer: its screen closes what's over it, and
    /// the Sessions tab goes back to it.
    func returnToComposer() {
        guard let id = active else { return }
        closers[id]?()
        returning = (id, (returning?.count ?? 0) + 1)
    }

    private func finish(_ deliver: @escaping @MainActor (String, SessionIdentity, SessionComposer?) -> Void) {
        guard let id = active, let dictation = dictations[id] else { return }
        if dictation.isPreparing {
            dictation.cancel()
            return
        }
        // Transcribing can outlast the app's time in front.
        let background = UIApplication.shared.beginBackgroundTask(withName: "Dictation")
        Task {
            defer { UIApplication.shared.endBackgroundTask(background) }
            guard let words = await dictation.finish() else { return }
            let composer = open.contains(id) ? composers[id]?.composer : nil
            deliver(words, id, composer)
            if !open.contains(id), !dictation.isActive { dictations[id] = nil }
        }
    }

    private func add(_ words: String, to composer: SessionComposer) {
        composer.draft = composer.draft.isEmpty ? words : DictationText.join(composer.draft, words)
    }

    private func wait(_ words: String, for id: SessionIdentity) {
        waiting[id] = waiting[id].map { DictationText.join($0, words) } ?? words
    }

    private struct WeakComposer {
        weak var composer: SessionComposer?
    }
}
