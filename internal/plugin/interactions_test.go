package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

func TestInteractionParsingPreservesV1FieldsAndOpaqueValues(t *testing.T) {
	confirm, err := parseInteraction("kit/ui/confirm", json.RawMessage(`{"title":"Confirm","message":"**Markdown**\n\n\u0060\u0060\u0060go\nx()\n\u0060\u0060\u0060","confirmLabel":"Continue","cancelLabel":"Stop","defaultValue":true}`))
	if err != nil || confirm.Kind != InteractionConfirm || confirm.ConfirmLabel != "Continue" || confirm.CancelLabel != "Stop" || confirm.DefaultValue == nil || !*confirm.DefaultValue || !strings.Contains(confirm.Message, "```go") {
		t.Fatalf("confirm = %#v, %v", confirm, err)
	}
	input, err := parseInteraction("kit/ui/input", json.RawMessage(`{"title":"Input","message":null,"placeholder":"Your note","initialValue":"initial\nvalue"}`))
	if err != nil || input.Placeholder != "Your note" || input.InitialValue != "initial\nvalue" {
		t.Fatalf("input = %#v, %v", input, err)
	}
	selected, err := parseInteraction("kit/ui/select", json.RawMessage(`{"title":"Select","filterable":true,"placeholder":"Search","options":[{"label":"Null","value":null},{"label":"Large","value":9007199254740993,"description":"Exact integer"},{"label":"Object","value":{"nested":[true,"hello"]}}]}`))
	if err != nil || !selected.Filterable || len(selected.Options) != 3 {
		t.Fatalf("select = %#v, %v", selected, err)
	}
	for index, want := range []string{`{"value":null}`, `{"value":9007199254740993}`, `{"value":{"nested":[true,"hello"]}}`} {
		result, err := interactionResult(selected, InteractionResponse{OptionIndex: index})
		if err != nil || string(result) != want {
			t.Fatalf("selection %d = %s, %v", index, result, err)
		}
	}
	for _, request := range []InteractionRequest{confirm, input, selected} {
		result, err := interactionResult(request, InteractionResponse{Cancelled: true})
		want := "null"
		if request.Kind == InteractionConfirm {
			want = "false"
		}
		if err != nil || string(result) != want {
			t.Fatalf("cancel %s = %s, %v", request.Kind, result, err)
		}
	}
	result, err := interactionResult(input, InteractionResponse{Text: ""})
	if err != nil || string(result) != `""` {
		t.Fatalf("empty input = %s, %v", result, err)
	}
	result, err = interactionResult(confirm, InteractionResponse{Confirmed: true})
	if err != nil || string(result) != "true" {
		t.Fatalf("confirm result = %s, %v", result, err)
	}
}

func TestInteractionPayloadAndResultValidation(t *testing.T) {
	for _, test := range []struct{ method, raw string }{
		{"confirm", `{"title":"Confirm","defaultValue":null}`},
		{"confirm", `{"title":"Confirm","defaultValue":"true"}`},
		{"confirm", `{"title":"Confirm","title":"Duplicate"}`},
		{"confirm", `{"title":"Confirm","initialValue":"Wrong kind"}`},
		{"input", `{"title":"Input","message":"\u001b"}`},
		{"input", `{"title":"Input","initialValue":1}`},
		{"input", `{"title":null}`},
		{"select", `{"title":"Select","options":[]}`},
		{"select", `{"title":"Select","options":null}`},
		{"select", `{"title":"Select","options":[{"label":"Missing value"}]}`},
		{"select", `{"title":"Select","options":[{"label":"x","value":null,"value":false}]}`},
		{"select", `{"title":"Select","options":[{"label":"x","value":null,"unknown":1}]}`},
		{"select", `{"title":"Select","filterable":null,"options":[{"label":"x","value":null}]}`},
	} {
		if _, err := parseInteraction("kit/ui/"+test.method, json.RawMessage(test.raw)); err == nil {
			t.Errorf("accepted %s", test.raw)
		}
	}
	for _, test := range []struct{ method, raw string }{
		{"input", `{"title":"Input","initialValue":"` + strings.Repeat("x", maxInteractionTextBytes+1) + `"}`},
		{"select", `{"title":"Select","options":[` + strings.Repeat(`{"label":"x","value":null},`, maxInteractionOptions) + `{"label":"last","value":null}]}`},
		{"select", `{"title":"Select","options":[{"label":"x","value":"` + strings.Repeat("x", maxInteractionTextBytes) + `"}]}`},
	} {
		if _, err := parseInteraction("kit/ui/"+test.method, json.RawMessage(test.raw)); err == nil {
			t.Errorf("accepted oversized %s", test.method)
		}
	}
	request := InteractionRequest{Kind: InteractionSelect, Options: []InteractionOption{{Value: json.RawMessage("null")}}}
	for _, index := range []int{-1, 1} {
		if _, err := interactionResult(request, InteractionResponse{OptionIndex: index}); err == nil {
			t.Errorf("accepted invalid index %d", index)
		}
	}
	if _, err := interactionResult(InteractionRequest{Kind: InteractionInput}, InteractionResponse{Text: "\x00"}); err == nil {
		t.Fatal("invalid input answer accepted")
	}
}

