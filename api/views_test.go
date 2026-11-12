package kit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

var viewTestID = "view_" + strings.Repeat("0a", 16)

type viewTransport struct {
	sessionTransport
	opens    int
	frames   []string
	err      error
	result   protocol.ViewRevealResult
	revealed []protocol.ViewRevealInput
}

func (t *viewTransport) StreamViewReveals(_ context.Context, _ string, viewID string) (io.ReadCloser, error) {
	t.opens++
	if t.err != nil {
		return nil, t.err
	}
	frame := ""
	if len(t.frames) > 0 {
		frame, t.frames = t.frames[0], t.frames[1:]
	}
	return io.NopCloser(strings.NewReader(frame)), nil
}

func (t *viewTransport) RevealView(_ context.Context, _ string, _ string, input protocol.ViewRevealInput) (protocol.ViewRevealResult, error) {
	t.revealed = append(t.revealed, input)
	return t.result, t.err
}

func viewRecord(sessionID, viewID, target string) string {
	return "event: view.reveal\ndata: {\"sessionId\":\"" + sessionID + "\",\"viewId\":\"" + viewID + "\"," + target + "}\n\n"
}

func TestWatchViewRevealsDeliversRevealsInOrderAndReconnects(t *testing.T) {
	transport := &viewTransport{frames: []string{
		"",
		viewRecord(pullRequestTestSession, viewTestID, `"line":40`) + viewRecord(pullRequestTestSession, viewTestID, `"annotation":7`),
	}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	ctx, cancel := context.WithCancel(t.Context())
	var got []protocol.ViewReveal
	err := session.WatchViewReveals(ctx, viewTestID, func(reveal protocol.ViewReveal) {
		got = append(got, reveal)
		if len(got) == 2 {
			cancel()
		}
	})
	want := []protocol.ViewReveal{
		{SessionID: pullRequestTestSession, ViewID: viewTestID, Line: 40},
		{SessionID: pullRequestTestSession, ViewID: viewTestID, Annotation: 7},
	}
	if !errors.Is(err, context.Canceled) || transport.opens != 2 || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("WatchViewReveals() opens=%d err=%v got=%+v", transport.opens, err, got)
	}
}

func TestWatchViewRevealsStopsOnAnotherViewsReveal(t *testing.T) {
	other := "view_" + strings.Repeat("ff", 16)
	transport := &viewTransport{frames: []string{viewRecord(pullRequestTestSession, other, `"line":1`)}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	err := session.WatchViewReveals(t.Context(), viewTestID, func(protocol.ViewReveal) { t.Fatal("another view's reveal delivered") })
	var terminal *StreamWatchTerminalError
	if !errors.As(err, &terminal) || transport.opens != 1 {
		t.Fatalf("WatchViewReveals() opens=%d err=%v, want one terminal attempt", transport.opens, err)
	}
}

func TestViewRoutesOnAServerThatPredatesThemAreUnsupported(t *testing.T) {
	transport := &viewTransport{err: &httpapi.StreamError{Err: httpapi.ErrOperationNotServed}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	err := session.WatchViewReveals(t.Context(), viewTestID, func(protocol.ViewReveal) {})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Capability != "watchViewReveals" || transport.opens != 1 {
		t.Fatalf("WatchViewReveals() opens=%d err=%v, want one unsupported attempt", transport.opens, err)
	}
	transport.err = httpapi.ErrOperationNotServed
	_, err = session.RevealView(t.Context(), viewTestID, protocol.ViewRevealInput{Line: 3})
	if !errors.As(err, &unsupported) || unsupported.Capability != "revealView" {
		t.Fatalf("RevealView() err=%v, want unsupported", err)
	}
}

func TestViewRoutesTellAMissingSessionFromAMissingRoute(t *testing.T) {
	notFound := &httpapi.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: "session not found"}
	transport := &viewTransport{err: notFound}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	err := session.WatchViewReveals(t.Context(), viewTestID, func(protocol.ViewReveal) {})
	var terminal *StreamWatchTerminalError
	if !errors.As(err, &terminal) || errors.Is(err, ErrUnsupported) {
		t.Fatalf("WatchViewReveals() err=%v, want a terminal not-found failure", err)
	}
	_, err = session.RevealView(t.Context(), viewTestID, protocol.ViewRevealInput{Line: 3})
	var server *ServerError
	if !errors.As(err, &server) || server.Code != ErrorNotFound || errors.Is(err, ErrUnsupported) {
		t.Fatalf("RevealView() err=%v, want a not-found server error", err)
	}
}

func TestRevealViewReportsDeliveryAndRejectsInvalidRequestsBeforeSending(t *testing.T) {
	transport := &viewTransport{result: protocol.ViewRevealResult{Delivered: true}}
	session := newSession(nil, transport, pullRequestTestSession, SessionSnapshot{})
	result, err := session.RevealView(t.Context(), viewTestID, protocol.ViewRevealInput{Annotation: 9})
	if err != nil || !result.Delivered || len(transport.revealed) != 1 || transport.revealed[0] != (protocol.ViewRevealInput{Annotation: 9}) {
		t.Fatalf("RevealView() = %+v, %v; sent %+v", result, err, transport.revealed)
	}
	for _, request := range []struct {
		viewID string
		input  protocol.ViewRevealInput
	}{
		{"view_1", protocol.ViewRevealInput{Line: 1}},
		{viewTestID, protocol.ViewRevealInput{}},
	} {
		if _, err := session.RevealView(t.Context(), request.viewID, request.input); err == nil {
			t.Fatalf("RevealView(%q, %+v) accepted an invalid request", request.viewID, request.input)
		}
	}
	if err := session.WatchViewReveals(t.Context(), "view_1", func(protocol.ViewReveal) {}); err == nil {
		t.Fatal("WatchViewReveals accepted an invalid view id")
	}
	if len(transport.revealed) != 1 || transport.opens != 0 {
		t.Fatalf("invalid requests reached the transport: revealed %d, opens %d", len(transport.revealed), transport.opens)
	}
}

func TestAnUnroutedViewRouteIsNotServed(t *testing.T) {
	transport := testSessionTransport(func(context.Context, string, string, io.Reader, bool) (*http.Response, error) {
		header := http.Header{}
		header.Set("Content-Type", "text/plain; charset=utf-8")
		return &http.Response{StatusCode: http.StatusNotFound, Header: header, Body: io.NopCloser(strings.NewReader("404 page not found\n"))}, nil
	})
	if _, err := httpapi.OpenStream(t.Context(), transport, httpapi.StreamViewReveals, httpapi.ViewPath{SessionID: pullRequestTestSession, ViewID: viewTestID}); !errors.Is(err, httpapi.ErrOperationNotServed) {
		t.Fatalf("unrouted open = %v, want ErrOperationNotServed", err)
	}
	if _, err := httpapi.Call(t.Context(), transport, httpapi.RevealView, httpapi.ViewPath{SessionID: pullRequestTestSession, ViewID: viewTestID}, protocol.ViewRevealInput{Line: 1}); !errors.Is(err, httpapi.ErrOperationNotServed) {
		t.Fatalf("unrouted reveal = %v, want ErrOperationNotServed", err)
	}
}
