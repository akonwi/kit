package session

import (
	"context"
	"sync"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/skills"
)

// ClientToolFeed shares a session runtime's client-hosted plugin tools with
// its subagents' droids (ADR 0051). Attach installs the current tools on a
// droid, tells the droid's skill offering which of them are connected, and
// keeps both current until detach is called or the runtime closes.
type ClientToolFeed interface {
	Attach(*droids.Droid, *skills.Offering) (detach func(), err error)
}

// clientToolFeed is a runtime's ClientToolFeed. Tools reach subagents
// through each droid's additional tools, which children otherwise leave empty.
type clientToolFeed struct {
	mu     sync.Mutex
	closed bool
	tools  []droids.AnyTool
	prompt string
	// names are the model names of tools.
	names  []string
	droids map[*droids.Droid]*skills.Offering
}

// Attach installs the feed's current tools on droid and offers the skills
// they bring through offering, which may be nil.
func (f *clientToolFeed) Attach(droid *droids.Droid, offering *skills.Offering) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return func() {}, nil
	}
	if err := f.install(droid, offering); err != nil {
		return nil, err
	}
	if f.droids == nil {
		f.droids = make(map[*droids.Droid]*skills.Offering)
	}
	f.droids[droid] = offering
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.droids, droid)
	}, nil
}

// install gives droid the feed's tools and offering the feed's tool names.
func (f *clientToolFeed) install(droid *droids.Droid, offering *skills.Offering) error {
	skillPrompt := offering.Prepare(f.names)
	if err := droid.SetAdditionalTools(f.tools, f.prompt+skillPrompt); err != nil {
		offering.SetClientTools(nil)
		return err
	}
	offering.SetClientTools(f.names)
	return nil
}

// publish replaces every attached droid's client tools. A droid that has shut
// down rejects the update and is left to its detach.
func (f *clientToolFeed) publish(tools []droids.AnyTool, prompt string, names []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.tools, f.prompt, f.names = append([]droids.AnyTool(nil), tools...), prompt, append([]string(nil), names...)
	for droid, offering := range f.droids {
		_ = f.install(droid, offering)
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
	for droid, offering := range f.droids {
		offering.SetClientTools(nil)
		_ = droid.SetAdditionalTools(nil, "")
	}
	f.droids = nil
	f.tools, f.prompt, f.names = nil, "", nil
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
