package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/akonwi/kit/internal/weburl"
)

const (
	openURLMethod = "kit/system/open-url"
	// maxOpenURLParamsBytes bounds the encoded params, allowing for JSON escaping
	// of a maximal address.
	maxOpenURLParamsBytes = 8 << 10
	// MaxOpenURLRequests is how many URL requests one session's plugins may
	// make within OpenURLWindow. Requests beyond it fail as temporarily busy.
	MaxOpenURLRequests = 5
	// OpenURLWindow is the sliding window MaxOpenURLRequests applies to.
	OpenURLWindow = 10 * time.Second
)

// URLRequest is a validated, canonical web address a plugin asked its owning
// session's attached clients to open.
type URLRequest struct {
	Owner InstanceID
	URL   string
}

func parseOpenURL(raw json.RawMessage) (string, error) {
	fields, err := boundedParamsObject(raw, maxOpenURLParamsBytes, "url")
	if err != nil {
		return "", err
	}
	value, present := fields["url"]
	if !present || bytes.Equal(value, []byte("null")) {
		return "", rpcError(-32602, "Missing required parameter: url")
	}
	var address string
	if json.Unmarshal(value, &address) != nil {
		return "", rpcError(-32602, "Invalid parameter: url")
	}
	canonical, err := weburl.Canonical(address)
	if err != nil {
		return "", rpcError(-32602, "Invalid parameter: "+err.Error())
	}
	return canonical, nil
}

// handleOpenURL forwards a current generation's validated address to the
// session owner. It is fire-and-forget: success means the session accepted
// the request, not that any client opened it.
func (h *Host) handleOpenURL(ctx context.Context, owner InstanceID, params json.RawMessage) (json.RawMessage, error) {
	address, err := parseOpenURL(params)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if ctx.Err() != nil {
		h.mu.Unlock()
		return nil, ctx.Err()
	}
	if h.activeEntryLocked(owner) == nil {
		h.mu.Unlock()
		return nil, rpcError(-32002, "Plugin generation is unavailable")
	}
	open := h.config.OpenURL
	if open == nil {
		h.mu.Unlock()
		return nil, rpcError(-32601, "Opening URLs is unavailable")
	}
	admitted, ok := h.admitOpenURLLocked()
	if !ok {
		h.mu.Unlock()
		return nil, &RPCError{Code: -32006, Message: "Too many URL requests; try again later", Data: json.RawMessage(`{"reason":"rate_limited"}`)}
	}
	h.mu.Unlock()
	// Never hold host authority while the session publishes the request.
	if err := open(ctx, URLRequest{Owner: owner, URL: address}); err != nil {
		// Only published requests count toward the limit.
		h.mu.Lock()
		h.releaseOpenURLLocked(admitted)
		h.mu.Unlock()
		return nil, err
	}
	return json.RawMessage("null"), nil
}

// releaseOpenURLLocked forgets one admission recorded at admitted. Callers
// hold h.mu.
func (h *Host) releaseOpenURLLocked(admitted time.Time) {
	for index, at := range h.openURLTimes {
		if at.Equal(admitted) {
			h.openURLTimes = append(h.openURLTimes[:index], h.openURLTimes[index+1:]...)
			return
		}
	}
}

// admitOpenURLLocked records one request at the returned time unless the
// session's plugins have already made MaxOpenURLRequests within
// OpenURLWindow. Callers hold h.mu.
func (h *Host) admitOpenURLLocked() (time.Time, bool) {
	now := h.now()
	kept := h.openURLTimes[:0]
	for _, at := range h.openURLTimes {
		if now.Sub(at) < OpenURLWindow {
			kept = append(kept, at)
		}
	}
	h.openURLTimes = kept
	if len(h.openURLTimes) >= MaxOpenURLRequests {
		return time.Time{}, false
	}
	h.openURLTimes = append(h.openURLTimes, now)
	return now, true
}
