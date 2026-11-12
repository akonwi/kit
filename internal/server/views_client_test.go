package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
	kitsession "github.com/akonwi/kit/internal/session"
)

// viewErrorTestService fails the view operations, like the session
// operations of sessionErrorTestService, with err.
type viewErrorTestService struct {
	snapshotSessionErrorTestService
}

func (s viewErrorTestService) WatchViewReveals(context.Context, string, string) (viewRevealSource, error) {
	return nil, s.err
}

func (s viewErrorTestService) RevealView(context.Context, string, string, protocol.ViewRevealInput) (bool, error) {
	return false, s.err
}

// viewTestClient connects the public client to the real handler, whose view
// operations fail with err. With routed false, the handler answers view
// paths as a server that predates them does: with its mux's unrouted 404.
func viewTestClient(t *testing.T, err error, routed bool) *kit.Session {
	t.Helper()
	handler := sessionErrorTestHandler(t, viewErrorTestService{snapshotSessionErrorTestService{sessionErrorTestService{err: err}}}, err)
	served := contractConformanceMiddleware(t, handler, func(err error) { t.Errorf("contract violation: %v", err) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !routed && strings.Contains(r.URL.Path, "/views/") {
			http.NotFound(w, r)
			return
		}
		served.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	client, connectErr := kit.Connect(t.Context(), kit.Endpoint(server.URL, kit.AccessToken("test")))
	if connectErr != nil {
		t.Fatalf("Connect() = %v", connectErr)
	}
	t.Cleanup(func() { _ = client.Close() })
	session, attachErr := client.Attach(t.Context(), sessionErrorTestID)
	if attachErr != nil {
		t.Fatalf("Attach() = %v", attachErr)
	}
	return session
}

// TestViewOperationsTellAnOlderServerFromAMissingSession guards the one
// distinction the terminal client relies on (ADR 0068): both answer 404, but
// only the older server, which never routed the operations, is unsupported.
func TestViewOperationsTellAnOlderServerFromAMissingSession(t *testing.T) {
	t.Parallel()
	viewID := "view_" + strings.Repeat("0a", 16)
	reveal := func(session *kit.Session) error {
		_, err := session.RevealView(t.Context(), viewID, protocol.ViewRevealInput{Line: 3})
		return err
	}
	watch := func(session *kit.Session) error {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		return session.WatchViewReveals(ctx, viewID, func(protocol.ViewReveal) { t.Error("unexpected reveal") })
	}

	older := viewTestClient(t, nil, false)
	for name, err := range map[string]error{"revealView": reveal(older), "watchViewReveals": watch(older)} {
		var unsupported *kit.UnsupportedError
		if !errors.As(err, &unsupported) || unsupported.Capability != name {
			t.Errorf("%s on an older server = %#v, want UnsupportedError", name, err)
		}
	}

	missing := viewTestClient(t, kitsession.ErrNotFound, true)
	want := &kit.ServerError{Code: kit.ErrorNotFound, Message: "session not found"}
	if err := reveal(missing); !sameServerError(err, want) || errors.Is(err, kit.ErrUnsupported) {
		t.Errorf("revealView of a missing session = %#v, want %#v", err, want)
	}
	err := watch(missing)
	var terminal *kit.StreamWatchTerminalError
	if !errors.As(err, &terminal) || errors.Is(err, kit.ErrUnsupported) || !strings.Contains(err.Error(), "session not found") {
		t.Errorf("watchViewReveals of a missing session = %#v, want a terminal not-found failure", err)
	}
}

// TestRevealingAnUnwatchedViewIsNotAnError keeps a view nobody watches, which
// the terminal client answers by restarting the pane, apart from failures.
func TestRevealingAnUnwatchedViewIsNotAnError(t *testing.T) {
	t.Parallel()
	session := viewTestClient(t, nil, true)
	result, err := session.RevealView(t.Context(), "view_"+strings.Repeat("0a", 16), protocol.ViewRevealInput{Annotation: 4})
	if err != nil || result.Delivered {
		t.Fatalf("RevealView() = %+v, %v; want undelivered without error", result, err)
	}
}
