import Foundation
import Observation

enum DiffLayout: String, CaseIterable { case unified = "Unified", split = "Split" }

@MainActor @Observable final class DiffState {
    var layout: DiffLayout = .unified
    var wrapLines = false
    var fileStates: [String: DiffState] = [:]
    private var fileReadTail: Task<Void, Never>?
    var catalog: WireDiffTargetCatalog?
    var target: WireDiffTargetEntry?
    var observation: WireDiffObservation?
    var files: [WireDiffFileSummary] = []
    var selectedPath: String?
    var document: DiffDocument?
    var loading = false
    var error: String?
    var notice: String?
    var stale = false
    var fileCursor: String?
    var lineCursor: String?
    var navigation: FileAnnotation?
    var navigationToken = UUID()
    private var hunks: [WireDiffHunk] = []
    private var epoch = UUID()
    private var fileCursors = Set<String>()
    private var lineCursors = Set<String>()
    private var annotationID: UInt64?

    /// Files keep independent pagination and native editor identity within one observed revision.
    func loadFile(_ file: WireDiffFileSummary, client: any DiffClient, session: String) async {
        guard fileStates[file.path] == nil, let observation else { return }
        let previous = fileReadTail
        let task = Task { @MainActor [weak self] in
            await previous?.value
            guard let self, !Task.isCancelled, self.observation?.revision == observation.revision,
                  self.observation?.target.id == observation.target.id, self.fileStates[file.path] == nil else { return }
            let reader = DiffState()
            reader.catalog = self.catalog; reader.target = self.target; reader.observation = observation
            reader.annotationID = self.annotationID
            if let document = self.document, document.file.path == file.path {
                reader.document = document; reader.selectedPath = file.path
                reader.hunks = self.hunks; reader.lineCursor = self.lineCursor
                reader.lineCursors = self.lineCursors; reader.notice = self.notice
                reader.stale = self.stale
                self.fileStates[file.path] = reader
            } else {
                self.fileStates[file.path] = reader
                await reader.read(file, client: client, session: session)
                if Task.isCancelled, self.fileStates[file.path] === reader, reader.document == nil {
                    self.fileStates.removeValue(forKey: file.path)
                }
            }
        }
        fileReadTail = task
        await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
    }

    func reveal(_ note: FileAnnotation) { navigation = note; navigationToken = UUID() }
    func invalidate() { fileStates = [:]; epoch = UUID(); catalog = nil; target = nil; observation = nil; files = []; selectedPath = nil; document = nil; loading = false; error = nil }

    /// Fetch in isolation so polling never clears the visible diff or interrupts a draft.
    func poll(client: any DiffClient, session: String, canApply: () -> Bool) async {
        guard !loading, canApply() else { return }
        let token = epoch
        let updated = DiffState()
        updated.target = target
        updated.selectedPath = selectedPath
        await updated.refresh(client: client, session: session)
        guard epoch == token, !Task.isCancelled, canApply() else { return }
        if let error = updated.error { self.error = error; stale = true; return }
        catalog = updated.catalog
        error = nil
        stale = false
        // Preserve expanded pages, source selection, and scroll position when unchanged.
        if document != nil, document?.file.path == updated.document?.file.path,
           document?.file.fileRevision == updated.document?.file.fileRevision,
           observation?.revision == updated.observation?.revision,
           observation?.target.id == updated.observation?.target.id { return }
        epoch = UUID()
        fileStates = [:]
        target = updated.target; observation = updated.observation; files = updated.files
        selectedPath = updated.selectedPath; document = updated.document; notice = updated.notice
        fileCursor = updated.fileCursor; lineCursor = updated.lineCursor; hunks = updated.hunks
        fileCursors = updated.fileCursors; lineCursors = updated.lineCursors; annotationID = nil
    }

    func refresh(client: any DiffClient, session: String) async {
        let token = UUID(); epoch = token; loading = true; error = nil
        defer { if epoch == token { loading = false } }
        do {
            let catalog = try await client.diffTargets(session)
            guard epoch == token, !Task.isCancelled else { return }
            self.catalog = catalog
            guard let choice = catalog.targets?.first(where: { $0.targetId == target?.targetId }) ?? catalog.targets?.first else {
                fileStates = [:]; target = nil; observation = nil; document = nil; files = []; notice = "No diff targets available."; return
            }
            await select(choice, client: client, session: session)
        } catch { if epoch == token { fail(error) } }
    }

