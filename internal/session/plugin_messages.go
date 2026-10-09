package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/identifier"
)

// PluginMessageBoundaryKind identifies plugin-submitted context messages in
// conversation history and transcript projections.
const PluginMessageBoundaryKind = "plugin_message"

// PluginMessageInput is one plugin-authored message for the plugin's owning
// session. IdempotencyKey is optional; when present, a retry with the same key
// and text returns the original admission.
type PluginMessageInput struct {
	PluginID       string
	Text           string
	IdempotencyKey string
}

// PluginMessageResult identifies an admitted plugin message and the turn it started.
type PluginMessageResult struct {
	MessageID string
	TurnID    string
}

// PluginMessageHost installs the session-owned message submission port before
// Start. current reports whether the submitting plugin generation is still
// current; the session checks it under admission authority so a revoked
// generation cannot start a turn.
type PluginMessageHost interface {
	SetMessageObserver(func(context.Context, PluginMessageInput, func() bool) (PluginMessageResult, error))
}

// ErrPluginMessageConflict indicates that an idempotency key was reused with
// different text.
var ErrPluginMessageConflict = errors.New("plugin message idempotency key was used with different text")

var pluginMessageKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func validatePluginMessage(input PluginMessageInput) error {
	if !pluginInteractionDomain.MatchString(input.PluginID) {
		return fmt.Errorf("%w: invalid plugin id", ErrInvalidInput)
	}
	if strings.TrimSpace(input.Text) == "" || len(input.Text) > maxPromptTextBytes || !utf8.ValidString(input.Text) || strings.IndexByte(input.Text, 0) >= 0 {
		return fmt.Errorf("%w: plugin message text must be non-blank UTF-8 without NUL and at most %d bytes", ErrInvalidInput, maxPromptTextBytes)
	}
	if input.IdempotencyKey != "" && !pluginMessageKey.MatchString(input.IdempotencyKey) {
		return fmt.Errorf("%w: invalid plugin message idempotency key", ErrInvalidInput)
	}
	return nil
}

// pluginMessageBoundary derives the durable boundary for one submission. A
// keyed submission has a stable message ID derived from the plugin and key,
// plus a content receipt that distinguishes a retry from a conflicting reuse.
func pluginMessageBoundary(input PluginMessageInput) (droids.BoundaryMessage, error) {
	details, err := json.Marshal(struct {
		Version  int    `json:"version"`
		PluginID string `json:"pluginId"`
	}{1, input.PluginID})
	if err != nil {
		return droids.BoundaryMessage{}, err
	}
	boundary := droids.BoundaryMessage{
		Kind: PluginMessageBoundaryKind, Source: input.PluginID,
		Content: []droids.InputContent{droids.TextInput{Text: input.Text}}, Details: details,
	}
	if input.IdempotencyKey == "" {
		boundary.ID, err = identifier.New("pluginmsg_")
		return boundary, err
	}
	key := sha256.Sum256([]byte("plugin-message-key\x00" + input.PluginID + "\x00" + input.IdempotencyKey))
	content := sha256.Sum256([]byte("plugin-message-content\x00" + input.PluginID + "\x00" + input.IdempotencyKey + "\x00" + input.Text))
	boundary.ID = "pluginmsg_" + hex.EncodeToString(key[:16])
	boundary.ReceiptIDs = []string{boundary.ID, "pluginmsg_content_" + hex.EncodeToString(content[:16])}
	return boundary, nil
}

// replayPluginMessage reports the original admission for a keyed retry, or a
// conflict when the key was admitted with different text.
func replayPluginMessage(ctx context.Context, droid *droids.Droid, boundary droids.BoundaryMessage) (PluginMessageResult, bool, error) {
	if len(boundary.ReceiptIDs) == 0 {
		return PluginMessageResult{}, false, nil
	}
	content, err := droid.BoundaryStatus(ctx, boundary.ReceiptIDs[len(boundary.ReceiptIDs)-1])
	if err != nil {
		return PluginMessageResult{}, false, err
	}
	if content.Received {
		return PluginMessageResult{MessageID: boundary.ID, TurnID: string(content.TurnID)}, true, nil
	}
	key, err := droid.BoundaryStatus(ctx, boundary.ID)
	if err != nil {
		return PluginMessageResult{}, false, err
	}
	if key.Received {
		return PluginMessageResult{}, false, ErrPluginMessageConflict
	}
	return PluginMessageResult{}, false, nil
}

