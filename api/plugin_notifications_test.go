package kit

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

type reconnectToastTransport struct {
	sessionTransport
	mu    sync.Mutex
	opens int
}

func (t *reconnectToastTransport) StreamPluginToasts(context.Context, string) (io.ReadCloser, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.opens++
	title := "First"
	if t.opens > 1 {
		title = "Second"
	}
	body := "event: plugin.toast\ndata: {\"pluginId\":\"demo\",\"instance\":\"owner:1\",\"title\":\"" + title + "\",\"variant\":\"info\"}\n\n"
	return io.NopCloser(strings.NewReader(body)), nil
}

func TestPluginToastStreamReconnectsAfterCleanEnd(t *testing.T) {
	transport := &reconnectToastTransport{}
	session := newSession(nil, transport, "session_test", SessionSnapshot{})
	stream, err := session.WatchPluginToasts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for _, title := range []string{"First", "Second"} {
		select {
		case toast := <-stream.Updates():
			if toast.Title != title {
				t.Fatalf("toast title = %q, want %q", toast.Title, title)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s toast", title)
		}
	}
}

func TestReadPluginToastStreamClassifiesEndings(t *testing.T) {
	const record = "event: plugin.toast\ndata: {\"pluginId\":\"demo\",\"instance\":\"owner:1\",\"title\":\"Notice\",\"variant\":\"info\"}\n\n"
	for name, test := range map[string]struct {
		body     string
		terminal bool
	}{
		"clean end":      {": connected\n\n" + record, false},
		"truncated":      {": connected\n\n" + strings.TrimSuffix(record, "\n"), true},
		"oversized":      {"event: plugin.toast\ndata: " + strings.Repeat(" ", 32<<10) + "\n\n", true},
		"invalid record": {"event: plugin.toast\ndata: {}\n\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			updates := make(chan protocol.PluginToast, 1)
			err := readPluginToastStream(t.Context(), io.NopCloser(strings.NewReader(test.body)), updates)
			var terminal *StreamWatchTerminalError
			if errors.As(err, &terminal) != test.terminal {
				t.Fatalf("err = %v, terminal want %t", err, test.terminal)
			}
			if name == "clean end" && (err != nil || len(updates) != 1 || (<-updates).Title != "Notice") {
				t.Fatalf("clean end: err=%v delivered=%d", err, len(updates))
			}
		})
	}
}
