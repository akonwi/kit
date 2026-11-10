//go:build unix

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

// TestACPStdoutCarriesOnlyProtocolRecordsFromAnEmptyHome runs `kit acp` as an
// editor would, against a Kit home that does not exist yet, so the first
// request creates the home and starts its server. Every line on stdout must be
// a JSON-RPC record answering the client; whatever else the process or the
// server it starts writes belongs on stderr.
func TestACPStdoutCarriesOnlyProtocolRecordsFromAnEmptyHome(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in short mode")
	}
	binary := filepath.Join(t.TempDir(), "kit")
	build := exec.Command("go", "build", "-o", binary, "./testdata/kit")
	build.Env = append(os.Environ(), "GOTOOLCHAIN=auto")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Kit test binary: %v\n%s", err, output)
	}
	home := filepath.Join(t.TempDir(), "home")
	environment := append(withoutKitHome(os.Environ()), apphome.EnvHome+"="+home)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stop := exec.CommandContext(ctx, binary, "server", "stop")
		stop.Env = environment
		_ = stop.Run()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "acp")
	command.Env = environment
	command.Dir = t.TempDir()
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	var stdout []string
	// await reads stdout until the response to id arrives.
	await := func(id int) map[string]any {
		t.Helper()
		for {
			select {
			case line, open := <-lines:
				if !open {
					t.Fatalf("stdout closed before response %d\nstdout:\n%s\nstderr:\n%s", id, strings.Join(stdout, "\n"), stderr.String())
				}
				stdout = append(stdout, line)
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("stdout line is not JSON: %q\nstderr:\n%s", line, stderr.String())
				}
				if record["jsonrpc"] != "2.0" {
					t.Fatalf("stdout line is not a JSON-RPC 2.0 record: %q", line)
				}
				if value, ok := record["id"].(float64); ok && int(value) == id && record["method"] == nil {
					return record
				}
			case <-ctx.Done():
				t.Fatalf("no response %d: %v\nstdout:\n%s\nstderr:\n%s", id, ctx.Err(), strings.Join(stdout, "\n"), stderr.String())
			}
		}
	}
	send := func(id int, method string, params any) {
		t.Helper()
		record, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(stdin, "%s\n", record); err != nil {
			t.Fatalf("write %s: %v", method, err)
		}
	}

	send(1, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	if result, _ := await(1)["result"].(map[string]any); result["protocolVersion"] != float64(1) {
		t.Fatalf("initialize result = %v", result)
	}
	// A new home has no provider logged in, which the client hears as the
	// error that asks for authentication.
	send(2, "session/new", map[string]any{"cwd": command.Dir, "mcpServers": []any{}})
	if failure, _ := await(2)["error"].(map[string]any); failure["code"] != float64(-32000) {
		t.Fatalf("session/new error = %v, want code -32000", failure)
	}
	send(3, "session/list", map[string]any{})
	if result, _ := await(3)["result"].(map[string]any); result == nil || len(result["sessions"].([]any)) != 0 {
		t.Fatalf("session/list result = %v, want no sessions", result)
	}

	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	for line := range lines {
		stdout = append(stdout, line)
		if !json.Valid([]byte(line)) {
			t.Fatalf("stdout line is not JSON: %q", line)
		}
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("kit acp: %v\nstderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(apphome.FromHome(home).ServerRegistry); err != nil {
		t.Fatalf("kit acp did not start the home's server: %v", err)
	}
}

func withoutKitHome(environment []string) []string {
	kept := environment[:0:0]
	for _, entry := range environment {
		if !strings.HasPrefix(entry, apphome.EnvHome+"=") {
			kept = append(kept, entry)
		}
	}
	return kept
}
