package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestTrackerCloseWaitsForRunningHandlers(t *testing.T) {
	tracker := newRequestTracker()
	entered, release := make(chan struct{}), make(chan struct{})
	handler := tracker.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	served := make(chan int, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		served <- recorder.Code
	}()
	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := tracker.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() with a running handler = %v, want deadline exceeded", err)
	}

	closed := make(chan error, 1)
	go func() { closed <- tracker.Close(t.Context()) }()
	close(release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close() after the handler returned = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not observe the handler returning")
	}
	if code := <-served; code != http.StatusNoContent {
		t.Fatalf("in-flight response = %d, want %d", code, http.StatusNoContent)
	}
}

func TestRequestTrackerRefusesRequestsAfterClose(t *testing.T) {
	tracker := newRequestTracker()
	if err := tracker.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	called := false
	handler := tracker.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if called || recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("request after Close: called=%t status=%d, want refused with %d", called, recorder.Code, http.StatusServiceUnavailable)
	}
}
