package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

func TestPluginInstanceHelper(t *testing.T) {
	if os.Getenv("KIT_INSTANCE_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	if mode == "early" {
		_ = encoder.Encode(requestMessage(json.RawMessage(`"early"`), "host/register", nil))
	}
	if mode == "early-notify" {
		_ = encoder.Encode(requestMessage(nil, "host/event", nil))
	}
	for {
		var message rpcMessage
		if decoder.Decode(&message) != nil {
			os.Exit(0)
		}
		if message.Method == nil {
			if mode == "footer" && string(message.ID) == `"footer-set"` {
				_ = os.WriteFile("footer-result.json", message.Result, 0600)
			}
			if mode == "subagents" && string(message.ID) == `"subagent-register"` {
				_ = os.WriteFile("subagent-result.json", message.Result, 0600)
			}
			continue
		}
		switch *message.Method {
		case "initialize":
			_ = os.WriteFile("initial.json", message.Params, 0600)
			if mode == "stall-init" {
				signal.Ignore(syscall.SIGTERM)
				for {
					time.Sleep(time.Hour)
				}
			}
			if mode == "hold-init" {
				for {
					if _, err := os.Stat("release"); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			if mode == "invalid-batch" {
				_ = encoder.Encode([]any{17, responseMessage(message.ID, json.RawMessage(`{"protocolVersion":1}`), nil)})
				continue
			}
			if mode == "bad-version" {
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`{"protocolVersion":2}`), nil))
				continue
			}
			if mode == "extra-version-field" {
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`{"protocolVersion":1,"extra":true}`), nil))
				continue
			}
			if mode == "reject-init" {
				_ = encoder.Encode(responseMessage(message.ID, nil, rpcError(-32000, "unsupported protocol")))
				continue
			}
			if mode == "batch-ready" {
				var initial struct {
					Context json.RawMessage `json:"context"`
				}
				_ = json.Unmarshal(message.Params, &initial)
				_ = encoder.Encode([]rpcMessage{responseMessage(message.ID, json.RawMessage(`{"protocolVersion":1}`), nil), requestMessage(json.RawMessage(`"immediate"`), "host/register", initial.Context)})
			} else {
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`{"protocolVersion":1}`), nil))
			}
			if mode == "footer" {
				_ = encoder.Encode(requestMessage(json.RawMessage(`"footer-set"`), "kit/footer/set", json.RawMessage(`{"id":"status","content":[{"text":"Plugin ready","style":{"fg":"toolText","bold":true}}]}`)))
				_ = encoder.Encode(requestMessage(json.RawMessage(`"footer-hide"`), "kit/footer/hide", json.RawMessage(`{"id":"kit.footer.location"}`)))
			}
			if mode == "subagents" {
				_ = encoder.Encode(requestMessage(json.RawMessage(`"subagent-register"`), "kit/subagents/register", json.RawMessage(`{"id":"reviewer","description":"Review changes","instructions":"Inspect changes and report findings.","model":null}`)))
			}
			if mode == "commands" {
				for _, id := range []string{"ok", "fail", "invalid", "wait"} {
					params, _ := json.Marshal(map[string]string{"id": id, "description": "Run " + id, "argName": "value"})
					requestID, _ := json.Marshal("register-" + id)
					_ = encoder.Encode(requestMessage(requestID, "kit/commands/register", params))
				}
			}
		case "kit/tool-calls/before-execute":
			_ = os.WriteFile("interceptor.json", message.Params, 0600)
			var params struct {
				ToolCall struct {
					Name string `json:"name"`
				} `json:"toolCall"`
			}
			_ = json.Unmarshal(message.Params, &params)
			switch params.ToolCall.Name {
			case "wait":
			case "reject":
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`{"action":"reject-and-continue","message":"Policy rejected this call"}`), nil))
			case "invalid":
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`{"action":"approve"}`), nil))
			default:
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`{"action":"allow"}`), nil))
			}
		case "kit/tools/execute":
			_ = os.WriteFile("tool.json", message.Params, 0600)
			var params struct {
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
			}
			_ = json.Unmarshal(message.Params, &params)
			switch params.ID {
			case "wait":
			case "invalid":
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`{"content":[{"type":"image","data":"bad","mimeType":"image/png"}]}`), nil))
			default:
				result, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": string(params.Input)}}, "details": params.Input, "terminate": true})
				_ = encoder.Encode(responseMessage(message.ID, result, nil))
			}
		case "kit/commands/execute":
			var params struct {
				ID   string `json:"id"`
				Args string `json:"args"`
			}
			_ = json.Unmarshal(message.Params, &params)
			_ = os.WriteFile("command.json", message.Params, 0600)
			switch params.ID {
			case "fail":
				_ = encoder.Encode(responseMessage(message.ID, nil, rpcError(-32000, "command failed")))
			case "invalid":
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`true`), nil))
			case "wait":
			default:
				_ = encoder.Encode(responseMessage(message.ID, nil, nil))
			}
		case "shutdown":
			if mode == "ignore-shutdown" {
				signal.Ignore(syscall.SIGTERM)
				for {
					time.Sleep(time.Hour)
				}
			}
			if mode == "bad-shutdown" {
				_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`true`), nil))
				continue
			}
			_ = encoder.Encode(responseMessage(message.ID, json.RawMessage(`null`), nil))
			os.Exit(0)
		case "kit/events/git.changed", "kit/events/project.changed":
			contextFile, contextErr := os.OpenFile("context-events.jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if contextErr == nil {
				_ = json.NewEncoder(contextFile).Encode(message)
				_ = contextFile.Close()
			}
			if *message.Method == "kit/events/project.changed" {
				_ = os.WriteFile("project.json", message.Params, 0600)
				continue
			}
			file, err := os.OpenFile("git-events.jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err == nil {
				_ = json.NewEncoder(file).Encode(message)
				_ = file.Close()
			}
		case "kit/events/session.changed":
			_ = os.WriteFile("session.json", message.Params, 0600)
		case turnStartedMethod, turnCompletedMethod:
			file, err := os.OpenFile("turn-events.jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err == nil {
				_ = json.NewEncoder(file).Encode(message)
				_ = file.Close()
			}
		case "event":
			_ = os.WriteFile("event.json", message.Params, 0600)
		case "read-event":
			data, _ := os.ReadFile("event.json")
			_ = encoder.Encode(responseMessage(message.ID, data, nil))
		case "exit":
			_, _ = fmt.Fprintln(os.Stderr, "unexpected exit diagnostic")
			os.Exit(0)
		case "wait":
			_ = encoder.Encode(requestMessage(json.RawMessage(`"nested"`), "host/wait", nil))
			// Keep reading so cancellation and shutdown can proceed concurrently.
		case "kit/cancel":
		default:
			_ = encoder.Encode(responseMessage(message.ID, message.Params, nil))
		}
	}
}

