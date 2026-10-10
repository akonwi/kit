package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/pullrequest"
)

// PullRequestStatusUpdate ties canonical remote status to the workspace identity
// that produced it. Status is nil when that identity no longer has an observation.
type PullRequestStatusUpdate struct {
	CWD          string
	Root         string
	RepositoryID string
	Branch       string
	Status       *pullrequest.Status
}

// PullRequestStatusSubscription is a bounded stream of distinct observations.
type PullRequestStatusSubscription interface {
	Next(context.Context) (PullRequestStatusUpdate, error)
	Close()
}

// PullRequestStatusHost is the optional rich GitHub observation port of a loaded
// session host. The canonical model is renderer-neutral and may gain additional
// consumers independently of Droid delivery.
type PullRequestStatusHost interface {
	CurrentPullRequestStatus() PullRequestStatusUpdate
	SubscribePullRequestStatus() (PullRequestStatusSubscription, error)
}

const maxPullRequestBoundaryDetailsBytes = 60 << 10

type pullRequestBoundaryDetails struct {
	Version int                 `json:"version"`
	Branch  string              `json:"branch"`
	Status  *pullrequest.Status `json:"status"`
}

func startPullRequestInformer(loaded *runtime, host PullRequestStatusHost) error {
	subscription, err := host.SubscribePullRequestStatus()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	loaded.pullRequestInformerCancel = cancel
	loaded.pullRequestInformerSource = subscription
	loaded.pullRequestInformerDone = make(chan struct{})
	go runPullRequestInformer(ctx, loaded, host, subscription)
	return nil
}

func runPullRequestInformer(ctx context.Context, loaded *runtime, host PullRequestStatusHost, subscription PullRequestStatusSubscription) {
	defer close(loaded.pullRequestInformerDone)
	var previous PullRequestStatusUpdate
	for {
		update, err := subscription.Next(ctx)
		if err != nil {
			return
		}
		if update.Status == nil {
			previous = update
			continue
		}
		if samePullRequestStatus(previous, update) {
			continue
		}
		previous = clonePullRequestStatusUpdate(update)
		if !samePullRequestIdentity(update, host.CurrentPullRequestStatus()) {
			continue
		}
		id, err := identifier.New("github_")
		if err != nil {
			continue
		}
		details, err := encodePullRequestBoundaryDetails(update)
		if err != nil {
			return
		}
		boundary := droids.BoundaryMessage{
			ID: id, Kind: "pull_request_status", Source: "github",
			Content: []droids.InputContent{droids.TextInput{Text: pullRequestStatusSummary(update.Status)}},
			Details: details,
		}
		if !informPullRequestBoundary(ctx, loaded, boundary, update, host) {
			return
		}
	}
}

func encodePullRequestBoundaryDetails(update PullRequestStatusUpdate) (json.RawMessage, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(pullRequestBoundaryDetails{Version: 1, Branch: update.Branch, Status: update.Status}); err != nil {
		return nil, err
	}
	details := bytes.TrimSuffix(output.Bytes(), []byte("\n"))
	if len(details) > maxPullRequestBoundaryDetailsBytes {
		return nil, fmt.Errorf("pull request boundary details exceed %d bytes", maxPullRequestBoundaryDetailsBytes)
	}
	return append(json.RawMessage(nil), details...), nil
}

func informPullRequestBoundary(ctx context.Context, loaded *runtime, boundary droids.BoundaryMessage, update PullRequestStatusUpdate, host PullRequestStatusHost) bool {
	for {
		if !samePullRequestIdentity(update, host.CurrentPullRequestStatus()) {
			return true
		}
		loaded.transitionMu.RLock()
		loaded.mu.Lock()
		if !samePullRequestIdentity(update, host.CurrentPullRequestStatus()) {
			loaded.mu.Unlock()
			loaded.transitionMu.RUnlock()
			return true
		}
		droid := loaded.droid
		err := droid.Inform(ctx, boundary)
		if err != nil {
			received, reconcileErr := droid.BoundaryReceived(context.Background(), boundary.ID)
			if reconcileErr == nil && received {
				err = nil
			}
		}
		loaded.mu.Unlock()
		loaded.transitionMu.RUnlock()
		if err == nil {
			return true
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, droids.ErrClosed) || ctx.Err() != nil {
			return false
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func clonePullRequestStatusUpdate(value PullRequestStatusUpdate) PullRequestStatusUpdate {
	value.Status = pullrequest.Clone(value.Status)
	return value
}

func samePullRequestStatus(left, right PullRequestStatusUpdate) bool {
	return left.CWD == right.CWD && left.Root == right.Root && left.RepositoryID == right.RepositoryID && left.Branch == right.Branch && reflect.DeepEqual(left.Status, right.Status)
}

func samePullRequestIdentity(left, right PullRequestStatusUpdate) bool {
	if left.Status == nil || right.Status == nil {
		return left.Status == nil && right.Status == nil && left.CWD == right.CWD && left.Root == right.Root && left.RepositoryID == right.RepositoryID && left.Branch == right.Branch
	}
	return left.CWD == right.CWD && left.Root == right.Root && left.RepositoryID == right.RepositoryID && left.Branch == right.Branch && left.Status.Number == right.Status.Number && left.Status.URL == right.Status.URL && left.Status.HeadOID == right.Status.HeadOID
}

func pullRequestStatusSummary(status *pullrequest.Status) string {
	counts := map[pullrequest.CheckState]int{}
	for _, check := range status.Checks {
		counts[check.State]++
	}
	return fmt.Sprintf(
		"Pull request #%d status changed: checks %d passed, %d pending, %d failed, %d skipped, %d canceled, %d unknown; review decision %s; required checks %s; required approvals %s; merge readiness %s.",
		status.Number, counts[pullrequest.CheckPassed], counts[pullrequest.CheckPending], counts[pullrequest.CheckFailed], counts[pullrequest.CheckSkipped], counts[pullrequest.CheckCanceled], counts[pullrequest.CheckUnknown],
		status.ReviewDecision, status.RequiredChecks, status.RequiredApprovals, status.Readiness,
	)
}
