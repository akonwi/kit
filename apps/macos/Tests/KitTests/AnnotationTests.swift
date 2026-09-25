import Foundation
import Testing
@testable import Kit

struct AnnotationTests {
    @Test func utf16SelectionMapsInclusiveLines() {
        let source = "a🌿b\nsecond\nthird\n"
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: 1, length: 3)) == 1...1)
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: 0, length: 6)) == 1...2)
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: 0, length: 5)) == 1...1)
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: 5, length: 6)) == 2...2)
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: source.utf16.count, length: 0)) == nil)
        #expect(AnnotationSelection.lines(in: "a\r\nb", selection: NSRange(location: 3, length: 1)) == 2...2)
    }

    @Test func selectionBoundsAndLimits() {
        let source = String(repeating: "x\n", count: 201)
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: 0, length: 400)) == 1...200)
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: 0, length: 402)) == nil)
        #expect(AnnotationSelection.lines(in: source, selection: NSRange(location: NSNotFound, length: 0)) == nil)
        #expect(AnnotationSelection.lines(in: "", selection: NSRange(location: 0, length: 0)) == nil)
    }

    @Test func bodyUsesUtf8BoundsAndRendererSafety() {
        #expect(FileAnnotation.validBody(String(repeating: "é", count: 8192)))
        #expect(!FileAnnotation.validBody(String(repeating: "é", count: 8193)))
        #expect(FileAnnotation.validBody("Keep this\n\tindented"))
        #expect(!FileAnnotation.validBody(" \n\t"))
        #expect(!FileAnnotation.validBody("escape\u{1b}"))
        #expect(!FileAnnotation.validBody("hidden\u{200b}"))
    }

    @Test func frozenEvidenceAndSessionIsolation() throws {
        let anchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "main.swift", fileRevision: "file_original", startLine: 3, endLine: 4), workingTreeDiff: nil)
        let preview = WireAnnotationPreview(startLine: 3, endLine: 4, text: "old source", truncated: nil)
        let record = WireAnnotation(id: 9, sessionId: "session_a", anchor: anchor, diffTarget: nil, body: "Keep this",
            preview: preview, stale: true, staleReason: .init(rawValue: "file_changed"), validationDeferred: nil)
        let note = try FileAnnotation(record, session: "session_a")
        #expect(note.label == "main.swift:3–4")
        #expect(note.source == "old source")
        #expect(note.stale)
        #expect(throws: (any Error).self) { try FileAnnotation(record, session: "session_b") }
        let sent = try FileAnnotation(WireSubmittedAnnotation(originalAnnotationId: 9, anchor: anchor, diffTarget: nil, body: "Keep this", preview: preview))
        #expect(sent.complete)
        #expect(sent.source == "old source")
    }
}

extension AnnotationTests {
    @Test func orderedEventsConsumeOnlySubmittedDrafts() throws {
        let session = SessionExcerpt(id: "session_a", title: "Test", sourceTitle: "Test", model: "m",
            thinking: "high", workspace: "w", date: "", messages: [])
        var projection = try SessionEventProjection(session: session, source: [])
        func event(_ kind: String, _ extra: [String: Any]) throws -> WireSessionEvent {
            var payload: [String: Any] = ["kind": kind, "sessionId": "session_a", "streamId": "stream",
                "sequence": 1, "turnId": "", "runId": ""]
            payload.merge(extra) { _, new in new }
            return try JSONDecoder().decode(WireSessionEvent.self, from: JSONSerialization.data(withJSONObject: payload))
        }
        func record(_ id: Int, _ body: String) -> [String: Any] {
            ["id": id, "sessionId": "session_a", "body": body,
             "anchor": ["kind": "workspace_file", "workspaceFile": ["workspaceId": "workspace_test", "path": "main.swift",
                "fileRevision": "file_test", "startLine": 1, "endLine": 1]],
             "preview": ["startLine": 1, "endLine": 1, "text": "let value = 1"]]
        }
        try projection.apply(event("annotation.created", ["annotation": record(2, "Second")]))
        try projection.apply(event("annotation.created", ["annotation": record(1, "First")]))
        #expect(projection.session.annotations?.map(\.id) == [1, 2])
        try projection.apply(event("annotation.updated", ["annotation": record(1, "Edited")]))
        #expect(projection.session.annotations?.map(\.body) == ["Edited", "Second"])
        try projection.apply(event("annotation.submitted", ["annotationIds": [1], "acceptedMessageId": "message_test"]))
        #expect(projection.session.annotations?.map(\.id) == [2])
        try projection.apply(event("annotation.deleted", ["annotationId": 2]))
        #expect(projection.session.annotations == [])
    }

