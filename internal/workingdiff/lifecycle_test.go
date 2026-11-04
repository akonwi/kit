package workingdiff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/workspace"
)

// newTestService returns a service that is closed when the test ends.
func newTestService(t testing.TB, ws *workspace.Service) (*Service, error) {
	t.Helper()
	s, err := NewService(ws)
	if err == nil {
		t.Cleanup(func() {
			if err := s.Close(context.Background()); err != nil {
				t.Errorf("close diff service: %v", err)
			}
		})
	}
	return s, err
}

func TestCloseRemovesTheGitHomeAndIsIdempotent(t *testing.T) {
	d := fixture(t)
	ws := workspace.NewService()
	s, err := NewService(ws)
	if err != nil {
		t.Fatal(err)
	}
	home := s.runner.home
	if info, err := os.Stat(home); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("git home before Close = %v, %v; want a 0700 directory", info, err)
	}

	var closers sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		closers.Go(func() { errs[i] = s.Close(t.Context()) })
	}
	closers.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Close %d = %v, want nil", i, err)
		}
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("git home after Close: stat error = %v, want not exist", err)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}

	ref := ws.Ref("session_closed", d)
	_, err = s.Observe(t.Context(), "session_closed", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	var diffErr *Error
	if !errors.As(err, &diffErr) || diffErr.Code != Unavailable {
		t.Fatalf("Observe after Close = %v, want %s", err, Unavailable)
	}
}

func TestCloseStopsRunningGitCommandsBeforeRemovingTheGitHome(t *testing.T) {
	s, err := NewService(workspace.NewService())
	if err != nil {
		t.Fatal(err)
	}
	home := s.runner.home
	// The fake git marks its start in its working directory, the git home,
	// and then blocks until it is killed.
	fake := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch started\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s.runner.exe = fake

	ran := make(chan error, 1)
	go func() {
		_, err := s.runner.run(context.Background(), nil, nil, 1<<10, "status")
		ran <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(home, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake git did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Close(closeContext); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	select {
	case err := <-ran:
		if err == nil {
			t.Fatal("running git command succeeded after Close, want it stopped")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("running git command did not stop after Close")
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("git home after Close: stat error = %v, want not exist", err)
	}
	if _, err := s.runner.run(context.Background(), nil, nil, 1<<10, "status"); !errors.Is(err, errRunnerClosed) {
		t.Fatalf("run after Close = %v, want %v", err, errRunnerClosed)
	}
}
