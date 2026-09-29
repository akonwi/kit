package httpapi

import (
	"io"
	"strings"
	"testing"
	"time"
)

type embeddedBase struct {
	Ready bool `json:"ready"`
}

// EmbeddedOptional is exported because encoding/json cannot allocate an
// embedded pointer to an unexported struct.
type EmbeddedOptional struct {
	Label string `json:"label"`
}

type embeddingPayload struct {
	embeddedBase
	*EmbeddedOptional
	Named embeddedBase `json:"named"`
	Value string       `json:"value"`
}

func TestStrictPayloadAppliesEmbeddedFieldPromotion(t *testing.T) {
	for _, test := range []struct {
		body string
		ok   bool
	}{
		{`{"ready":true,"named":{"ready":false},"value":"a"}`, true},
		{`{"ready":true,"label":"x","named":{"ready":false},"value":"a"}`, true},
		{`{"named":{"ready":false},"value":"a"}`, false},              // promoted required boolean missing
		{`{"ready":null,"named":{"ready":false},"value":"a"}`, false}, // promoted required boolean null
		{`{"ready":true,"named":{},"value":"a"}`, false},              // tagged embedding is a named field
		{`{"ready":true,"value":"a"}`, false},
	} {
		var payload embeddingPayload
		if err := decodeStrictPayload([]byte(test.body), &payload); (err == nil) != test.ok {
			t.Errorf("%s: err = %v, want ok=%t", test.body, err, test.ok)
		}
	}
}

// dripReader delivers its chunks one per interval, then blocks until closed.
type dripReader struct {
	chunks   []string
	interval time.Duration
	closed   chan struct{}
}

func (r *dripReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		<-r.closed
		return 0, io.ErrClosedPipe
	}
	select {
	case <-time.After(r.interval):
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
	n := copy(p, r.chunks[0])
	r.chunks = r.chunks[1:]
	return n, nil
}

func (r *dripReader) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}

func readUntilClosed(t *testing.T, chunks []string, interval, limit time.Duration) (string, time.Duration) {
	t.Helper()
	body := &dripReader{chunks: chunks, interval: interval, closed: make(chan struct{})}
	reader, stop := WatchStreamIdle(t.Context(), body, limit)
	defer stop()
	started := time.Now()
	data, _ := io.ReadAll(reader)
	return string(data), time.Since(started)
}

func TestWatchStreamIdleResetsOnlyAtBoundaries(t *testing.T) {
	// A partial record dripped byte by byte never reaches a boundary.
	drip := strings.Split("event: test.value\ndata: {\"value\":", "")
	data, elapsed := readUntilClosed(t, drip, 10*time.Millisecond, 100*time.Millisecond)
	if elapsed > 300*time.Millisecond || len(data) >= len(drip) {
		t.Fatalf("drip held stream open for %v and %d bytes", elapsed, len(data))
	}
	// Heartbeats (including CRLF) every 40 ms keep a 100 ms bound open.
	beats := []string{": heartbeat\n\n", ": heartbeat\r\n\r\n", ": heartbeat\n", "\n", ": heartbeat\n\n", ": heartbeat\n\n", ": heartbeat\n\n"}
	data, _ = readUntilClosed(t, beats, 40*time.Millisecond, 100*time.Millisecond)
	if data != strings.Join(beats, "") {
		t.Fatalf("heartbeats were cut off: %q", data)
	}
}
