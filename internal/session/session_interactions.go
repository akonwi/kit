package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/akonwi/kit/droids"
)

// sessionInteractions asks the session's user through the session's current
// droid, which owns every pending interaction. It remembers recently settled
// interactions, so a late answer is told the question was settled, and the
// plugin generations whose dialogs may still be answered.
type sessionInteractions struct {
	sessionID string
	// publish appends events for questions asked outside any turn; a run's
	// drain projects the others.
	publish func(NewEvent)

	mu      sync.Mutex
	droid   *droids.Droid
	settled []string
	fences  map[PluginInteractionOwner]*pluginFence
}

// pluginFence reports whether a plugin generation's dialogs may still be
// shown and answered. refs counts its dialogs in flight.
type pluginFence struct {
	available func() bool
	refs      int
}

// interactionMetadata is the application metadata Kit stores with a droid
// interaction.
type interactionMetadata struct {
	Plugin *PluginInteractionOwner `json:"plugin,omitempty"`
}

func newSessionInteractions(sessionID string, publish func(NewEvent)) *sessionInteractions {
	return &sessionInteractions{sessionID: sessionID, publish: publish, fences: map[PluginInteractionOwner]*pluginFence{}}
}

// setDroid makes droid the one that asks; a configuration change replaces it.
func (s *sessionInteractions) setDroid(droid *droids.Droid) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.droid = droid
}

func (s *sessionInteractions) current() *droids.Droid {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.droid
}

// remember records that interaction id settled, so a late response reports
// it as settled.
func (s *sessionInteractions) remember(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.settled, id) {
		return
	}
	s.settled = append(s.settled, id)
	if len(s.settled) > 64 {
		s.settled = append([]string(nil), s.settled[len(s.settled)-64:]...)
	}
}

// notPending reports why id cannot be answered: it settled recently, or it
// was never pending.
func (s *sessionInteractions) notPending(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.settled, id) {
		return ErrInteractionSettled
	}
	return ErrInteractionNotFound
}

// fence lets owner's dialogs be shown and answered while available reports
// true, until the returned release is called.
func (s *sessionInteractions) fence(owner PluginInteractionOwner, available func() bool) (release func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fence := s.fences[owner]
	if fence == nil {
		fence = &pluginFence{}
		s.fences[owner] = fence
	}
	fence.available = available
	fence.refs++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if fence.refs--; fence.refs == 0 && s.fences[owner] == fence {
			delete(s.fences, owner)
		}
	}
}

// visible reports whether request may be shown and answered: a plugin's
// dialog only while its plugin generation is active.
func (s *sessionInteractions) visible(request InteractionRequest) bool {
	if request.Plugin == nil {
		return true
	}
	s.mu.Lock()
	fence := s.fences[*request.Plugin]
	var available func() bool
	if fence != nil {
		available = fence.available
	}
	s.mu.Unlock()
	return available != nil && available()
}

// pending projects the droid's pending interactions that may be shown,
// oldest first.
func (s *sessionInteractions) pending(ctx context.Context) ([]InteractionRequest, error) {
	droid := s.current()
	if droid == nil {
		return nil, nil
	}
	interactions, err := droid.Interactions(ctx)
	if err != nil {
		return nil, err
	}
	return s.project(interactions), nil
}

// project projects interactions, omitting those that may not be shown.
func (s *sessionInteractions) project(interactions []droids.Interaction) []InteractionRequest {
	requests := make([]InteractionRequest, 0, len(interactions))
	for _, interaction := range interactions {
		if request := projectDroidInteraction(s.sessionID, interaction); s.visible(request) {
			requests = append(requests, request)
		}
	}
	return requests
}

// ask asks the user a live question through the current droid and waits for
// the settlement. A question attached to a tool call belongs to its turn,
// whose run announces it; ask announces one asked outside any turn itself.
func (s *sessionInteractions) ask(ctx context.Context, question droids.Question, options droids.AskOptions) (droids.Settlement, error) {
	droid := s.current()
	if droid == nil {
		return droids.Settlement{}, ErrClosed
	}
	announced := ""
	if options.Call == nil {
		options.Requested = func(interaction droids.Interaction) {
			request := projectDroidInteraction(s.sessionID, interaction)
			announced = request.ID
			s.publish(NewEvent{SessionID: s.sessionID, Kind: EventInteractionRequested, Interaction: ptrInteractionRequest(request)})
		}
	}
	settlement, err := droid.Ask(ctx, question, options)
	// Ask returns ctx's error alone once the unanswered question is recorded
	// as settled; joined with another error, it is still pending.
	settled := settlement.Status != "" && (err == nil || err == ctx.Err())
	if announced != "" && settled {
		s.remember(announced)
		s.publish(NewEvent{SessionID: s.sessionID, Kind: EventInteractionResolved, InteractionID: announced, InteractionResolution: droidInteractionResolution(settlement)})
	}
	switch {
	case err == nil:
		return settlement, nil
	case err == ctx.Err():
		return settlement, err
	case errors.Is(err, droids.ErrBusy):
		return settlement, ErrInteractionCapacity
	case errors.Is(err, droids.ErrInvalidInteraction):
		return settlement, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	case errors.Is(err, droids.ErrClosed):
		return settlement, ErrClosed
	}
	return settlement, err
}

// pluginMetadata is the metadata stored with a plugin's dialog.
func pluginMetadata(owner PluginInteractionOwner) json.RawMessage {
	raw, _ := json.Marshal(interactionMetadata{Plugin: &owner})
	return raw
}

// interactionPlugin reads the plugin that owns an interaction, if any.
func interactionPlugin(metadata json.RawMessage) *PluginInteractionOwner {
	if len(metadata) == 0 {
		return nil
	}
	var decoded interactionMetadata
	if json.Unmarshal(metadata, &decoded) != nil || decoded.Plugin == nil || !validPluginInteractionOwner(*decoded.Plugin) {
		return nil
	}
	return decoded.Plugin
}

// hookCallKey carries the tool call a before-tool hook checks to the plugin
// dialogs the hook opens.
type hookCallKey struct{}

func withHookCall(ctx context.Context, call droids.ToolContext) context.Context {
	return context.WithValue(ctx, hookCallKey{}, call)
}

func hookCall(ctx context.Context) (droids.ToolContext, bool) {
	call, ok := ctx.Value(hookCallKey{}).(droids.ToolContext)
	return call, ok && call.TurnID != "" && call.ToolCallID != ""
}