    @MainActor @Test func annotationDraftsAreSessionScoped() {
        let fixture = Fixture(sessions: ["a", "b"].map {
            SessionExcerpt(id: $0, title: $0, sourceTitle: "Test", model: "m", thinking: "high", workspace: "w", date: "", messages: [])
        })
        let store = SessionStore(fixture: fixture)
        store.annotationState.draft = "Draft A"
        store.select("b")
        #expect(store.annotationState.draft == "")
        store.annotationState.draft = "Draft B"
        store.select("a")
        #expect(store.annotationState.draft == "Draft A")
    }
}

private actor AnnotationFailureClient: AnnotationClient {
    let serverID = "test"
    let isDemo = false
    var creates = 0
    var deletes = 0
    var reads = 0
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func annotations(_ session: String) async throws -> [FileAnnotation] { reads += 1; return [] }
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation {
        creates += 1; throw URLError(.networkConnectionLost)
    }
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(404) }
    func deleteAnnotation(_ session: String, id: UInt64) async throws { deletes += 1 }
}

extension AnnotationTests {
    @MainActor @Test func ambiguousCreationPreservesDraftAndNeverDeletesReplacement() async {
        let state = AnnotationState()
        state.draft = "Keep my full comment"
        let client = AnnotationFailureClient()
        let anchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "main.swift", fileRevision: "file_new", startLine: 1, endLine: 1), workingTreeDiff: nil)
        #expect(await state.save(client: client, session: "session_a", anchor: anchor, replacing: 1) == false)
        #expect(state.draft == "Keep my full comment")
        #expect(state.uncertain)
        #expect(await client.reads == 1)
        #expect(await client.deletes == 0)
        #expect(await state.save(client: client, session: "session_a", anchor: anchor, replacing: 1) == false)
        #expect(await client.creates == 1)
    }
}

extension AnnotationTests {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_ANNOTATION_LIVE_TEST"] == "1"))
    func temporarySessionAnnotationsRoundTripAndBecomeStale() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("kit-annotation-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appendingPathComponent("sample.swift")
        try "let greeting = \"🌿\"\nprint(greeting)\n".write(to: file, atomically: true, encoding: .utf8)
        let client = try await HTTPClient.local()
        let model = try #require(try await client.models().first)
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        _ = try await client.createSession(.init(id: id, cwd: directory.path, name: "Annotation verification",
            model: model.id, thinkingLevel: model.thinkingLevels?.first?.rawValue ?? "off", temporary: true))
        let observer = try await HTTPClient.local()
        let probe = AnnotationStreamProbe()
        let watch = Task { try await observer.watch(id) { value in await probe.receive(value) } }
        defer { watch.cancel() }
        for _ in 0..<100 {
            if await probe.ready { break }
            try await Task.sleep(for: .milliseconds(20))
        }
        #expect(await probe.ready)
        do {
            let fileRead = try await client.readWorkspaceFile(id, path: "sample.swift")
            let anchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: fileRead.workspace.workspaceId,
                path: fileRead.path, fileRevision: fileRead.revision, startLine: 1, endLine: 2), workingTreeDiff: nil)
            let created = try await client.createAnnotation(id, input: .init(anchor: anchor, body: "Explain this greeting."))
            #expect(created.source == "let greeting = \"🌿\"\nprint(greeting)")
            for _ in 0..<100 {
                if await probe.bodies.contains("Explain this greeting.") { break }
                try await Task.sleep(for: .milliseconds(20))
            }
            #expect(await probe.bodies.contains("Explain this greeting."))
            let edited = try await client.updateAnnotation(id, input: .init(annotationId: created.id, body: "Keep the Unicode greeting."))
            #expect(edited.body == "Keep the Unicode greeting.")
            for _ in 0..<100 {
                if await probe.bodies.contains(edited.body) { break }
                try await Task.sleep(for: .milliseconds(20))
            }
            #expect(await probe.bodies.contains(edited.body))
            #expect(try await client.annotations(id).map(\.id) == [created.id])
            try "let greeting = \"changed\"\n".write(to: file, atomically: true, encoding: .utf8)
            let stale = try #require(try await client.annotations(id).first)
            #expect(stale.stale)
            #expect(stale.source == created.source)
            try await client.deleteAnnotation(id, id: created.id)
            #expect(try await client.annotations(id).isEmpty)
            for _ in 0..<100 {
                if await probe.liveIDs.isEmpty { break }
                try await Task.sleep(for: .milliseconds(20))
            }
            #expect(await probe.liveIDs.isEmpty)
            guard ProcessInfo.processInfo.environment["KIT_ANNOTATION_SEND_LIVE_TEST"] == "1" else {
                try await client.disposeSession(id)
                return
            }
            let current = try await client.readWorkspaceFile(id, path: "sample.swift")
            let currentAnchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: current.workspace.workspaceId,
                path: current.path, fileRevision: current.revision, startLine: 1, endLine: 1), workingTreeDiff: nil)
            let first = try await client.createAnnotation(id, input: .init(anchor: currentAnchor, body: "Reply only verified; do not use tools."))
            let second = try await client.createAnnotation(id, input: .init(anchor: currentAnchor, body: "This is a disposable verification session."))
            #expect(first.id > created.id)
            let receipt = try await client.submit(id, input: .init(text: "", attachmentIds: [], annotationIds: [second.id, first.id]))
            if let run = receipt.reservation?.runId { try? await client.abort(id, run: run) }
            #expect(try await client.annotations(id).isEmpty)
            try "newer source\n".write(to: file, atomically: true, encoding: .utf8)
            var accepted = try await client.snapshot(id)
            for _ in 0..<40 where accepted.activeRunID != nil {
                try await Task.sleep(for: .milliseconds(50))
                accepted = try await client.snapshot(id)
            }
            let evidence = try #require(accepted.messages.first(where: { $0.role == "user" })?.annotations)
            #expect(evidence.map(\.id) == [second.id, first.id])
            #expect(evidence.map(\.source) == [second.source, first.source])
            try await client.disposeSession(id)
        } catch {
            try? await client.disposeSession(id)
            throw error
        }
    }
}

