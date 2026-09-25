package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/gofrs/flock"
)

func TestSubprocessRestartReplacesOlderDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in short mode")
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	binaryDirectory := t.TempDir()
	oldBinary := filepath.Join(binaryDirectory, "kit-old")
	newBinary := filepath.Join(binaryDirectory, "kit-new")
	buildKitBinary(t, root, oldBinary, "bootstrap-old")
	buildKitBinary(t, root, newBinary, "bootstrap-new")

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "home"))
	runKit := func(binary string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = root
		command.Env = append(os.Environ(), apphome.EnvHome+"="+paths.Home)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", binary, strings.Join(args, " "), err, output)
		}
		return string(output)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, binary := range []string{newBinary, oldBinary} {
			command := exec.CommandContext(ctx, binary, "server", "stop")
			command.Env = append(os.Environ(), apphome.EnvHome+"="+paths.Home)
			_ = command.Run()
		}
	})

	runKit(oldBinary, "server", "start")
	oldRegistry, err := LoadRegistry(paths)
	if err != nil {
		t.Fatalf("load old registry: %v", err)
	}
	if oldRegistry.KitVersion != "bootstrap-old" {
		t.Fatalf("old version = %q", oldRegistry.KitVersion)
	}

	runKit(newBinary, "server", "restart")
	newRegistry, err := LoadRegistry(paths)
	if err != nil {
		t.Fatalf("load new registry: %v", err)
	}
	if newRegistry.KitVersion != "bootstrap-new" {
		t.Fatalf("new version = %q", newRegistry.KitVersion)
	}
	if newRegistry.PID == oldRegistry.PID {
		t.Fatalf("restart retained pid %d", newRegistry.PID)
	}

	status := runKit(newBinary, "server", "status")
	if !strings.Contains(status, "version bootstrap-new") {
		t.Fatalf("status = %q", status)
	}
	runKit(newBinary, "server", "stop")

	lifetimeLock := flock.New(paths.ServerLock)
	locked, err := lifetimeLock.TryLock()
	if err != nil {
		t.Fatalf("inspect lifetime lock: %v", err)
	}
	if !locked {
		t.Fatal("daemon stop returned before lifetime lock was released")
	}
	if err := lifetimeLock.Unlock(); err != nil {
		t.Fatalf("release test lifetime lock: %v", err)
	}
}

func buildKitBinary(t *testing.T, root, output, buildVersion string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ldflag := fmt.Sprintf("-X github.com/akonwi/kit/internal/version.Version=%s", buildVersion)
	command := exec.CommandContext(ctx, "go", "build", "-ldflags", ldflag, "-o", output, "./cmd/kit")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=auto")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build Kit test binary: %v\n%s", err, result)
	}
}
