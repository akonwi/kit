//go:build darwin || linux

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const helperMarker = "KIT_PLUGIN_PROCESS_TEST_HELPER"

// TestPluginProcessHelper is re-executed only in explicitly marked subprocesses.
func TestPluginProcessHelper(t *testing.T) {
	if os.Getenv(helperMarker) != "1" {
		return
	}
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 || index >= len(os.Args) {
		os.Exit(98)
	}
	args := os.Args[index:]
	switch args[0] {
	case "rpc":
		decoder := json.NewDecoder(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)
		var pending json.RawMessage
		for {
			var request rpcMessage
			if decoder.Decode(&request) != nil {
				break
			}
			if request.Method == nil {
				_ = encoder.Encode(responseMessage(pending, request.Result, nil))
				continue
			}
			switch *request.Method {
			case "initialize":
				_ = encoder.Encode(responseMessage(request.ID, json.RawMessage(`{"protocolVersion":1}`), nil))
			case "roundtrip":
				pending = request.ID
				_ = encoder.Encode(requestMessage(json.RawMessage(`"child-1"`), "host/echo", request.Params))
			default:
				_ = encoder.Encode(responseMessage(request.ID, nil, rpcError(-32601, "Method not found")))
			}
		}
	case "describe":
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"cwd": cwd, "args": args[1:], "environment": os.Getenv("KIT_PLUGIN_INHERITED_TEST")})
		_, _ = io.Copy(io.Discard, os.Stdin)
	default:
		os.Exit(97)
	}
	os.Exit(0)
}

func startHelper(t *testing.T, ctx context.Context, mode string, args ...string) *Process {
	t.Helper()
	t.Setenv(helperMarker, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process, err := StartProcess(ctx, Installation{Root: t.TempDir(), Manifest: Manifest{ManifestVersion: 1, ID: "test-plugin", Transport: StdioTransport{Type: "stdio", Command: executable, Args: append([]string{"-test.run=^TestPluginProcessHelper$", "--", mode}, args...)}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := process.Stop(ctx); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return process
}

func TestProcessRelativeExecutableAndExactWorkingDirectory(t *testing.T) {
	t.Setenv(helperMarker, "1")
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(root, "plugin")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	process, err := StartProcess(ctx, Installation{Root: root, Manifest: Manifest{ManifestVersion: 1, ID: "relative", Transport: StdioTransport{Type: "stdio", Command: "./plugin", Args: []string{"-test.run=^TestPluginProcessHelper$", "--", "describe"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Stop(context.Background())
	var got struct {
		Cwd string `json:"cwd"`
	}
	if err := json.NewDecoder(process.Output()).Decode(&got); err != nil {
		t.Fatal(err)
	}
	// macOS commonly resolves /var to /private/var in getcwd.
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cwd != physical && got.Cwd != root {
		t.Fatalf("cwd = %q, want %q", got.Cwd, root)
	}
}

func TestProcessRejectsCancelledOrInvalidLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := StartProcess(ctx, Installation{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled launch = %v", err)
	}
	valid := Installation{Root: t.TempDir(), Manifest: Manifest{ManifestVersion: 1, ID: "missing", Transport: StdioTransport{Type: "stdio", Command: "./missing"}}}
	if _, err := StartProcess(t.Context(), valid); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing executable = %v", err)
	}
	valid.Root = "relative"
	if _, err := StartProcess(t.Context(), valid); err == nil {
		t.Fatal("accepted relative installation")
	}
}

func TestProcessRPCFullDuplex(t *testing.T) {
	process := startHelper(t, t.Context(), "rpc")
	endpoint := NewRPCEndpoint(t.Context(), process.Output(), process.Input(), RPCHandlers{Request: func(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if method != "host/echo" {
			return nil, rpcError(-32601, "Method not found")
		}
		return params, nil
	}})
	t.Cleanup(func() {
		endpoint.Close(nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = endpoint.Wait(ctx)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	initialized, err := endpoint.Call(ctx, "initialize", json.RawMessage(`{"protocolVersion":1}`))
	if err != nil || string(initialized) != `{"protocolVersion":1}` {
		t.Fatalf("initialize = %s, %v", initialized, err)
	}
	params := json.RawMessage(`{"value":"real process roundtrip"}`)
	result, err := endpoint.Call(ctx, "roundtrip", params)
	if err != nil || string(result) != string(params) {
		t.Fatalf("roundtrip = %s, %v", result, err)
	}
	endpoint.Close(nil)
	if err := process.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
