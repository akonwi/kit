package httpapi

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// WatchStreamIdle closes body when no record or heartbeat boundary (a blank
// line) arrives within limit, or when ctx ends. Arbitrary bytes do not reset
// the deadline, so a peer cannot hold a stream open by dripping a partial
// record. Read the returned reader instead of body; stop releases the watchdog
// and closes body.
func WatchStreamIdle(ctx context.Context, body io.ReadCloser, limit time.Duration) (io.Reader, func()) {
	reader := &boundaryReader{source: body}
	reader.last.Store(time.Now().UnixNano())
	done := make(chan struct{})
	var stopped atomic.Bool
	stop := func() {
		if stopped.CompareAndSwap(false, true) {
			close(done)
		}
		_ = body.Close()
	}
	go func() {
		ticker := time.NewTicker(limit / 4)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = body.Close()
				return
			case <-done:
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, reader.last.Load())) > limit {
					_ = body.Close()
					return
				}
			}
		}
	}()
	return reader, stop
}

// boundaryReader records when the most recent blank line completed. Only the
// reading goroutine touches lineBytes; last is shared with the watchdog.
type boundaryReader struct {
	source    io.Reader
	lineBytes int
	last      atomic.Int64
}

func (r *boundaryReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	for _, b := range p[:n] {
		switch b {
		case '\n':
			if r.lineBytes == 0 {
				r.last.Store(time.Now().UnixNano())
			}
			r.lineBytes = 0
		case '\r':
		default:
			r.lineBytes++
		}
	}
	return n, err
}
