import Foundation
import Testing
@testable import Kit

@MainActor
struct InteractionFlowTests {
    @Test func guidedQuestionsAnswerAsInTheTUI() {
        let flow = InteractionFlow.planning()
        #expect(flow.guided)
        // A single choice answers when chosen and moves on.
        #expect(flow.choose(1) == false)
        #expect(flow.step == 1)
        #expect(flow.answers[0].choices == ["File workspace"])
        // A multiple choice toggles; Next answers with what is checked.
        #expect(flow.mode == .multiple)
        #expect(flow.choose(0) == false)
        #expect(flow.choose(1) == false)
        #expect(flow.choose(1) == false)
        #expect(flow.step == 1)
        #expect(flow.next() == false)
        #expect(flow.answers[1].choices == ["Unit tests"])
        flow.answers[2].text = "  Keep keyboard support  "
        #expect(flow.next() == false)
        #expect(flow.answers[2].text == "Keep keyboard support")
        // A yes/no question has nothing for Next to send until answered.
        #expect(flow.mode == .confirm)
        #expect(flow.next() == false)
        #expect(flow.step == 3)
        #expect(flow.confirm(false))
        #expect(flow.answers[3].choices == ["false"])
    }

    @Test func backShowsTheEarlierAnswerHighlighted() {
        let flow = InteractionFlow.planning()
        _ = flow.choose(2)
        #expect(flow.cursor == 0)
        flow.back()
        #expect(flow.step == 0)
        #expect(flow.cursor == 2)
        flow.back()
        #expect(flow.step == 0)
    }

    @Test func onlyOptionalQuestionsSkip() {
        let flow = InteractionFlow.planning()
        #expect(flow.skip() == false)
        #expect(flow.step == 0)
        flow.show(2)
        // An empty answer to an optional question skips it.
        #expect(flow.submitText() == false)
        #expect(flow.answers[2].skipped)
        #expect(flow.answered(2) == false)
        flow.back()
        flow.answers[2].text = "Preserve the draft"
        #expect(flow.submitText() == false)
        #expect(flow.answers[2].skipped == false)
        #expect(flow.answered(2))
        #expect(flow.summary.contains("Anything else to keep in mind?\nPreserve the draft"))
    }

    @Test func emptyMultipleChoiceSkipsOnlyWhenOptional() {
        let flow = InteractionFlow.planning()
        flow.show(1)
        #expect(flow.continueMultiple() == false)
        #expect(flow.step == 1)
    }

    @Test func modelConfirmationLeadsWithYesAndAnswersOnce() {
        let flow = InteractionFlow.approval()
        #expect(flow.guided == false)
        #expect(flow.offersChoice == false)
        #expect(flow.detail.contains("go test"))
        #expect(flow.confirm(false))
        #expect(flow.summary.hasSuffix("Deny"))
    }

    @Test func movingTheHighlightStaysWithinTheOptions() {
        let flow = InteractionFlow.planning()
        flow.move(-1)
        #expect(flow.cursor == 0)
        flow.move(5)
        #expect(flow.cursor == 2)
    }

    @Test func requestsThisClientCantPresentCanStillBeCancelled() throws {
        let request = WireInteractionRequest(id: "interaction_empty", sessionId: "s",
            kind: try #require(WireInteractionKind(rawValue: "select")), title: "Pick one",
            options: [], createdAt: "2026-10-12T00:00:00Z")
        let flow = try InteractionFlow(request: request)
        #expect(flow.mode == .unsupported)
        #expect(flow.title == "Pick one")
        #expect(flow.response(cancelled: false) == nil)
        #expect(flow.response(cancelled: true)?.cancelled == true)
    }

    @Test func anAuthorizationAnswersWithThePastedRedirect() throws {
        let json = #"{"id":"interaction_authorize","sessionId":"s","turnId":"t","toolCallId":"c","kind":"authorize","title":"Authorize the docs MCP server","url":"https://auth.example.com/authorize?state=s","redirectUri":"http://127.0.0.1:53173/mcp/callback","createdAt":"2026-10-12T00:00:00Z"}"#
        let request = try JSONDecoder().decode(WireInteractionRequest.self, from: Data(json.utf8))
        let flow = try InteractionFlow(request: request)
        #expect(flow.mode == .text)
        #expect(flow.authorizationURL == URL(string: "https://auth.example.com/authorize?state=s"))
        // Nothing pasted yet is not an answer.
        #expect(flow.submitText() == false)
        flow.answers[0].text = "  http://127.0.0.1:53173/mcp/callback?code=c&state=s  "
        #expect(flow.submitText())
        #expect(flow.response(cancelled: false)?.value == "http://127.0.0.1:53173/mcp/callback?code=c&state=s")
    }

