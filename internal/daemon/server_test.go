package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

func TestRunServesHealthAndStopsGracefully(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{
			Paths:  paths,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()

	client := NewClient(paths)
	probeContext, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	var registry Registry
	for {
		var err error
		registry, _, err = client.Probe(probeContext)
		if err == nil {
			break
		}
		select {
		case <-probeContext.Done():
			t.Fatalf("daemon did not become ready: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if registry.PID != os.Getpid() {
		t.Errorf("PID = %d, want in-process test PID %d", registry.PID, os.Getpid())
	}
	info, err := os.Stat(paths.ServerToken)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token mode = %o, want 600", info.Mode().Perm())
	}

	response, err := http.Get(registry.URL + "/v1/health")
	if err != nil {
		t.Fatalf("unauthenticated health request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}

	stopContext, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := NewManager(paths).Stop(stopContext); err != nil {
		t.Fatalf("Manager.Stop() error = %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-stopContext.Done():
		t.Fatal("daemon did not stop")
	}
	if _, err := os.Stat(paths.ServerRegistry); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("registry still exists after shutdown: %v", err)
	}
	if _, err := os.Stat(paths.ServerToken); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("token still exists after shutdown: %v", err)
	}
}
