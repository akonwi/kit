package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestResolveFooterPriorityAndTone(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		recovery footerRecovery
		queue    int
		composer string
		want     footerPresentation
	}{
		{name: "empty", want: footerPresentation{}},
		{name: "ordinary composer", composer: "hello", want: footerPresentation{}},
		{name: "bash mode", composer: "!echo hi", want: footerPresentation{Text: "bash command " + glyphMiddleDot + " result will be added to context", Tone: footerSuccess}},
		{name: "excluded bash mode", composer: "!!echo hi", want: footerPresentation{Text: "bash command " + glyphMiddleDot + " result excluded from context", Tone: footerSuccess}},
		{name: "queued over bash", queue: 3, composer: "!echo hi", want: footerPresentation{Text: "3 queued " + glyphMiddleDot + " ↑ restore"}},
		{name: "reconnecting over queue", recovery: footerReconnectingActivity, queue: 3, composer: "!!echo hi", want: footerPresentation{Text: "Reconnecting activity…"}},
		{name: "syncing over queue", recovery: footerSyncingFinalTranscript, queue: 3, composer: "!!echo hi", want: footerPresentation{Text: "Syncing final transcript…"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := resolveFooter(test.recovery, protocol.FollowUpQueue{Count: test.queue}, test.composer)
			if got != test.want {
				t.Fatalf("footer = %+v, want %+v", got, test.want)
			}
		})
	}
}