func testInstance(t *testing.T, parent context.Context, mode string, handlers RPCHandlers, deadlines instanceDeadlines) (*Instance, string) {
	t.Helper()
	t.Setenv("KIT_INSTANCE_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	installation := Installation{Root: root, Manifest: Manifest{ManifestVersion: 1, ID: "example", Transport: StdioTransport{Type: "stdio", Command: executable, Args: []string{"-test.run=^TestPluginInstanceHelper$", "--", mode}}}}
	instance, err := startInstance(parent, InstanceID{HostID: "pluginhost_0123456789abcdef0123456789abcdef", SessionID: "session-one", PluginID: "example", Generation: 1}, installation, PluginContext{Project: ProjectContext{Cwd: root}, Session: SessionContext{ID: "session-one"}}, handlers, deadlines)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = instance.Stop(ctx)
		select {
		case <-instance.Done():
		case <-ctx.Done():
			t.Error("instance cleanup did not finish")
		}
	})
	return instance, root
}

func testInstanceDeadlines() instanceDeadlines {
	return instanceDeadlines{2 * time.Second, 100 * time.Millisecond, time.Second}
}

func waitInstance(t *testing.T, instance *Instance) {
	t.Helper()
	select {
	case <-instance.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("instance did not stop")
	}
}

func TestInstanceInitializationPublishesReadyBeforePipelinedWork(t *testing.T) {
	registered := make(chan json.RawMessage, 1)
	instance, root := testInstance(t, t.Context(), "batch-ready", RPCHandlers{Request: func(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if method != "host/register" {
			return nil, rpcError(-32601, "unknown")
		}
		registered <- params
		return nil, nil
	}}, testInstanceDeadlines())
	if err := instance.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case params := <-registered:
		var got PluginContext
		if err := json.Unmarshal(params, &got); err != nil {
			t.Fatal(err)
		}
		if got.Project.Cwd != root || got.Project.Git != nil || got.Session.ID != "session-one" || got.Session.Name != nil {
			t.Fatalf("initial context = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("pipelined registration was rejected")
	}
	status := instance.Status()
	if status.State != InstanceReady || status.ID.Generation != 1 || status.ID.SessionID != "session-one" {
		t.Fatalf("status = %#v", status)
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status := instance.Status(); status.State != InstanceStopped || status.Failure != nil {
		t.Fatalf("normal shutdown = %#v", status)
	}
}

func TestInstanceRejectsInvalidInitializationAndEarlyWork(t *testing.T) {
	for _, mode := range []string{"bad-version", "extra-version-field", "reject-init", "early", "early-notify", "invalid-batch", "stall-init"} {
		t.Run(mode, func(t *testing.T) {
			var called atomic.Int32
			deadlines := testInstanceDeadlines()
			if mode == "stall-init" {
				deadlines.initialize = 100 * time.Millisecond
			}
			instance, _ := testInstance(t, t.Context(), mode, RPCHandlers{Request: func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
				called.Add(1)
				return nil, nil
			}, Notification: func(context.Context, string, json.RawMessage) error { called.Add(1); return nil }}, deadlines)
			if err := instance.WaitReady(t.Context()); err == nil {
				t.Fatal("invalid initialization became ready")
			}
			waitInstance(t, instance)
			if called.Load() != 0 {
				t.Fatal("work executed before initialization")
			}
			status := instance.Status()
			if status.State != InstanceFailed || status.Failure == nil || status.Failure.Phase != "initialize" {
				t.Fatalf("failure = %#v", status)
			}
		})
	}
}

func TestInstanceWaiterDeadlineDoesNotStopInitialization(t *testing.T) {
	instance, root := testInstance(t, t.Context(), "hold-init", RPCHandlers{}, testInstanceDeadlines())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := instance.WaitReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait = %v", err)
	}
	if status := instance.Status(); status.State != InstanceInitializing {
		t.Fatalf("waiter stopped instance: %#v", status)
	}
	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := instance.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestInstanceStopCancelsBothDirectionsAndRejectsOldGeneration(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	instance, _ := testInstance(t, t.Context(), "normal", RPCHandlers{Request: func(ctx context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
		if method != "host/wait" {
			return nil, rpcError(-32601, "unknown")
		}
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}, testInstanceDeadlines())
	if err := instance.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	call := make(chan error, 1)
	go func() { _, err := instance.Call(t.Context(), "wait", nil); call <- err }()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_ = instance.Stop(ctx)
	select {
	case <-instance.Revoked():
	default:
		t.Fatal("revocation was not synchronous")
	}
	if _, err := instance.Call(t.Context(), "echo", nil); err == nil {
		t.Fatal("old instance accepted new work")
	}
	if err := <-call; !errors.Is(err, context.Canceled) {
		t.Fatalf("outgoing cancellation = %v", err)
	}
	<-cancelled
	waitInstance(t, instance)
}

func TestInstanceCrashAndShutdownEscalation(t *testing.T) {
	t.Run("crash", func(t *testing.T) {
		instance, _ := testInstance(t, t.Context(), "normal", RPCHandlers{}, testInstanceDeadlines())
		if err := instance.WaitReady(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := instance.Call(t.Context(), "exit", nil); err == nil {
			t.Fatal("crash succeeded")
		}
		waitInstance(t, instance)
		status := instance.Status()
		if status.State != InstanceFailed || status.Failure == nil || status.Failure.Stderr != "unexpected exit diagnostic\n" {
			t.Fatalf("crash = %#v", status)
		}
	})
	t.Run("shutdown timeout", func(t *testing.T) {
		instance, _ := testInstance(t, t.Context(), "ignore-shutdown", RPCHandlers{}, testInstanceDeadlines())
		if err := instance.WaitReady(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := instance.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if status := instance.Status(); status.State != InstanceStopped {
			t.Fatalf("forced shutdown = %#v", status)
		}
	})
	t.Run("invalid shutdown result", func(t *testing.T) {
		instance, _ := testInstance(t, t.Context(), "bad-shutdown", RPCHandlers{}, testInstanceDeadlines())
		if err := instance.WaitReady(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := instance.Stop(t.Context()); err == nil {
			t.Fatal("invalid shutdown result accepted")
		}
		if status := instance.Status(); status.Failure == nil || status.Failure.Phase != "shutdown" {
			t.Fatalf("shutdown = %#v", status)
		}
	})
}

func TestInstanceOwnerCancellationStopsDetachedInstance(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	instance, _ := testInstance(t, parent, "normal", RPCHandlers{}, testInstanceDeadlines())
	if err := instance.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitInstance(t, instance)
	if status := instance.Status(); status.State != InstanceStopped || status.Failure != nil {
		t.Fatalf("owner shutdown = %#v", status)
	}
}

func TestInstanceStopDuringInitializationAndLifecycleOwnership(t *testing.T) {
	instance, _ := testInstance(t, t.Context(), "hold-init", RPCHandlers{}, testInstanceDeadlines())
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status := instance.Status(); status.State != InstanceStopped || status.Failure != nil {
		t.Fatalf("cancelled startup = %#v", status)
	}
	ready, _ := testInstance(t, t.Context(), "normal", RPCHandlers{}, testInstanceDeadlines())
	if err := ready.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"initialize", "shutdown"} {
		if _, err := ready.Call(t.Context(), method, nil); err == nil {
			t.Fatalf("caller owned %s", method)
		}
		if err := ready.Notify(t.Context(), method, nil); err == nil {
			t.Fatalf("notification owned %s", method)
		}
	}
}

func TestInstanceIdentityAndContextValidation(t *testing.T) {
	installation := Installation{Root: t.TempDir(), Manifest: Manifest{ManifestVersion: 1, ID: "example", Transport: StdioTransport{Type: "stdio", Command: "./must-not-launch"}}}
	initial := PluginContext{Project: ProjectContext{Cwd: t.TempDir()}, Session: SessionContext{ID: "session-one"}}
	for _, id := range []InstanceID{{HostID: "pluginhost_0123456789abcdef0123456789abcdef", SessionID: "session-one", PluginID: "example"}, {HostID: "pluginhost_0123456789abcdef0123456789abcdef", SessionID: "other-session", PluginID: "example", Generation: 1}, {HostID: "pluginhost_0123456789abcdef0123456789abcdef", SessionID: "session-one", PluginID: "different-plugin", Generation: 1}} {
		if _, err := StartInstance(t.Context(), id, installation, initial, RPCHandlers{}); err == nil || errors.Is(err, os.ErrNotExist) {
			t.Fatalf("identity reached launch: %v", err)
		}
	}
	initial.Project.Cwd = "relative"
	if _, err := StartInstance(t.Context(), InstanceID{HostID: "pluginhost_0123456789abcdef0123456789abcdef", SessionID: "session-one", PluginID: "example", Generation: 1}, installation, initial, RPCHandlers{}); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid cwd reached launch: %v", err)
	}
}

func TestInstanceFailureSummaryIsBoundedAndSanitized(t *testing.T) {
	source := "failed\x1b[31m\n\t\u202e" + string([]byte{0xff})
	source += strings.Repeat("猫", MaxFailureMessageBytes)
	summary := diagnosticMessage(errors.New(source))
	if len(summary) > MaxFailureMessageBytes || !utf8.ValidString(summary) || !strings.HasSuffix(summary, "...") {
		t.Fatalf("invalid diagnostic summary: %d bytes", len(summary))
	}
	for _, r := range summary {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			t.Fatalf("diagnostic retained control %U", r)
		}
	}
}

func TestInstancePublishesNotificationsOnlyWhileReady(t *testing.T) {
	instance, _ := testInstance(t, t.Context(), "normal", RPCHandlers{}, testInstanceDeadlines())
	if err := instance.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"cwd":"/next-project"}`)
	if err := instance.Notify(t.Context(), "event", params); err != nil {
		t.Fatal(err)
	}
	result, err := instance.Call(t.Context(), "read-event", nil)
	if err != nil || string(result) != string(params) {
		t.Fatalf("notification = %s, %v", result, err)
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := instance.Notify(t.Context(), "event", params); err == nil {
		t.Fatal("revoked instance accepted notification")
	}
}
