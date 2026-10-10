package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/pullrequest"
)

type testPullRequestStatusHost struct {
	mu      sync.Mutex
	current PullRequestStatusUpdate
	values  chan PullRequestStatusUpdate
}

func newTestPullRequestStatusHost() *testPullRequestStatusHost {
	return &testPullRequestStatusHost{values: make(chan PullRequestStatusUpdate, 16)}
}
func (h *testPullRequestStatusHost) publish(value PullRequestStatusUpdate) {
	h.mu.Lock()
	h.current = clonePullRequestStatusUpdate(value)
	h.mu.Unlock()
	h.values <- clonePullRequestStatusUpdate(value)
}
func (h *testPullRequestStatusHost) CurrentPullRequestStatus() PullRequestStatusUpdate {
	h.mu.Lock()
	defer h.mu.Unlock()
	return clonePullRequestStatusUpdate(h.current)
}
func (h *testPullRequestStatusHost) SubscribePullRequestStatus() (PullRequestStatusSubscription, error) {
	return testPullRequestStatusSubscription{h.values}, nil
}

type testPullRequestStatusSubscription struct{ values chan PullRequestStatusUpdate }

func (s testPullRequestStatusSubscription) Close() {}
func (s testPullRequestStatusSubscription) Next(ctx context.Context) (PullRequestStatusUpdate, error) {
	select {
	case <-ctx.Done():
		return PullRequestStatusUpdate{}, ctx.Err()
	case value := <-s.values:
		return value, nil
	}
}

func testStatusDroid(t *testing.T) *droids.Droid {
	t.Helper()
	base := droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
	model, err := droids.BindModel(droids.AdaptProvider("test", []droids.Model{base}, func(context.Context, droids.Model, droids.Request) droids.Stream { return nil }), base)
	if err != nil {
		t.Fatal(err)
	}
	droid, err := droids.Spawn(t.Context(), droids.ConversationID("conversation_github_status"), droids.Config{Model: model})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	return droid
}

func TestPullRequestBoundaryDetailsStayWithinDroidLimit(t *testing.T) {
	text := strings.Repeat("<", 256)
	status := &pullrequest.Status{Number: 42, URL: "https://github.com/" + strings.Repeat("a", 4000), HeadRefName: text}
	for range 48 {
		status.Checks = append(status.Checks, pullrequest.Check{Name: text, Workflow: text, State: pullrequest.CheckPending, Required: true})
		status.Reviews = append(status.Reviews, pullrequest.Review{Author: text, State: "CHANGES_REQUESTED"})
	}
	details, err := encodePullRequestBoundaryDetails(PullRequestStatusUpdate{Branch: text, Status: status})
	if err != nil || len(details) > maxPullRequestBoundaryDetailsBytes || strings.Contains(string(details), `\\u003c`) {
		t.Fatalf("details bytes=%d err=%v", len(details), err)
	}
}

func TestPullRequestInformerDeduplicatesConsecutiveStatusAndPreservesTransitions(t *testing.T) {
	host := newTestPullRequestStatusHost()
	loaded := &runtime{droid: testStatusDroid(t)}
	if err := startPullRequestInformer(loaded, host); err != nil {
		t.Fatal(err)
	}
	defer func() {
		loaded.pullRequestInformerCancel()
		loaded.pullRequestInformerSource.Close()
		<-loaded.pullRequestInformerDone
	}()
	status := func(state pullrequest.CheckState) PullRequestStatusUpdate {
		return PullRequestStatusUpdate{
			CWD: "/repo", Root: "/repo", Branch: "main",
			Status: &pullrequest.Status{Number: 42, URL: "https://github.com/a/b/pull/42", HeadRefName: "main", HeadOID: "0123456789abcdef0123456789abcdef01234567", Checks: []pullrequest.Check{{Name: "test", State: state}}},
		}
	}
	host.publish(status(pullrequest.CheckPending))
	host.publish(status(pullrequest.CheckPending))
	host.publish(status(pullrequest.CheckFailed))
	host.publish(status(pullrequest.CheckPending))

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := loaded.droid.Snapshot(t.Context(), droids.SnapshotOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Pending.Boundaries) == 3 {
			for _, boundary := range snapshot.Pending.Boundaries {
				if boundary.Message.Kind != "pull_request_status" || boundary.Message.Source != "github" {
					t.Fatalf("boundary=%+v", boundary.Message)
				}
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pull request transitions were not informed")
}

func TestPullRequestInformerFencesStaleHeadRevision(t *testing.T) {
	host := newTestPullRequestStatusHost()
	loaded := &runtime{droid: testStatusDroid(t)}
	if err := startPullRequestInformer(loaded, host); err != nil {
		t.Fatal(err)
	}
	defer func() {
		loaded.pullRequestInformerCancel()
		loaded.pullRequestInformerSource.Close()
		<-loaded.pullRequestInformerDone
	}()
	stale := PullRequestStatusUpdate{CWD: "/repo", Root: "/repo", Branch: "main", Status: &pullrequest.Status{Number: 42, HeadOID: "0123456789abcdef0123456789abcdef01234567"}}
	current := clonePullRequestStatusUpdate(stale)
	current.Status.HeadOID = "abcdef0123456789abcdef0123456789abcdef01"
	host.mu.Lock()
	host.current = current
	host.mu.Unlock()
	host.values <- stale
	time.Sleep(20 * time.Millisecond)
	snapshot, err := loaded.droid.Snapshot(t.Context(), droids.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending.Boundary != 0 {
		t.Fatalf("stale boundary was informed: %+v", snapshot.Pending.Boundaries)
	}
}
