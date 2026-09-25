import Testing
@testable import Kit

@MainActor
struct InteractionFlowTests {
    @Test func keyboardSubmissionMatchesV2() {
        let flow = InteractionFlow.planning()
        #expect(flow.submitStep(option: "File workspace") == false)
        #expect(flow.step == 1)
        flow.choose("Unit tests")
        #expect(flow.submitStep(option: "Keyboard navigation") == false)
        #expect(flow.answers[1].choices == ["Unit tests"])
        #expect(flow.step == 2)
        flow.answers[2].text = "Keep keyboard support"
        #expect(flow.submitStep(option: nil) == false)
        #expect(flow.step == 3)
        #expect(flow.submitStep(option: "Yes", selectBoolean: false) == false)
        #expect(flow.answers[3].choices.isEmpty)
        #expect(flow.submitStep(option: "No"))
        #expect(flow.answers[3].choices == ["No"])
    }

    @Test func guidedAnswersSurviveNavigationAndOptionalSkip() {
        let flow = InteractionFlow.planning()
        #expect(flow.canContinue == false)
        #expect(flow.advance() == false)
        #expect(flow.step == 0)
        flow.choose("File workspace")
        #expect(flow.advance() == false)
        flow.choose("Unit tests")
        flow.choose("Keyboard navigation")
        flow.step = 0
        #expect(flow.answers[0].choices == ["File workspace"])
        flow.step = 1
        #expect(flow.answers[1].choices == ["Unit tests", "Keyboard navigation"])
        #expect(flow.advance() == false)
        #expect(flow.advance(skip: true) == false)
        #expect(flow.answers[2].skipped)
        flow.choose("No")
        #expect(flow.advance())
        #expect(flow.summary.contains("Skipped"))
        #expect(flow.summary.hasSuffix("No"))
    }
    @Test func requiredConfirmationCannotBeSkippedOrSubmittedUnanswered() {
        let flow = InteractionFlow.approval()
        #expect(flow.advance(skip: true) == false)
        #expect(flow.advance() == false)
        flow.choose("Deny")
        #expect(flow.advance())
        #expect(flow.summary.hasSuffix("Deny"))
    }
    @Test func revisitingSkippedTextRecordsTheNewAnswer() {
        let flow = InteractionFlow.planning()
        flow.step = 2
        #expect(flow.advance(skip: true) == false)
        flow.step = 2
        flow.answers[2].text = "Preserve the draft"
        #expect(flow.advance() == false)
        #expect(flow.answers[2].skipped == false)
        #expect(flow.summary.contains("Anything else to keep in mind?\nPreserve the draft"))
    }
}