    func select(_ target: WireDiffTargetEntry, client: any DiffClient, session: String, more: Bool = false) async {
        guard let catalog else { return }
        let token = UUID(); epoch = token; loading = true; error = nil; stale = false
        let priorPath = selectedPath
        if !more {
            fileStates = [:]
            self.target = target; observation = nil; files = []; document = nil; selectedPath = nil
            fileCursor = nil; lineCursor = nil; hunks = []; fileCursors = []; annotationID = nil; notice = nil
        }
        defer { if epoch == token { loading = false } }
        do {
            let page = try await client.observeDiff(session, input: .init(workspaceId: catalog.workspaceId,
                targetReference: target.reference, expectedTargetId: target.targetId,
                expectedTargetRevision: more ? observation?.revision : nil, pageSize: 100, cursor: more ? fileCursor : nil))
            guard epoch == token, !Task.isCancelled else { return }
            let incoming = page.files ?? []
            guard files.count + incoming.count <= 4000, Set(files.map(\.path)).isDisjoint(with: incoming.map(\.path)) else { throw ClientError.invalidPayload }
            if let cursor = page.nextCursor, !cursor.isEmpty {
                guard !incoming.isEmpty, fileCursors.insert(cursor).inserted else { throw ClientError.invalidPayload }
            }
            observation = page.observation; files += incoming; fileCursor = page.nextCursor?.isEmpty == false ? page.nextCursor : nil
            if !page.observation.complete { notice = "The server returned a partial change list (\(page.observation.truncation?.reason ?? "limit reached"))." }
            else if !(page.observation.omissions ?? []).isEmpty { notice = "Some repository entries were omitted by the server." }
            if !more, let file = files.first(where: { $0.path == priorPath }) ?? files.first {
                await read(file, client: client, session: session)
            }
        } catch { if epoch == token { fail(error) } }
    }

    func read(_ file: WireDiffFileSummary, client: any DiffClient, session: String, more: Bool = false) async {
        guard let observation else { return }
        let token = UUID(); epoch = token; loading = true; error = nil; stale = false
        if !more { selectedPath = file.path; document = nil; notice = nil; hunks = []; lineCursor = nil; lineCursors = [] }
        defer { if epoch == token { loading = false } }
        do {
            let page = try await client.readDiff(session, input: .init(targetId: observation.target.id, targetRevision: observation.revision,
                path: file.path, expectedFileRevision: file.fileRevision, annotationId: annotationID,
                pageSize: 500, maxHunks: 10, cursor: more ? lineCursor : nil))
            guard epoch == token, !Task.isCancelled else { return }
            try accept(page, more: more)
        } catch { if epoch == token { fail(error) } }
    }

    func openAnnotation(_ note: FileAnnotation, client: any DiffClient, session: String) async -> Bool {
        guard let anchor = note.diffAnchor else { return false }
        let token = UUID(); epoch = token; loading = true; error = nil; stale = false
        defer { if epoch == token { loading = false } }
        do {
            let page = try await client.readDiff(session, input: .init(targetId: anchor.targetId, targetRevision: anchor.targetRevision,
                path: anchor.path, expectedFileRevision: anchor.fileRevision, annotationId: note.id, pageSize: 1000, maxHunks: 20, cursor: nil))
            guard epoch == token, !Task.isCancelled else { return false }
            let sameObservation = observation?.target.id == page.observation.target.id && observation?.revision == page.observation.revision
            if !sameObservation { fileStates = [:]; files = [page.file]; fileCursor = nil }
            else if !files.contains(where: { $0.path == page.file.path }) { files.append(page.file) }
            target = catalog?.targets?.first { $0.targetId == anchor.targetId }
            observation = page.observation; selectedPath = page.file.path
            hunks = []; lineCursors = []; annotationID = note.id
            try accept(page, more: false)
            // Walk bounded continuation pages to find the exact anchor, never its mutable file.
            for _ in 0..<100 where document?.range(note.anchor) == nil && lineCursor != nil {
                let next = try await client.readDiff(session, input: .init(targetId: anchor.targetId, targetRevision: anchor.targetRevision,
                    path: anchor.path, expectedFileRevision: anchor.fileRevision, annotationId: note.id, pageSize: 1000, maxHunks: 20, cursor: lineCursor))
                guard epoch == token, !Task.isCancelled else { return false }
                try accept(next, more: true)
            }
            let reader = DiffState()
            reader.catalog = catalog; reader.target = target; reader.observation = observation
            reader.document = document; reader.selectedPath = selectedPath; reader.hunks = hunks
            reader.lineCursor = lineCursor; reader.lineCursors = lineCursors
            reader.annotationID = note.id; reader.notice = notice
            fileStates[page.file.path] = reader
            return document?.range(note.anchor) != nil
        } catch { if epoch == token { fail(error) }; return false }
    }

    private func accept(_ page: WireFileDiffPage, more: Bool) throws {
        let incoming = page.hunks ?? []
        if let cursor = page.nextCursor, !cursor.isEmpty {
            guard !incoming.isEmpty, lineCursors.insert(cursor).inserted else { throw ClientError.invalidPayload }
        }
        let combined = (more ? hunks : []) + incoming
        guard combined.reduce(0, { $0 + ($1.lines?.count ?? 0) }) <= 100_000,
              combined.reduce(0, { $0 + ($1.lines ?? []).reduce(0, { $0 + $1.content.utf8.count }) }) <= 8 * 1024 * 1024 else { throw ClientError.oversized }
        hunks = combined
        document = DiffDocument(observation: page.observation, file: page.file, hunks: hunks)
        lineCursor = page.nextCursor?.isEmpty == false ? page.nextCursor : nil
        if page.computation.state == "too_complex" { notice = "This diff is too complex to display within the server limits." }
        else if page.file.contentState != "text" { notice = "\(page.file.contentState.replacingOccurrences(of: "_", with: " ").capitalized) · \(page.file.reason ?? "No text diff available")" }
        else if hunks.isEmpty { notice = page.file.change == "mode_changed" ? "File mode changed; contents are unchanged." : "No textual changes." }
    }
    private func fail(_ error: Error) {
        if error is CancellationError { return }
        self.error = error.localizedDescription
        stale = true
    }
}
