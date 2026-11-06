import Foundation
import Observation

/// A session's shared scratchpad as one client edits it: the draft, the
/// shared copy it was based on, a debounced autosave, and, when the shared
/// copy changes under unsaved edits, a review of the two before either wins.
@MainActor @Observable
public final class ScratchpadState {
    public enum Status: Equatable, Sendable { case loading, saved, unsaved, saving, conflict, failed(String) }
    /// Waits out a delay, throwing if the waiting task is cancelled first.
    public typealias Sleep = @Sendable (Duration) async throws -> Void
    public var draft: String
    public private(set) var authoritative: ScratchpadRecord?
    public private(set) var status: Status
    public private(set) var reviewing = false
    public private(set) var reviewed: ScratchpadRecord?
    /// Bumped when the draft is replaced by a shared copy rather than typed,
    /// so an editor knows to reload its text.
    public private(set) var documentVersion = 0
    @ObservationIgnored private var debounce: Task<Void, Never>?
    @ObservationIgnored private var savingContent: String?
    @ObservationIgnored private var loading = false
    /// Waits out `autosaveDelay`.
    @ObservationIgnored private let sleeper: Sleep
    /// How long an edit waits for more before it is saved.
    public static let autosaveDelay: Duration = .seconds(5)

    /// A demo scratchpad starts saved with a heading; a real one loads.
    /// `sleep` waits out the autosave delay, so tests can decide when it ends.
    public init(demo: Bool, sleep: @escaping Sleep = { try await Task.sleep(for: $0) }) {
        self.sleeper = sleep
        draft = demo ? "# Scratchpad\n\n" : ""
        status = demo ? .saved : .loading
    }

    public var dirty: Bool { authoritative.map { $0.content != draft } ?? !draft.isEmpty }
    public var reviewText: String {
        guard let reviewed else { return "" }
        return Self.unified(shared: reviewed.content, draft: draft)
    }

    public func edit(_ value: String) {
        draft = value
        guard let authoritative else { status = .unsaved; return }
        if status == .conflict || reviewing { return }
        status = value == authoritative.content ? .saved : .unsaved
        if status == .unsaved { schedule() } else { debounce?.cancel() }
    }

    public func observe(_ record: ScratchpadRecord) {
        guard record.revision >= (authoritative?.revision ?? 0) else { return }
        if let previous = authoritative, previous.owner != record.owner { return }
        if authoritative?.revision == record.revision && authoritative?.content == record.content { return }
        let wasDirty = dirty
        let ownSave = savingContent == record.content
        authoritative = record
        if !wasDirty || draft == record.content {
            if draft != record.content { documentVersion += 1 }
            draft = record.content
            status = .saved
            debounce?.cancel()
        } else if ownSave {
            status = .unsaved
            schedule()
        } else {
            debounce?.cancel()
            status = .conflict
            if reviewing { reviewed = record }
        }
    }

    public func load(client: any ScratchpadClient, session: String) async {
        guard authoritative == nil, !loading else { return }
        loading = true
        defer { loading = false }
        do { observe(try await client.scratchpad(session: session)) }
        catch { status = .failed(error.localizedDescription) }
    }

    private func schedule() {
        debounce?.cancel()
        debounce = Task { [weak self, sleeper] in
            try? await sleeper(Self.autosaveDelay)
            guard !Task.isCancelled else { return }
            await self?.autosave()
        }
    }

    @ObservationIgnored private var client: (any ScratchpadClient)?
    @ObservationIgnored private var session = ""
    public func bind(client: any ScratchpadClient, session: String) {
        self.client = client
        self.session = session
        if dirty, authoritative != nil, status == .unsaved { schedule() }
    }
    private func autosave() async {
        // The debounce task owns this call. Clearing its handle prevents save()
        // (and the successful observe()) from cancelling the request itself.
        debounce = nil
        guard let client else { return }
        _ = await save(client: client, session: session)
    }

    @discardableResult public func save(client: any ScratchpadClient, session: String, reviewedRevision: Int64? = nil) async -> Bool {
        debounce?.cancel()
        guard let current = authoritative else { await load(client: client, session: session); return !dirty }
        guard savingContent == nil else { return false }
        guard status != .conflict || reviewedRevision != nil else { return false }
        if !dirty { status = .saved; return true }
        let content = draft
        let revision = reviewedRevision ?? current.revision
        savingContent = content
        status = .saving
        defer { savingContent = nil }
        do {
            let saved = try await client.updateScratchpad(session: session, content: content, expectedRevision: revision)
            guard saved.owner == current.owner else { throw ClientError.invalidPayload }
            observe(saved)
            guard authoritative?.revision == saved.revision, status != .conflict else { return false }
            if draft != saved.content, status != .conflict { status = .unsaved; schedule() }
            reviewing = false
            reviewed = nil
            return !dirty
        } catch ScratchpadFailure.conflict(let latest) {
            observe(latest)
            status = .conflict
            if reviewing { reviewed = latest }
            return false
        } catch {
            if status != .conflict { status = .failed(error.localizedDescription) }
            return false
        }
    }

    public func review() {
        guard status == .conflict, let authoritative else { return }
        reviewed = authoritative
        reviewing = true
    }
    public func keepEditing() { reviewing = false }
    public func useShared() {
        guard let authoritative else { return }
        debounce?.cancel()
        if draft != authoritative.content { documentVersion += 1 }
        draft = authoritative.content
        status = .saved
        reviewing = false
        reviewed = nil
    }
    public func replaceReviewed(client: any ScratchpadClient, session: String) async {
        guard reviewing, let revision = reviewed?.revision else { return }
        _ = await save(client: client, session: session, reviewedRevision: revision)
    }

    public static func unified(shared: String, draft: String) -> String {
        let old = shared.components(separatedBy: "\n")
        let new = draft.components(separatedBy: "\n")
        let changes = new.difference(from: old)
        let removed = Dictionary(grouping: changes.compactMap { change -> (Int, String)? in
            if case .remove(let offset, let line, _) = change { return (offset, line) }
            return nil
        }, by: { $0.0 })
        let inserted = Dictionary(grouping: changes.compactMap { change -> (Int, String)? in
            if case .insert(let offset, let line, _) = change { return (offset, line) }
            return nil
        }, by: { $0.0 })
        var lines = ["--- shared", "+++ local draft", "@@ -1,\(old.count) +1,\(new.count) @@"]
        var i = 0, j = 0
        while i < old.count || j < new.count {
            if let edits = removed[i] { lines += edits.map { "-" + $0.1 }; i += edits.count; continue }
            if let edits = inserted[j] { lines += edits.map { "+" + $0.1 }; j += edits.count; continue }
            if i < old.count, j < new.count { lines.append(" " + old[i]); i += 1; j += 1 }
            else if i < old.count { lines.append("-" + old[i]); i += 1 }
            else { lines.append("+" + new[j]); j += 1 }
        }
        return lines.joined(separator: "\n")
    }
}
