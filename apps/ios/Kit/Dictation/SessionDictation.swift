import KitClient
import KitDictation
import Observation

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
    /// Each session's kept draft, where words for a closed session go.
    @ObservationIgnored private let drafts: SessionDrafts
    /// Asks the screens to go back to a session's composer; the number
    /// changes with each ask.
    private(set) var returning: (session: SessionIdentity, count: Int)?

    @ObservationIgnored private let keepRunning: SharedMicrophone.KeepRunning

    init(engine: any DictationEngine = DictationSupport.engine, drafts: SessionDrafts,
         keepRunning: @escaping SharedMicrophone.KeepRunning = BackgroundTime.keep) {
        self.engine = engine
        self.drafts = drafts
        self.keepRunning = keepRunning
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
    /// it shows over the composer. The composer starts with the session's
    /// kept draft, and keeps it as it changes.
    func attach(_ composer: SessionComposer, for id: SessionIdentity, closeOver: @escaping () -> Void) {
        composers[id] = WeakComposer(composer: composer)
        closers[id] = closeOver
        if dictations[id] == nil { dictations[id] = Dictation(engine: engine) }
        if composer.draftChanged == nil {
            // A composer that starts with words, such as a failed fork's message, keeps them.
            if composer.draft.isEmpty { composer.draft = drafts.text(for: id) } else { drafts.save(composer.draft, for: id) }
            let drafts = drafts
            composer.draftChanged = { drafts.save($0, for: id) }
        }
    }

    func composerAppeared(_ id: SessionIdentity) {
        showing[id, default: 0] += 1
    }

    func composerDisappeared(_ id: SessionIdentity) {
        guard let count = showing[id] else { return }
        showing[id] = count > 1 ? count - 1 : nil
    }

    /// The sessions open in the Sessions tab changed. A recording whose
    /// session closed ends, and its words wait in its draft; one still
    /// getting ready is dropped, since nothing was said yet. One that's
    /// transcribing delivers its words when they come.
    func opened(_ sessions: Set<SessionIdentity>) {
        open = sessions
        for (id, dictation) in dictations where !sessions.contains(id) {
            switch dictation.phase {
            case .recording: stop()
            case .preparing: dictation.cancel()
            case .transcribing: break
            case .idle: dictations[id] = nil
            }
        }
        composers = composers.filter { sessions.contains($0.key) && $0.value.composer != nil }
        closers = closers.filter { sessions.contains($0.key) }
    }

    /// The Sessions tab was left or the app went to the background: a
    /// recording ends and its words go to the draft. One still getting ready
    /// is dropped; one that's transcribing carries on.
    func interrupted() {
        guard let id = active, let dictation = dictations[id] else { return }
        switch dictation.phase {
        case .recording: stop()
        case .preparing: dictation.cancel()
        case .idle, .transcribing: break
        }
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
        // The app keeps running until the words are in the draft, and the
        // draft is written, even if the app was left meanwhile. If that time
        // runs out first, the words so far go in the draft, never sent, before it ends.
        let delivered = Delivered()
        let end = keepRunning { [self] in
            let soFar = dictation.transcribedSoFar().trimmingCharacters(in: .whitespacesAndNewlines)
            guard !delivered.done, !soFar.isEmpty else { return }
            delivered.done = true
            if open.contains(id), let composer = composers[id]?.composer { add(soFar, to: composer) } else { wait(soFar, for: id) }
            drafts.write()
        }
        Task {
            defer {
                if !open.contains(id), !dictation.isActive { dictations[id] = nil }
                drafts.write()
                end()
            }
            guard let words = await dictation.finish(), !delivered.done else { return }
            delivered.done = true
            let composer = open.contains(id) ? composers[id]?.composer : nil
            deliver(words, id, composer)
        }
    }

    private func add(_ words: String, to composer: SessionComposer) {
        composer.draft = composer.draft.isEmpty ? words : DictationText.join(composer.draft, words)
    }

    private func wait(_ words: String, for id: SessionIdentity) {
        drafts.append(words, for: id)
    }

    /// Whether a recording's words went somewhere already.
    private final class Delivered {
        var done = false
    }

    private struct WeakComposer {
        weak var composer: SessionComposer?
    }
}
