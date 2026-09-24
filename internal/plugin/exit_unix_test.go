//go:build darwin || linux

package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestObserveExitPreservesWaitableLeader(t *testing.T) {
	t.Setenv(helperMarker, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestPluginProcessHelper$", "--", "exit")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	for range 2 {
		// The second observation covers a child already exited before registration.
		done := make(chan error, 1)
		go func() { done <- observeExit(command.Process.Pid) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("exit observation did not complete")
		}
	}
	var exit *exec.ExitError
	if err := command.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("observation consumed wait status: %v", err)
	}
}

func TestStopReportsCleanupFailureSeparatelyFromExit(t *testing.T) {
	done := make(chan struct{})
	close(done)
	cleanupErr := syscall.EPERM
	process := &Process{stop: make(chan struct{}), done: done, cleanupErr: cleanupErr, err: errors.Join(errors.New("leader exit"), cleanupErr)}
	if err := process.Stop(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("stop hid cleanup failure: %v", err)
	}
	if err := process.Wait(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("wait hid cleanup failure: %v", err)
	}
}