private actor AnnotationStreamProbe {
    var ready = false
    var bodies = Set<String>()
    var liveIDs: [UInt64] = []
    func receive(_ session: SessionExcerpt) {
        ready = true
        liveIDs = (session.annotations ?? []).map(\.id)
        bodies.formUnion((session.annotations ?? []).map(\.body))
    }
}

extension AnnotationTests {
    @Test func annotationOnlyTranscriptUsesFrozenStructuredEvidence() throws {
        let payload = #"[{"id":"message_test","turnId":"turn_test","sequence":1,"role":"user","createdAt":"2026-09-17T00:00:00Z","content":[{"kind":"annotations","annotations":[{"originalAnnotationId":4,"anchor":{"kind":"workspace_file","workspaceFile":{"workspaceId":"workspace_test","path":"main.swift","fileRevision":"file_old","startLine":1,"endLine":1}},"body":"Preserve this","preview":{"startLine":1,"endLine":1,"text":"original source"}}]}]}]"#
        let source = try JSONDecoder().decode([WireTranscriptMessage].self, from: Data(payload.utf8))
        let messages = try SessionProjection.transcript(source)
        #expect(messages.count == 1)
        #expect(messages[0].role == "user")
        #expect(messages[0].annotations?.map(\.body) == ["Preserve this"])
        #expect(messages[0].annotations?.map(\.source) == ["original source"])
        #expect(messages[0].annotations?.map(\.revision) == ["file_old"])
    }
}

private actor ReplacementClient: AnnotationClient {
    let serverID = "test"
    let isDemo = false
    var creates = 0
    var deletes = 0
    let stale: FileAnnotation
    var created: FileAnnotation?
    init(stale: FileAnnotation) { self.stale = stale }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func annotations(_ session: String) async throws -> [FileAnnotation] {
        (deletes < 2 ? [stale] : []) + (created.map { [$0] } ?? [])
    }
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation {
        creates += 1
        let value = try FileAnnotation(id: 2, anchor: input.anchor, body: input.body, source: "new source", stale: false, complete: true)
        created = value
        return value
    }
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(404) }
    func deleteAnnotation(_ session: String, id: UInt64) async throws {
        deletes += 1
        if deletes == 1 { throw URLError(.networkConnectionLost) }
    }
}

extension AnnotationTests {
    @MainActor @Test func acknowledgedReplacementNeverCreatesTwiceAfterDeleteFailure() async throws {
        let anchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "main.swift", fileRevision: "file_new", startLine: 1, endLine: 1), workingTreeDiff: nil)
        let stale = try FileAnnotation(id: 1, anchor: anchor, body: "Keep my comment", source: "old source", stale: true, complete: true)
        let client = ReplacementClient(stale: stale)
        let state = AnnotationState(); state.observe([stale]); state.draft = stale.body
        #expect(await state.save(client: client, session: "session_a", anchor: anchor, replacing: 1) == false)
        #expect(state.records.map(\.id) == [1, 2])
        #expect(state.draft == stale.body)
        #expect(state.uncertain)
        state.acknowledgeUncertainty()
        #expect(await state.save(client: client, session: "session_a", anchor: anchor, replacing: 1))
        #expect(await client.creates == 1)
        #expect(state.records.map(\.id) == [2])
    }
}

