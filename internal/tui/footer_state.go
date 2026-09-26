package tui

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
)

// footerRecovery is the only mutable global-footer state. Session mode and
// composer guidance are projected from their authoritative owners instead.
type footerRecovery uint8

const (
	footerHealthy footerRecovery = iota
	footerReconnectingActivity
	footerSyncingFinalTranscript
	footerDaemonIncompatible
	footerCheckingDaemon
)

type footerTone uint8

const (
	footerMuted footerTone = iota
	footerSuccess
)

type footerPresentation struct {
	Text string
	Tone footerTone
}

// resolveFooter gives transient recovery precedence over the queue and the
// composer mode. Errors and completed operations are presented elsewhere.
func resolveFooter(recovery footerRecovery, followUps protocol.FollowUpQueue, composer string) footerPresentation {
	switch recovery {
	case footerReconnectingActivity:
		return footerPresentation{Text: "Reconnecting activity…"}
	case footerSyncingFinalTranscript:
		return footerPresentation{Text: "Syncing final transcript…"}
	case footerDaemonIncompatible:
		return footerPresentation{Text: "Server incompatible · sending unavailable · Ctrl+r recheck"}
	case footerCheckingDaemon:
		return footerPresentation{Text: "Checking server…"}
	}
	if followUps.Count > 0 {
		return footerPresentation{Text: fmt.Sprintf("%d queued %s ↑ restore", followUps.Count, glyphMiddleDot)}
	}
	if strings.HasPrefix(composer, "!!") {
		return footerPresentation{Text: "bash command " + glyphMiddleDot + " result excluded from context", Tone: footerSuccess}
	}
	if strings.HasPrefix(composer, "!") {
		return footerPresentation{Text: "bash command " + glyphMiddleDot + " result will be added to context", Tone: footerSuccess}
	}
	return footerPresentation{}
}