    @Test func anAuthorizationAnswersWithTheRedirectTheBrowserReceives() throws {
        let json = #"{"id":"interaction_authorize","sessionId":"s","turnId":"t","toolCallId":"c","kind":"authorize","title":"Authorize the docs MCP server","url":"https://auth.example.com/authorize?state=s","redirectUri":"http://127.0.0.1:53173/mcp/callback","createdAt":"2026-10-12T00:00:00Z"}"#
        let request = try JSONDecoder().decode(WireInteractionRequest.self, from: Data(json.utf8))
        let flow = try InteractionFlow(request: request)
        #expect(flow.authorizationRedirect?.callback == URL(string: "http://127.0.0.1:53173/mcp/callback"))
        #expect(!flow.receiveRedirect(URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=other")!))
        #expect(flow.receiveRedirect(URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=s")!))
        #expect(flow.response(cancelled: false)?.value == "http://127.0.0.1:53173/mcp/callback?code=c&state=s")
    }

    @Test func anAuthorizationOfAPageThatIsNotHTTPSCanOnlyBeCancelled() throws {
        let request = WireInteractionRequest(id: "interaction_authorize", sessionId: "s", turnId: "t", toolCallId: "c",
            kind: .value4, title: "Authorize", url: "http://auth.example.com/authorize",
            redirectUri: "http://127.0.0.1:53173/mcp/callback", createdAt: "2026-10-12T00:00:00Z")
        let flow = try InteractionFlow(request: request)
        #expect(flow.mode == .unsupported)
        #expect(flow.response(cancelled: true)?.cancelled == true)
    }

    @Test func optionListLettersAndOverflowFollowTheTUI() {
        #expect(DockOptionList.marker(0) == "A")
        #expect(DockOptionList.marker(25) == "Z")
        #expect(DockOptionList.marker(26) == "AA")
        #expect(DockOptionList.marker(27) == "AB")
        #expect(DockOptionList.index(forKey: "a", count: 3) == 0)
        #expect(DockOptionList.index(forKey: "C", count: 3) == 2)
        #expect(DockOptionList.index(forKey: "d", count: 3) == nil)
        #expect(DockOptionList.index(forKey: "1", count: 3) == nil)
        #expect(DockOptionList.hidden(count: 4, offset: 0) == (0, 0))
        #expect(DockOptionList.hidden(count: 11, offset: 0) == (0, 5))
        #expect(DockOptionList.hidden(count: 11, offset: 64) == (2, 3))
        #expect(DockOptionList.hidden(count: 11, offset: 900) == (5, 0))
    }

    @Test func answeredRequestsStayDockedUntilTheHoldEnds() async throws {
        let hold = InteractionHold(), flow = InteractionFlow.approval()
        var released = 0
        hold.hold(flow, for: .milliseconds(20)) { released += 1 }
        #expect(hold.flow === flow)
        // The main actor is shared with other tests, so wait for the timer.
        for _ in 0..<200 where hold.flow != nil { try await Task.sleep(for: .milliseconds(10)) }
        #expect(hold.flow == nil)
        #expect(released == 1)
    }

    @Test func aNextRequestTakesTheDockWithoutReturningToTheComposer() async throws {
        let hold = InteractionHold()
        var released = 0
        hold.hold(InteractionFlow.approval(), for: .milliseconds(20)) { released += 1 }
        hold.clear()
        try await Task.sleep(for: .milliseconds(80))
        #expect(hold.flow == nil)
        #expect(released == 0)
        // Holding again replaces the earlier hold and its timer.
        let second = InteractionFlow.planning()
        hold.hold(InteractionFlow.approval(), for: .milliseconds(20)) { released += 1 }
        hold.hold(second, for: .seconds(5)) { released += 10 }
        try await Task.sleep(for: .milliseconds(80))
        #expect(hold.flow === second)
        #expect(released == 0)
        hold.release()
        #expect(released == 10)
    }
}