func interactionHost(t *testing.T, observer func(context.Context, InteractionRequest) (InteractionResponse, error)) (*Host, InstanceID) {
	t.Helper()
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "demo", "demo", "commands")
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "one"}, Interaction: observer})
	t.Cleanup(func() {
		if err := host.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 4 })
	return host, host.Commands()[0].Owner
}

func TestInteractionHostFencesOwnerAndRetainsPrivateOptionValues(t *testing.T) {
	var observed InstanceID
	host, owner := interactionHost(t, func(_ context.Context, request InteractionRequest) (InteractionResponse, error) {
		observed = request.Owner
		request.Options[0].Value[0] = '1'
		request.Options[0].Value = json.RawMessage(`"replacement"`)
		return InteractionResponse{OptionIndex: 0}, nil
	})
	result, err := host.handleRequest(t.Context(), owner, "kit/ui/select", json.RawMessage(`{"title":"Choose","options":[{"label":"Zero","value":0}]}`))
	if err != nil || string(result) != `{"value":0}` || observed != owner {
		t.Fatalf("retained value = %s, %v, owner %#v", result, err, observed)
	}
	host.Reload()
	_, err = host.handleRequest(t.Context(), owner, "kit/ui/confirm", json.RawMessage(`{"title":"Late"}`))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32002 {
		t.Fatalf("old owner = %v", err)
	}
}

func TestInteractionHostUnavailableIsNotCancellation(t *testing.T) {
	for _, observer := range []func(context.Context, InteractionRequest) (InteractionResponse, error){nil, func(context.Context, InteractionRequest) (InteractionResponse, error) {
		return InteractionResponse{}, ErrInteractivityUnavailable
	}} {
		host, owner := interactionHost(t, observer)
		result, err := host.handleRequest(t.Context(), owner, "kit/ui/confirm", json.RawMessage(`{"title":"Confirm"}`))
		var failure *RPCError
		if len(result) != 0 || !errors.As(err, &failure) || failure.Code != -32000 || string(failure.Data) != `{"reason":"interactivity_unavailable"}` {
			t.Fatalf("unavailable = %s, %v", result, err)
		}
	}
}

