import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct SessionInteractionsTests {
    nonisolated static func request(_ id: String, kind: WireInteractionKind = .value0, title: String = "Run this command?",
                                    at seconds: Int = 0, options: [WireInteractionOption]? = nil,
                                    questions: [WireInteractionQuestion]? = nil) -> WireInteractionRequest {
        WireInteractionRequest(id: "interaction_" + id, sessionId: "s", kind: kind, title: title, options: options,
                               questions: questions, createdAt: "2026-10-30T10:00:0\(seconds)Z")
    }

    nonisolated static func snapshot(_ requests: [WireInteractionRequest]) -> SessionExcerpt {
        var session = SessionTesting.session("s")
        session.pendingInteractions = requests
        return session
    }

    static func interactions(_ client: FakeActionsClient, requests: [WireInteractionRequest], live: Bool = true,
                             holdWindow: Duration = InteractionHold.window) -> SessionInteractions {
        let interactions = SessionInteractions(session: "s", client: client, holdWindow: holdWindow)
        interactions.live = live
        interactions.reconcile(snapshot(requests))
        return interactions
    }

    @Test func theOldestRequestIsDockedAndKeepsItsAnswersAcrossSnapshots() {
        let input = Self.request("b", kind: .value1, title: "Branch name?", at: 1)
        let interactions = Self.interactions(FakeActionsClient(), requests: [input, Self.request("a")])
        #expect(interactions.docked?.request?.id == "interaction_a")
        let typed = interactions.flows[1]
        typed.answers[0].text = "fix/sse-resume"
        interactions.reconcile(Self.snapshot([input]))
        #expect(interactions.docked === typed)
        #expect(interactions.docked?.answers[0].text == "fix/sse-resume")
    }

    /// Waits for `condition` rather than a fixed time, which a busy machine may overrun.
    static func wait(_ condition: () -> Bool) async {
        for _ in 0..<500 where !condition() { try? await Task.sleep(for: .milliseconds(10)) }
        #expect(condition())
    }

    @Test func approvingSendsTheConfirmationAndHoldsTheRequestAfterItResolves() async {
        let client = FakeActionsClient()
        let request = Self.request("a")
        // A hold longer than the test, so it can't end before the checks.
        let interactions = Self.interactions(client, requests: [request], holdWindow: .seconds(60))
        let flow = interactions.docked!
        interactions.forward(flow) { $0.confirm(true) }
        #expect(flow.submitting)
        await Self.wait { interactions.hold.flow === flow }
        #expect(client.responses.map(\.requestId) == ["interaction_a"])
        #expect(client.responses.map(\.confirmed) == [true])
        // The server resolves it; the answered request stays.
        interactions.reconcile(Self.snapshot([]))
        #expect(interactions.docked === flow)
        #expect(flow.submitting)
    }

    @Test func theAnsweredRequestLeavesWhenTheHoldEnds() async {
        let client = FakeActionsClient()
        let interactions = Self.interactions(client, requests: [Self.request("a")], holdWindow: .milliseconds(10))
        let flow = interactions.docked!
        interactions.forward(flow) { $0.confirm(true) }
        await Self.wait { client.responses.count == 1 }
        interactions.reconcile(Self.snapshot([]))
        await Self.wait { interactions.docked == nil }
    }

    @Test func cancellingDeclinesTheRequest() async {
        let client = FakeActionsClient()
        let interactions = Self.interactions(client, requests: [Self.request("a")])
        interactions.cancel(interactions.docked!)
        await SessionComposerTests.settle()
        #expect(client.responses.map(\.cancelled) == [true])
        #expect(client.responses.map(\.confirmed) == [nil])
    }

    @Test func aFailedAnswerStaysDockedWithItsError() async {
        let client = FakeActionsClient()
        client.respondError = ClientError.http(503)
        let interactions = Self.interactions(client, requests: [Self.request("a")])
        let flow = interactions.docked!
        interactions.forward(flow) { $0.confirm(false) }
        await SessionComposerTests.settle()
        #expect(flow.error == ClientError.http(503).localizedDescription)
        #expect(!flow.submitting)
        #expect(interactions.docked === flow)
    }

    @Test func nothingIsSentWhileTheStreamIsDown() async {
        let client = FakeActionsClient()
        let interactions = Self.interactions(client, requests: [Self.request("a")], live: false)
        interactions.forward(interactions.docked!) { $0.confirm(true) }
        interactions.cancel(interactions.docked!)
        await SessionComposerTests.settle()
        #expect(client.responses.isEmpty)
    }

    @Test func aChoiceIsSentWithTheHighlightedOption() async {
        let client = FakeActionsClient()
        let options = [WireInteractionOption(id: "memory", label: "In memory", detail: nil),
                       WireInteractionOption(id: "disk", label: "On disk", detail: "Survives a relaunch")]
        let interactions = Self.interactions(client, requests: [Self.request("a", kind: .value2, title: "Where?", options: options)])
        let flow = interactions.docked!
        flow.cursor = 1
        interactions.forward(flow) { $0.next() }
        await SessionComposerTests.settle()
        #expect(client.responses.map(\.selectedOptionId) == ["disk"])
    }

    @Test func guidedQuestionsAreSentTogetherAfterTheLast() async {
        let client = FakeActionsClient()
        let questions = [
            WireInteractionQuestion(id: "q1", prompt: "Which checks?", detail: nil, kind: .value2, required: true,
                                    options: [.init(id: "unit", label: "Unit tests", detail: nil), .init(id: "ui", label: "UI tests", detail: nil)]),
            WireInteractionQuestion(id: "q2", prompt: "Anything else?", detail: nil, kind: .value0, required: false, options: nil),
        ]
        let interactions = Self.interactions(client, requests: [Self.request("a", kind: .value3, title: "Plan", questions: questions)])
        let flow = interactions.docked!
        interactions.forward(flow) { $0.choose(1) }
        interactions.forward(flow) { $0.choose(0) }
        interactions.forward(flow) { $0.continueMultiple() }
        #expect(flow.step == 1)
        #expect(client.responses.isEmpty)
        interactions.forward(flow) { $0.skip() }
        await SessionComposerTests.settle()
        #expect(client.responses.count == 1)
        #expect(client.responses.first?.answers?["q1"]?.optionIds == ["unit", "ui"])
        #expect(client.responses.first?.answers?["q2"]?.skipped == true)
    }
}

extension SessionInteractionsTests {
    @Test func aFoldedRequestKeepsItsAnswersAndANewOneArrivesOpen() {
        let first = Self.request("a", kind: .value1, title: "Branch name?")
        let interactions = Self.interactions(FakeActionsClient(), requests: [first])
        interactions.docked?.answers[0].text = "fix/sse"
        interactions.collapsed = true
        #expect(interactions.collapsed)
        interactions.reconcile(Self.snapshot([first]))
        #expect(interactions.collapsed)
        #expect(interactions.docked?.answers[0].text == "fix/sse")
        interactions.reconcile(Self.snapshot([Self.request("b", at: 1)]))
        #expect(!interactions.collapsed)
        interactions.collapsed = true
        interactions.reconcile(Self.snapshot([]))
        #expect(!interactions.collapsed)
    }
}