// submitPluginMessage admits a plugin message only when loaded can start a turn
// immediately. Recording the message and starting its turn are atomic; a
// rejected submission records nothing.
func (m *Manager) submitPluginMessage(ctx context.Context, loaded *runtime, sessionID string, input PluginMessageInput, current func() bool) (PluginMessageResult, error) {
	if err := m.beginAdmission(); err != nil {
		return PluginMessageResult{}, err
	}
	defer m.admissions.Done()
	if err := validatePluginMessage(input); err != nil {
		return PluginMessageResult{}, err
	}
	boundary, err := pluginMessageBoundary(input)
	if err != nil {
		return PluginMessageResult{}, err
	}
	// A keyed retry returns its original admission even while the session is busy.
	if result, replayed, err := m.replayOwnedPluginMessage(ctx, loaded, sessionID, boundary, current); err != nil || replayed {
		return result, err
	}
	if !acquireSettledAdmission(ctx, loaded) {
		if err := ctx.Err(); err != nil {
			return PluginMessageResult{}, err
		}
		// A concurrent submission with the same key may have been admitted
		// while this one waited; its receipts commit before its turn is visible.
		if result, replayed, err := m.replayOwnedPluginMessage(ctx, loaded, sessionID, boundary, current); err != nil || replayed {
			return result, err
		}
		return PluginMessageResult{}, ErrBusy
	}
	// Runtime transitions cannot begin until the admitted turn is launched.
	defer loaded.transitionMu.RUnlock()
	loaded.mu.Lock()
	release := func() {
		loaded.mu.Unlock()
		loaded.admissionMu.Unlock()
	}
	if err := m.pluginMessageOwnerLocked(loaded, sessionID, current); err != nil {
		release()
		return PluginMessageResult{}, err
	}
	if err := ctx.Err(); err != nil {
		release()
		return PluginMessageResult{}, err
	}
	if m.sessionDeleting(sessionID) || len(loaded.followUps) > 0 || loaded.activeRun != "" {
		release()
		return PluginMessageResult{}, ErrBusy
	}
	snapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		release()
		return PluginMessageResult{}, err
	}
	if snapshot.Active != nil {
		release()
		return PluginMessageResult{}, ErrBusy
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	if err != nil {
		release()
		return PluginMessageResult{}, err
	}
	if err := m.touchSessionActivity(ctx, sessionID, time.Now().UTC()); err != nil {
		subscription.Close()
		release()
		return PluginMessageResult{}, err
	}
	handle, err := loaded.droid.ReactTo(ctx, boundary)
	if err != nil {
		subscription.Close()
		if errors.Is(err, droids.ErrDuplicateBoundary) {
			result, _, replayErr := replayPluginMessage(context.WithoutCancel(ctx), loaded.droid, boundary)
			release()
			return result, replayErr
		}
		release()
		switch {
		case errors.Is(err, droids.ErrBusy):
			return PluginMessageResult{}, ErrBusy
		case errors.Is(err, droids.ErrConflict):
			return PluginMessageResult{}, ErrPluginMessageConflict
		}
		return PluginMessageResult{}, err
	}
	turnID := string(handle.TurnID())
	if _, err := m.launchAdmittedRunLocked(loaded, sessionID, handle, subscription, true, nil, []NewEvent{
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning},
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventPluginMessage, PluginID: input.PluginID, Text: boundedLiveText(input.Text)},
	}); err != nil {
		return PluginMessageResult{}, err
	}
	return PluginMessageResult{MessageID: boundary.ID, TurnID: turnID}, nil
}

// replayOwnedPluginMessage checks a keyed retry under runtime authority.
func (m *Manager) replayOwnedPluginMessage(ctx context.Context, loaded *runtime, sessionID string, boundary droids.BoundaryMessage, current func() bool) (PluginMessageResult, bool, error) {
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	if err := m.pluginMessageOwnerLocked(loaded, sessionID, current); err != nil {
		return PluginMessageResult{}, false, err
	}
	return replayPluginMessage(ctx, loaded.droid, boundary)
}

// pluginMessageOwnerLocked verifies, under runtime authority, that loaded is
// still the session's runtime and that the submitting generation is current.
func (m *Manager) pluginMessageOwnerLocked(loaded *runtime, sessionID string, current func() bool) error {
	m.mu.Lock()
	owned := !m.closed && m.runtimes[sessionID] == loaded
	m.mu.Unlock()
	if !owned || (current != nil && !current()) {
		return ErrClosed
	}
	return nil
}

// pluginAdmissionSettleWait bounds how long a plugin message waits for a turn
// that has finished but still holds transition and admission authority while
// it settles, for example while its completion event is delivered to plugins.
const pluginAdmissionSettleWait = 2 * time.Second

// acquireSettledAdmission acquires shared transition authority and then
// admission authority, in lock order, waiting briefly while they are held
// by a settling turn. On success the caller owns both. A turn settles once its
// completion has been delivered to plugins, even while it is still recorded as
// active. It fails without contending while a turn is running or user
// follow-ups are queued, so a settling turn's queued follow-ups are always
// admitted first. A runtime transition that outlasts the wait makes the
// submission busy.
func acquireSettledAdmission(ctx context.Context, loaded *runtime) bool {
	deadline := time.Now().Add(pluginAdmissionSettleWait)
	delay := time.Millisecond
	for {
		loaded.mu.Lock()
		activeRun, followUps := loaded.activeRun, len(loaded.followUps)
		loaded.mu.Unlock()
		// A turn whose completion has reached plugins is settling, not running:
		// a plugin that continues from that event waits for it to finish.
		if followUps > 0 || activeRun != "" && !loaded.turnEvents.completionDelivered(activeRun) {
			return false
		}
		if loaded.transitionMu.TryRLock() {
			if loaded.admissionMu.TryLock() {
				return true
			}
			loaded.transitionMu.RUnlock()
		}
		if !time.Now().Before(deadline) {
			return false
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
		delay = min(delay*2, 25*time.Millisecond)
	}
}

// pluginMessagePresentationContent removes droids' model-facing source framing
// so transcript clients receive the submitted text.
func pluginMessagePresentationContent(message droids.ContextMessage) []droids.InputContent {
	if message.Kind != PluginMessageBoundaryKind || len(message.Content) == 0 {
		return message.Content
	}
	if framing, ok := message.Content[0].(droids.TextInput); ok && framing.Text == droids.BoundaryFraming(message.Kind, message.Source) {
		return message.Content[1:]
	}
	return message.Content
}
