package session

import (
	"context"
	"sync"

	"github.com/akonwi/kit/droids"
)

// ClientToolFeed shares a session runtime's client-hosted plugin tools with
// its subagents' droids (ADR 0051). Attach installs the current tools on a
// droid and keeps them current until detach is called or the runtime closes.
type ClientToolFeed interface {
	Attach(*droids.Droid) (detach func(), err error)
}

// clientToolFeed is a runtime's ClientToolFeed. Tools reach subagents
// through each droid's additional tools, which children otherwise leave empty.
type clientToolFeed struct {
	mu     sync.Mutex
	closed bool
	tools  []droids.AnyTool
	prompt string
	droids map[*droids.Droid]struct{}
}

// Attach installs the feed's current tools on droid.
func (f *clientToolFeed) Attach(droid *droids.Droid) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return func() {}, nil
	}
	if err := droid.SetAdditionalTools(f.tools, f.prompt); err != nil {
		return nil, err
	}
	if f.droids == nil {
		f.droids = make(map[*droids.Droid]struct{})
	}
	f.droids[droid] = struct{}{}
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.droids, droid)
	}, nil
}

// publish replaces every attached droid's client tools. A droid that has shut
// down rejects the update and is left to its detach.
func (f *clientToolFeed) publish(tools []droids.AnyTool, prompt string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.tools, f.prompt = append([]droids.AnyTool(nil), tools...), prompt
	for droid := range f.droids {
		_ = droid.SetAdditionalTools(f.tools, f.prompt)
	}
}

// close withdraws the tools from every attached droid, since the plugin host
// that runs them is closing with the runtime.
func (f *clientToolFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	for droid := range f.droids {
		_ = droid.SetAdditionalTools(nil, "")
	}
	f.droids = nil
	f.tools, f.prompt = nil, ""
}

// ClientToolFeed resolves the client-hosted tool feed of a session runtime
// for its subagents.
func (m *Manager) ClientToolFeed(ctx context.Context, sessionID string) (ClientToolFeed, error) {
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &loaded.clientTools, nil
}