func TestInteractionHostBoundsPendingRequests(t *testing.T) {
	entered := make(chan struct{}, maxPendingPluginInteractions)
	host, owner := interactionHost(t, func(ctx context.Context, _ InteractionRequest) (InteractionResponse, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return InteractionResponse{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var wg sync.WaitGroup
	for range maxPendingPluginInteractions {
		wg.Go(func() { _, _ = host.handleRequest(ctx, owner, "kit/ui/input", json.RawMessage(`{"title":"Wait"}`)) })
	}
	for range maxPendingPluginInteractions {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("observer not entered")
		}
	}
	_, err := host.handleRequest(t.Context(), owner, "kit/ui/input", json.RawMessage(`{"title":"Overflow"}`))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32005 {
		t.Fatalf("pending limit = %v", err)
	}
	cancel()
	wg.Wait()
	host.mu.Lock()
	pending := host.interactionsInFlight
	host.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending after cancellation = %d", pending)
	}
}

func TestInteractionHostRejectsAnswerAfterReload(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	host, owner := interactionHost(t, func(context.Context, InteractionRequest) (InteractionResponse, error) {
		close(entered)
		<-release
		return InteractionResponse{Confirmed: true}, nil
	})
	result := make(chan error, 1)
	go func() {
		_, err := host.handleRequest(t.Context(), owner, "kit/ui/confirm", json.RawMessage(`{"title":"Confirm"}`))
		result <- err
	}()
	<-entered
	host.Reload()
	close(release)
	var failure *RPCError
	if err := <-result; !errors.As(err, &failure) || failure.Code != -32002 {
		t.Fatalf("late answer = %v", err)
	}
}

func TestUIDemoV1FixtureThroughHostInteractionAdapter(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	installUIDemoFixture(t, home)
	var mu sync.Mutex
	var kinds []InteractionKind
	notices := make(chan Toast, 8)
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: t.TempDir(), Session: SessionContext{ID: "ui-session"}, Interaction: func(ctx context.Context, request InteractionRequest) (InteractionResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		kinds = append(kinds, request.Kind)
		switch request.Kind {
		case InteractionSelect:
			return InteractionResponse{OptionIndex: 0}, nil
		case InteractionInput:
			if request.InitialValue != "initial note" {
				t.Errorf("initial value = %q", request.InitialValue)
			}
			return InteractionResponse{Text: "Answered note"}, nil
		case InteractionConfirm:
			if request.ConfirmLabel != "Show toast" || request.CancelLabel != "Cancel" || request.DefaultValue == nil || !*request.DefaultValue {
				t.Errorf("confirm metadata = %#v", request)
			}
			return InteractionResponse{Confirmed: true}, nil
		default:
			return InteractionResponse{}, errors.New("unexpected interaction")
		}
	}, Toast: func(ctx context.Context, toast Toast) error {
		select {
		case notices <- toast:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 1 })
	command := host.Commands()[0]
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := host.ExecuteCommand(ctx, command, "initial note"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]InteractionKind(nil), kinds...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []InteractionKind{InteractionSelect, InteractionSelect, InteractionInput, InteractionConfirm}) {
		t.Fatalf("flow = %v", got)
	}
	for range 2 {
		select {
		case toast := <-notices:
			if toast.Title == "UI demo complete" {
				if toast.Subtitle != "Current file · info · note=Answered note" {
					t.Fatalf("final toast = %#v", toast)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("missing final UI demo toast")
		}
	}
	t.Fatal("UI demo did not finish")
}

func installUIDemoFixture(t *testing.T, home apphome.Paths) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("UI fixture requires python3")
	}
	root := filepath.Join(home.Plugins, "ui-api-demo")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plugin.py", "plugin.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", ".kit", "plugins", "ui-api-demo", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPluginUIRequestCancellationFollowsInstanceReload(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	installUIDemoFixture(t, home)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: t.TempDir(), Session: SessionContext{ID: "one"}, Interaction: func(ctx context.Context, _ InteractionRequest) (InteractionResponse, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return InteractionResponse{}, ctx.Err()
	}, Toast: func(context.Context, Toast) error { return nil }})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 1 })
	command := host.Commands()[0]
	result := make(chan error, 1)
	go func() { result <- host.ExecuteCommand(t.Context(), command, "") }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("plugin did not enter UI callback")
	}
	host.Reload()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("revocation did not cancel plugin UI request")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("revoked UI command succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revoked command did not return")
	}
	eventuallyHost(t, func() bool {
		commands := host.Commands()
		return len(commands) == 1 && commands[0].Owner != command.Owner
	})
}

func TestInteractionOwnershipAcrossCWDAndRename(t *testing.T) {
	for _, source := range []Source{User, Project} {
		t.Run(string(source), func(t *testing.T) {
			home := apphome.FromHome(t.TempDir())
			cwd := t.TempDir()
			root := home.Plugins
			if source == Project {
				root = filepath.Join(cwd, ".kit", "plugins")
			}
			hostInstallation(t, root, "demo", "demo", "commands")
			entered, release := make(chan struct{}), make(chan struct{})
			host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "one"}, Interaction: func(context.Context, InteractionRequest) (InteractionResponse, error) {
				close(entered)
				<-release
				return InteractionResponse{Confirmed: true}, nil
			}})
			defer host.Close(context.Background())
			host.Start()
			eventuallyHost(t, func() bool { return len(host.Commands()) == 4 })
			owner := host.Commands()[0].Owner
			type outcome struct {
				value json.RawMessage
				err   error
			}
			result := make(chan outcome, 1)
			go func() {
				value, err := host.handleRequest(t.Context(), owner, "kit/ui/confirm", json.RawMessage(`{"title":"Original question"}`))
				result <- outcome{value, err}
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("observer not entered")
			}
			host.ChangeCWD(t.TempDir())
			host.Rename("Renamed")
			close(release)
			got := <-result
			if source == User {
				if got.err != nil || string(got.value) != "true" {
					t.Fatalf("retained user-instance answer = %s, %v", got.value, got.err)
				}
			} else {
				var failure *RPCError
				if !errors.As(got.err, &failure) || failure.Code != -32002 {
					t.Fatalf("revoked project-instance answer = %s, %v", got.value, got.err)
				}
			}
		})
	}
}
