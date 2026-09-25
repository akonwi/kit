package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type testPluginToastStream struct{ updates chan protocol.PluginToast }

func (s *testPluginToastStream) Updates() <-chan protocol.PluginToast { return s.updates }
func (s *testPluginToastStream) Err() error                           { return nil }

type testPluginToastSession struct {
	fakeSession
	opened chan *testPluginToastStream
	closed chan struct{}
}

func (s *testPluginToastSession) WatchPluginToasts(ctx context.Context) (sessionclient.PluginToastStream, error) {
	stream := &testPluginToastStream{updates: make(chan protocol.PluginToast, 1)}
	select {
	case s.opened <- stream:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	go func() { <-ctx.Done(); close(stream.updates); close(s.closed) }()
	return stream, nil
}

func TestPluginToastWatchPresentsLiveNotificationAndSuppressesDetachedCallback(t *testing.T) {
	state, completions, _ := mountPluginExecution(t, func(context.Context, protocol.PluginCommandInput) error { return nil })
	state.showToastOverride = nil
	state.toastCancels = make(map[uint64]context.CancelFunc)
	bound := &testPluginToastSession{fakeSession: fakeSession{id: "attached"}, opened: make(chan *testPluginToastStream, 1), closed: make(chan struct{})}
	state.bound = bound
	state.watchPluginToasts(bound, state.operation)
	var stream *testPluginToastStream
	select {
	case stream = <-bound.opened:
	case <-time.After(time.Second):
		t.Fatal("live stream not opened")
	}
	toast := protocol.PluginToast{PluginID: "plugin-demo", Instance: "owner:1", Title: "Plugin echo", Subtitle: "Hello demo", Variant: "info"}
	stream.updates <- toast
	select {
	case apply := <-completions:
		apply()
	case <-time.After(time.Second):
		t.Fatal("toast not dispatched")
	}
	notices := state.toasts.Snapshot()
	if len(notices) != 1 || notices[0].toastInput != (toastInput{Title: "Plugin echo", Subtitle: "plugin-demo " + glyphMiddleDot + " Hello demo", Variant: toastInfo}) {
		t.Fatalf("live notification = %#v", notices)
	}
	application := uitest.New(toastStack{Toasts: notices})
	application.Pump(100, 24)
	rows := paintedRows(application, 100, 24)
	if findPaintedRow(rows, "Plugin echo") < 0 || findPaintedRow(rows, "plugin-demo "+glyphMiddleDot+" Hello demo") < 0 {
		t.Fatalf("visible plugin toast =\n%s", strings.Join(rows, "\n"))
	}
	toast.Title = "Old attachment callback"
	stream.updates <- toast
	var queued func()
	select {
	case queued = <-completions:
	case <-time.After(time.Second):
		t.Fatal("second toast not queued")
	}
	state.resetAttachmentContext()
	defer state.attachmentCancel()
	queued()
	select {
	case <-bound.closed:
	case <-time.After(time.Second):
		t.Fatal("detached stream not cancelled")
	}
	if got := state.toasts.Snapshot(); len(got) != 0 {
		t.Fatalf("detached notification survived: %#v", got)
	}
}

func TestPluginToastScopesPersistentFeedbackWithoutRemovingOtherToasts(t *testing.T) {
	state, _, _ := mountPluginExecution(t, func(context.Context, protocol.PluginCommandInput) error { return nil })
	state.showToastOverride = nil
	state.toastCancels = make(map[uint64]context.CancelFunc)
	state.showToast(toastInput{Title: "Kit feedback", Variant: toastInfo, Persistent: true})
	for _, variant := range []string{"info", "warning", "error"} {
		state.showPluginToast(protocol.PluginToast{PluginID: "demo", Instance: "owner:1", Title: variant, Variant: variant, Persistent: true})
	}
	got := state.toasts.Snapshot()
	variants := []toastVariant{toastInfo, toastInfo, toastWarning, toastError}
	if len(got) != 4 {
		t.Fatalf("persistent notices = %#v", got)
	}
	for index, toast := range got {
		if !toast.Persistent || toast.Variant != variants[index] {
			t.Fatalf("persistent variant = %#v", toast)
		}
	}
	state.resetAttachmentContext()
	defer state.attachmentCancel()
	remaining := state.toasts.Snapshot()
	if !reflect.DeepEqual(remaining, got[:1]) {
		t.Fatalf("session switch changed unrelated feedback: %#v", remaining)
	}
}
