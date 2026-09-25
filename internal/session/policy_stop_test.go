package session_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestManagerPolicyStopHoldsQueueWithoutLockingSessions(t *testing.T) {
	for _, midstream := range []bool{false, true} {
		t.Run(fmt.Sprintf("midstream=%v", midstream), func(t *testing.T) {
			var calls atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				number := calls.Add(1)
				w.Header().Set("x-request-id", "req_policy")
				if number == 1 {
					if midstream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_policy\",\"status\":\"in_progress\"}}\n\n")
						fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"partial output\"}\n\n")
						w.(http.Flusher).Flush()
					}
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					if midstream {
						fmt.Fprint(w, "data: {\"type\":\"error\",\"code\":\"misalignment_policy_violation\",\"message\":\"provider private message\"}\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"error":{"code":"misalignment_policy_violation","type":"invalid_request_error","message":"provider private message"}}`)
					}
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"status\":\"completed\",\"output\":[]}}\n\n")
			}))
			defer server.Close()
			defer unblock()
			root := t.TempDir()
			store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			providers, err := droids.NewProviders(droids.OpenAI{APIKey: "test", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			defer unblock()
			create := func(name string) string {
				record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "openai/gpt-4o-mini", Name: name})
				if err != nil {
					t.Fatal(err)
				}
				return record.ID
			}
			first, other := create("policy"), create("unaffected")
			type settled struct {
				result session.PromptResult
				err    error
			}
			done := make(chan settled, 1)
			go func() { result, err := manager.RunPrompt(t.Context(), first, "first"); done <- settled{result, err} }()
			<-entered
			queued, err := manager.SubmitPrompt(t.Context(), first, "queued follow-up")
			if err != nil || !queued.Queued {
				t.Fatalf("queue=%+v err=%v", queued, err)
			}
			unaffected, err := manager.RunPrompt(t.Context(), other, "other session")
			if err != nil || unaffected.Status != session.RunStatusCompleted {
				t.Fatalf("unaffected=%+v %v", unaffected, err)
			}
			unblock()
			stopped := <-done
			if stopped.err != nil || stopped.result.Status != session.RunStatusFailed || stopped.result.ErrorMessage != "Provider stopped this request (misalignment_policy_violation)" {
				t.Fatalf("stopped=%+v", stopped)
			}
			snapshot, err := manager.Snapshot(t.Context(), first)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.ActiveRunID != "" || snapshot.FollowUps.Count != 1 || calls.Load() != 2 {
				t.Fatalf("active=%s queue=%+v calls=%d", snapshot.ActiveRunID, snapshot.FollowUps, calls.Load())
			}
			// Reuse the normal pending-input restore flow, not a policy-specific unlock.
			restored, err := manager.RestoreFollowUps(t.Context(), first)
			if err != nil || len(restored.Messages) != 1 {
				t.Fatalf("restore=%+v %v", restored, err)
			}
			manual, err := manager.RunPrompt(t.Context(), first, "manual retry")
			if err != nil || manual.Status != session.RunStatusCompleted || calls.Load() != 3 {
				t.Fatalf("manual=%+v %v calls=%d", manual, err, calls.Load())
			}
		})
	}
}
