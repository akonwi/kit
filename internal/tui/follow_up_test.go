package tui

import (
	"context"
	"reflect"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type followUpTestSession struct {
	Session
	FollowUpSession
	StructuredPromptSession
	input protocol.PromptInput
}

func (s *followUpTestSession) SubmitPromptInput(_ context.Context, input protocol.PromptInput) (PromptSubmission, error) {
	s.input = input
	return PromptSubmission{Queued: true, Queue: protocol.FollowUpQueue{Count: 1, Previews: []string{"Annotation"}, AnnotationIDs: input.AnnotationIDs}}, nil
}

type promptCommandQueueSession struct {
	Session
	name string
	args string
}

func (s *promptCommandQueueSession) SubmitPromptCommand(_ context.Context, name, args string) (PromptSubmission, error) {
	s.name, s.args = name, args
	return PromptSubmission{Queued: true, Queue: protocol.FollowUpQueue{Count: 1, Previews: []string{"/review"}}}, nil
}

type followUpTestRuntime struct{ callbacks chan func() }

func (r followUpTestRuntime) Dispatch(fn func()) { r.callbacks <- fn }

type followUpHarness struct{ state *followUpState }

func (w followUpHarness) CreateState() ui.State { return w.state }

type followUpState struct{ appState }

func (*followUpState) InitState()                        {}
func (*followUpState) Dispose()                          {}
func (s *followUpState) Build(ui.BuildContext) ui.Widget { return ui.Text{Value: s.composer} }

func TestSubmitPromptCommandPreservesQueuedAdmission(t *testing.T) {
	bound := &promptCommandQueueSession{}
	result, err := submitPromptCommand(t.Context(), bound, "review", "--staged")
	if err != nil || !result.Queued || result.Turn != nil || result.Queue.Count != 1 {
		t.Fatalf("submission = %+v, err = %v", result, err)
	}
	if bound.name != "review" || bound.args != "--staged" {
		t.Fatalf("command = %q %q", bound.name, bound.args)
	}
}

func TestQueueFollowUpRetainsAnnotationsAndAttachments(t *testing.T) {
	for _, text := range []string{"", "queued text"} {
		t.Run(text, func(t *testing.T) {
			bound := &followUpTestSession{}
			state := &followUpState{appState: appState{ctx: t.Context(), bound: bound, turnPending: true, composer: text, annotations: []protocol.AnnotationSummary{{ID: 7, BodyPreview: "Captured note"}}, composerAttachments: []stagedAttachment{{Info: protocol.AttachmentInfo{ID: "attachment_test", Filename: "file.txt"}}}}}
			application := uitest.New(followUpHarness{state})
			application.Pump(80, 24)
			runtime := followUpTestRuntime{callbacks: make(chan func(), 1)}
			state.queueFollowUp(text, runtime)
			select {
			case apply := <-runtime.callbacks:
				apply()
			case <-time.After(time.Second):
				t.Fatal("queue did not return")
			}
			want := protocol.PromptInput{Text: text, AnnotationIDs: []uint64{7}, AttachmentIDs: []string{"attachment_test"}}
			if !reflect.DeepEqual(bound.input, want) {
				t.Fatalf("submission = %+v, want %+v", bound.input, want)
			}
			if state.composer != "" || len(state.composerAttachments) != 0 || state.followUps.Count != 1 {
				t.Fatalf("composer = %q attachments=%v queue=%+v", state.composer, state.composerAttachments, state.followUps)
			}
			if len(state.annotationIDs()) != 0 || len(state.composerAnnotations()) != 0 {
				t.Fatal("queued annotations remained attached to the next draft")
			}
			state.annotations = append(state.annotations, protocol.AnnotationSummary{ID: 8, BodyPreview: "New note"})
			if got := state.annotationIDs(); !reflect.DeepEqual(got, []uint64{8}) {
				t.Fatalf("next draft annotations = %v", got)
			}
			state.composer = "new draft"
			state.restoreFollowUpDraft(protocol.RestoreFollowUpsResult{Messages: []protocol.PromptInput{want, want}}, map[string]stagedAttachment{"attachment_test": {Info: protocol.AttachmentInfo{ID: "attachment_test"}}})
			wantText := "new draft"
			if text != "" {
				wantText = text + "\n\n" + text + "\n\nnew draft"
			}
			if state.composer != wantText {
				t.Fatalf("restored composer = %q, want %q", state.composer, wantText)
			}
			if got := state.annotationIDs(); !reflect.DeepEqual(got, []uint64{7, 8}) {
				t.Fatalf("restored annotations = %v", got)
			}
			if got := state.composerPromptAttachmentIDs(); !reflect.DeepEqual(got, []string{"attachment_test"}) {
				t.Fatalf("restored attachments = %v", got)
			}
		})
	}
}

func TestPromptCommandDuringActiveTurnQueuesWithoutTouchingTheDraft(t *testing.T) {
	bound := &promptCommandQueueSession{}
	state := &followUpState{appState: appState{ctx: t.Context(), bound: bound, turnPending: true, composer: "unsent draft"}}
	application := uitest.New(followUpHarness{state})
	application.Pump(80, 24)
	state.submitPromptCommand("review", "--staged")
	deadline := time.Now().Add(time.Second)
	for state.followUpMutationPending || state.followUps.Count == 0 {
		if time.Now().After(deadline) {
			t.Fatal("queued prompt command did not settle")
		}
		time.Sleep(5 * time.Millisecond)
		application.Pump(80, 24)
	}
	if bound.name != "review" || bound.args != "--staged" {
		t.Fatalf("command = %q %q", bound.name, bound.args)
	}
	want := protocol.FollowUpQueue{Count: 1, Previews: []string{"/review"}}
	if !reflect.DeepEqual(state.followUps, want) || state.composer != "unsent draft" || !state.turnPending {
		t.Fatalf("queue = %+v composer = %q turnPending = %v", state.followUps, state.composer, state.turnPending)
	}
}