extension AnnotationTests {
    @MainActor @Test func changingFileDuringEditingRequiresExplicitNewRangeAndKeepsBody() throws {
        let oldAnchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "main.swift", fileRevision: "file_old", startLine: 1, endLine: 1), workingTreeDiff: nil)
        let note = try FileAnnotation(id: 9, anchor: oldAnchor, body: "Original", source: "old", stale: false, complete: true)
        let state = AnnotationState()
        state.begin(anchor: oldAnchor, editing: note)
        state.draft = "Unsaved revision of my comment"
        state.reselectEditor()
        #expect(state.editor == nil)
        #expect(state.selectionDraft?.body == "Unsaved revision of my comment")
        let newAnchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "main.swift", fileRevision: "file_new", startLine: 8, endLine: 10), workingTreeDiff: nil)
        state.begin(anchor: newAnchor)
        #expect(state.editor?.replacing == 9)
        #expect(state.editor?.editing == nil)
        #expect(state.editor?.anchor.workspaceFile?.startLine == 8)
        #expect(state.draft == "Unsaved revision of my comment")
    }
}


extension AnnotationTests {
    @Test func diffEvidencePreservesTargetRevisionAndDeferredValidation() throws {
        let anchor = WireAnnotationAnchor(kind: .value1, workspaceFile: nil,
            workingTreeDiff: .init(targetId: "difftarget_review", targetRevision: "diffrev_original",
                path: "main.swift", fileRevision: "diff_file_original", side: "old", startLine: 3, endLine: 4))
        let target = WirePinnedDiffTarget(workspaceId: "workspace_test", kind: "branch",
            base: .init(kind: "commit", oid: String(repeating: "a", count: 40)),
            head: .init(kind: "commit", oid: String(repeating: "b", count: 40)))
        let preview = WireAnnotationPreview(startLine: 3, endLine: 4, text: "removed source", truncated: nil)
        let record = WireAnnotation(id: 42, sessionId: "session_test", anchor: anchor, diffTarget: target,
            body: "Keep this evidence", preview: preview, stale: nil, staleReason: nil, validationDeferred: true)
        let note = try FileAnnotation(record, session: "session_test")
        #expect(note.anchor.workingTreeDiff?.targetRevision == "diffrev_original")
        #expect(note.diffTarget == target)
        #expect(note.source == "removed source")
        #expect(note.validationDeferred && !note.stale)
        #expect(note.statusLabel == "Evidence validation pending")
        let summary = try FileAnnotation(WireAnnotationSummary(id: 42, anchor: anchor, diffTarget: target,
            bodyPreview: "Keep this evidence", preview: "removed source", stale: nil, staleReason: nil, validationDeferred: true))
        #expect(summary.diffAnchor == note.diffAnchor)
        #expect(summary.validationDeferred)
        let sent = try FileAnnotation(WireSubmittedAnnotation(originalAnnotationId: 42, anchor: anchor,
            diffTarget: target, body: "Keep this evidence", preview: preview))
        #expect(sent.diffContext == "old side · difftarget_review · diffrev_original")
        #expect(sent.diffTarget == target)
        #expect(try JSONDecoder().decode(FileAnnotation.self, from: JSONEncoder().encode(sent)) == sent)
        let invalid = WireAnnotationAnchor(kind: .value0, workspaceFile: nil, workingTreeDiff: anchor.workingTreeDiff)
        #expect(throws: (any Error).self) {
            try FileAnnotation(id: 42, anchor: invalid, body: "Comment", source: "", stale: false, complete: true)
        }
        #expect(throws: (any Error).self) {
            try FileAnnotation(id: 42, anchor: anchor, body: "Comment", source: "", stale: true, complete: true, validationDeferred: true)
        }
    }

    @MainActor @Test func diffRevealInspectsCapturedEvidenceWithoutReanchoring() throws {
        let anchor = WireAnnotationAnchor(kind: .value1, workspaceFile: nil,
            workingTreeDiff: .init(targetId: "difftarget_review", targetRevision: "diffrev_original",
                path: "main.swift", fileRevision: "diff_file_original", side: "new", startLine: 1, endLine: 1))
        let note = try FileAnnotation(id: 42, anchor: anchor, body: "Review", source: "source", stale: false, complete: true)
        let state = AnnotationState()
        state.reveal(note)
        #expect(state.inspected == note)
        #expect(state.revealed == nil)
        #expect(state.editor == nil)
    }
}
