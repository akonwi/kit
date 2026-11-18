package session

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/droids"
	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/identifier"
)

// Follow-ups are prompts submitted while a turn runs. They wait in the
// session droid's durable queue (ADR 0073), which starts each one as the turn
// before it settles. Kit resolves a follow-up's content when it is submitted,
// projects the droid's queue to clients, and follows each turn the queue
// starts as it follows the turns it starts itself.

// queuedFollowUp is the composer form of one queued prompt, reconstructed
// from the content Kit resolved for it.
type queuedFollowUp struct {
	input PromptInput
	// annotationSubmission identifies the annotations the prompt submits.
	annotationSubmission string
}

// followUpFromContent reconstructs a queued prompt's composer form from its
// resolved content.
func followUpFromContent(content []droids.InputContent) queuedFollowUp {
	var followUp queuedFollowUp
	for _, block := range content {
		switch value := block.(type) {
		case droids.TextInput:
			if value.AttachmentID != "" {
				followUp.input.AttachmentIDs = append(followUp.input.AttachmentIDs, value.AttachmentID)
			} else if followUp.input.Text == "" {
				followUp.input.Text = value.Text
			}
		case droids.PromptCommandInput:
			followUp.input.Text = value.Text
			followUp.input.promptCommand = &promptCommandInvocation{name: value.Name, arguments: value.Arguments, source: value.Source}
		case droids.AnnotationInput:
			followUp.annotationSubmission = value.SubmissionID
			for _, annotation := range value.Annotations {
				followUp.input.AnnotationIDs = append(followUp.input.AnnotationIDs, annotation.ID)
			}
		case droids.FileInput:
			if value.AttachmentID != "" {
				followUp.input.AttachmentIDs = append(followUp.input.AttachmentIDs, value.AttachmentID)
			}
		}
	}
	return followUp
}

// restoredPrompt is the prompt a client puts back in its composer. A prompt
// command returns as its invocation, so submitting it again expands the
// command's current template.
func (followUp queuedFollowUp) restoredPrompt() PromptInput {
	input := PromptInput{Text: followUp.input.Text, AttachmentIDs: followUp.input.AttachmentIDs, AnnotationIDs: followUp.input.AnnotationIDs}
	if invocation := followUp.input.promptCommand; invocation != nil {
		input.Text = invocation.text()
	}
	return input
}

// queuedFollowUpEntry is one queued prompt Kit knows of, in queue order.
type queuedFollowUpEntry struct {
	id       droids.QueuedPromptID
	followUp queuedFollowUp
}

// takeQueuedFollowUpLocked removes and returns the prompt id. The caller
// holds loaded.mu.
func (loaded *runtime) takeQueuedFollowUpLocked(id droids.QueuedPromptID) (queuedFollowUp, bool) {
	for index, entry := range loaded.queuedFollowUps {
		if entry.id == id {
			loaded.queuedFollowUps = slices.Delete(loaded.queuedFollowUps, index, index+1)
			return entry.followUp, true
		}
	}
	return queuedFollowUp{}, false
}

// livePromptText is the text a turn's live user-message event shows.
func livePromptText(input PromptInput) string {
	text := input.Text
	if input.promptCommand != nil {
		text = input.promptCommand.text()
	}
	if strings.TrimSpace(text) == "" {
		if len(input.AnnotationIDs) > 0 {
			return "Annotations"
		}
		return "Attachment"
	}
	return text
}

// refreshFollowUpsLocked reprojects the droid's queue, remembers the composer
// form of every queued prompt, and, when announce is set, announces a changed
// projection to attached clients. The caller holds loaded.mu. A queue that
// cannot be read keeps the last projection.
//
// A prompt whose turn the droid started stays first in the projection until
// Kit follows that turn, so no snapshot shows the session idle in between.
func (loaded *runtime) refreshFollowUpsLocked(ctx context.Context, sessionID string, announce bool) FollowUpQueue {
	queue, err := loaded.droid.Queue(ctx)
	if err != nil {
		return loaded.followUps
	}
	queued := make(map[droids.QueuedPromptID]bool, len(queue.Prompts))
	for _, prompt := range queue.Prompts {
		queued[prompt.ID] = true
	}
	entries := make([]queuedFollowUpEntry, 0, len(loaded.queuedFollowUps)+len(queue.Prompts))
	known := make(map[droids.QueuedPromptID]queuedFollowUp, len(loaded.queuedFollowUps))
	for _, entry := range loaded.queuedFollowUps {
		known[entry.id] = entry.followUp
		if !queued[entry.id] {
			entries = append(entries, entry)
		}
	}
	for _, prompt := range queue.Prompts {
		followUp, ok := known[prompt.ID]
		if !ok {
			followUp = followUpFromContent(prompt.Input.Content)
		}
		entries = append(entries, queuedFollowUpEntry{id: prompt.ID, followUp: followUp})
	}
	loaded.queuedFollowUps = entries
	inputs := make([]PromptInput, 0, len(entries))
	for _, entry := range entries {
		inputs = append(inputs, entry.followUp.input)
	}
	projection := projectFollowUpQueue(inputs)
	if projection.Count > 0 && queue.Blocked != "" {
		projection.AdmissionFailure = followUpAdmissionFailure(queue.Blocked)
	}
	changed := !reflect.DeepEqual(projection, loaded.followUps)
	loaded.followUps = projection
	if announce && changed {
		queue := projection
		if err := loaded.events.append([]NewEvent{{SessionID: sessionID, Kind: EventFollowUpsChanged, FollowUps: &queue}}); err != nil {
			loaded.events.invalidate()
		}
	}
	return projection
}

// followUpAdmissionFailure is a bounded renderer-safe explanation of why the
// first queued prompt could not start.
func followUpAdmissionFailure(reason string) string {
	message := rendererSafeText(reason)
	if message == "" {
		return "the queued message could not start"
	}
	return truncateRendererText(message, maxFollowUpTextBytes)
}

// SubmitPromptInput atomically starts an idle session or queues a structured follow-up.
func (m *Manager) SubmitPromptInput(ctx context.Context, sessionID string, prompt PromptInput) (PromptSubmission, error) {
	if err := m.beginOperation(); err != nil {
		return PromptSubmission{}, err
	}
	defer m.ops.Done()
	if err := validatePromptInput(prompt); err != nil {
		return PromptSubmission{}, err
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return PromptSubmission{}, err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	if err := m.validateQueueRuntime(sessionID, loaded); err != nil {
		return PromptSubmission{}, err
	}
	loaded.mu.Lock()
	running := loaded.activeRun != ""
	loaded.mu.Unlock()
	if !running {
		reservation, err := m.StartPromptInput(ctx, sessionID, prompt)
		if err != nil {
			return PromptSubmission{}, err
		}
		return PromptSubmission{Reservation: reservation}, nil
	}
	return m.queueFollowUp(ctx, loaded, sessionID, prompt)
}

// queueFollowUp resolves prompt and admits it to the droid's queue. Its
// annotations stay pinned to it until it starts or is withdrawn.
func (m *Manager) queueFollowUp(ctx context.Context, loaded *runtime, sessionID string, prompt PromptInput) (PromptSubmission, error) {
	loaded.mu.Lock()
	for _, queued := range loaded.queuedFollowUps {
		for _, id := range queued.followUp.input.AnnotationIDs {
			if slices.Contains(prompt.AnnotationIDs, id) {
				loaded.mu.Unlock()
				return PromptSubmission{}, fmt.Errorf("%w: annotation %d already belongs to a queued message", ErrInvalidInput, id)
			}
		}
	}
	loaded.mu.Unlock()
	prepared, err := m.prepareAnnotations(ctx, loaded, sessionID, prompt)
	if err != nil {
		return PromptSubmission{}, err
	}
	var annotationRecords []kitannotation.Record
	annotationSubmission := ""
	if prepared != nil {
		annotationRecords = prepared.Records
		annotationSubmission, err = identifier.New("annotation_submission_")
		if err != nil {
			prepared.Abort()
			return PromptSubmission{}, err
		}
	}
	content, err := m.resolvePromptContent(ctx, sessionID, loaded.model, prompt, annotationRecords, annotationSubmission)
	if err != nil {
		if prepared != nil {
			prepared.Abort()
		}
		return PromptSubmission{}, err
	}
	var pinned *kitannotation.QueuedSubmission
	if prepared != nil {
		pinned = prepared.Queue()
	}
	admission, err := loaded.droid.Enqueue(ctx, droids.Input{Content: content}, droids.QueueOptions{})
	if err != nil {
		if pinned != nil {
			pinned.Release()
		}
		if errors.Is(err, droids.ErrBusy) {
			return PromptSubmission{}, fmt.Errorf("%w: follow-up queue capacity reached", ErrBusy)
		}
		if errors.Is(err, droids.ErrClosed) {
			return PromptSubmission{}, ErrClosed
		}
		return PromptSubmission{}, err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	loaded.queuedFollowUps = append(loaded.queuedFollowUps, queuedFollowUpEntry{id: admission.Prompt.ID, followUp: followUpFromContent(content)})
	queue := loaded.refreshFollowUpsLocked(ctx, sessionID, true)
	// The droid starts a prompt at once when its turn settled meanwhile; the
	// turn is followed as any other the queue starts.
	return PromptSubmission{Queued: true, Queue: queue}, nil
}

// RestoreFollowUps atomically withdraws every queued prompt, in queue order,
// and returns each to its composer form.
func (m *Manager) RestoreFollowUps(ctx context.Context, sessionID string) (FollowUpRestore, error) {
	if err := m.beginOperation(); err != nil {
		return FollowUpRestore{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return FollowUpRestore{}, err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	if err := m.validateQueueRuntime(sessionID, loaded); err != nil {
		return FollowUpRestore{}, err
	}
	withdrawn, err := loaded.droid.Withdraw(ctx)
	if err != nil {
		return FollowUpRestore{}, err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	messages := make([]PromptInput, 0, len(withdrawn))
	for _, prompt := range withdrawn {
		followUp, known := loaded.takeQueuedFollowUpLocked(prompt.ID)
		if !known {
			followUp = followUpFromContent(prompt.Input.Content)
		}
		if m.annotations != nil && len(followUp.input.AnnotationIDs) > 0 {
			m.annotations.UnpinQueued(sessionID, followUp.input.AnnotationIDs)
		}
		messages = append(messages, followUp.restoredPrompt())
	}
	queue := loaded.refreshFollowUpsLocked(ctx, sessionID, true)
	return FollowUpRestore{Messages: messages, Queue: queue}, nil
}

// PromoteFollowUps atomically moves every queued prompt into the running
// turn as steering.
func (m *Manager) PromoteFollowUps(ctx context.Context, sessionID string) (FollowUpPromotion, error) {
	if err := m.beginOperation(); err != nil {
		return FollowUpPromotion{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return FollowUpPromotion{}, err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	if err := m.validateQueueRuntime(sessionID, loaded); err != nil {
		return FollowUpPromotion{}, err
	}
	loaded.mu.Lock()
	running := loaded.activeRun != ""
	loaded.mu.Unlock()
	if !running {
		return FollowUpPromotion{}, ErrBusy
	}
	// Promotion removes prompts from the queue, so their drafts are reserved
	// durably first: if consuming them fails below, annotation recovery
	// finalizes the reservations of prompts the transcript accepted.
	reserved, err := m.reserveQueuedAnnotations(ctx, loaded, sessionID)
	if err != nil {
		return FollowUpPromotion{}, err
	}
	promoted, err := loaded.droid.Promote(ctx)
	if errors.Is(err, droids.ErrBusy) || errors.Is(err, droids.ErrConflict) {
		err = fmt.Errorf("%w: %v", ErrBusy, err)
	}
	loaded.mu.Lock()
	followUps := make([]queuedFollowUp, 0, len(promoted))
	for _, prompt := range promoted {
		followUp, known := loaded.takeQueuedFollowUpLocked(prompt.ID)
		if !known {
			followUp = followUpFromContent(prompt.Input.Content)
		}
		followUps = append(followUps, followUp)
	}
	queue := loaded.refreshFollowUpsLocked(ctx, sessionID, true)
	loaded.mu.Unlock()
	for _, followUp := range followUps {
		if followUp.annotationSubmission == "" || m.annotations == nil {
			continue
		}
		delete(reserved, followUp.annotationSubmission)
		if finalizeErr := m.annotations.FinalizeQueued(ctx, sessionID, followUp.annotationSubmission, followUp.input.AnnotationIDs); finalizeErr != nil {
			// The reservation stays and recovery finalizes it; the drafts
			// stay hidden meanwhile.
			m.annotations.UnpinQueued(sessionID, followUp.input.AnnotationIDs)
			err = errors.Join(err, fmt.Errorf("consume promoted annotations: %w", finalizeErr))
			continue
		}
		if acceptedMessageID, _ := loaded.droid.AnnotationSubmissionMessageID(ctx, followUp.annotationSubmission); acceptedMessageID != "" {
			m.AnnotationsSubmitted(sessionID, string(acceptedMessageID), followUp.input.AnnotationIDs)
		}
	}
	for submission := range reserved {
		// Still queued: its drafts return to the queued message. A failed
		// rollback is resolved by recovery, which rolls back reservations the
		// transcript never accepted.
		if rollbackErr := m.annotations.RollbackQueued(ctx, sessionID, submission); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("restore queued annotations: %w", rollbackErr))
		}
	}
	return FollowUpPromotion{Promoted: len(promoted), Queue: queue}, err
}

// reserveQueuedAnnotations durably reserves the drafts of every queued prompt
// that submits annotations and returns the reserved submissions. On failure it
// rolls back what it reserved.
func (m *Manager) reserveQueuedAnnotations(ctx context.Context, loaded *runtime, sessionID string) (map[string]struct{}, error) {
	reserved := map[string]struct{}{}
	if m.annotations == nil {
		return reserved, nil
	}
	loaded.mu.Lock()
	entries := slices.Clone(loaded.queuedFollowUps)
	loaded.mu.Unlock()
	for _, entry := range entries {
		followUp := entry.followUp
		if followUp.annotationSubmission == "" || len(followUp.input.AnnotationIDs) == 0 {
			continue
		}
		// ErrNotFound means the drafts are gone or still reserved by this
		// submission from an earlier attempt; finalizing or rolling back
		// handles both.
		if err := m.annotations.ReserveQueued(ctx, sessionID, followUp.annotationSubmission, followUp.input.AnnotationIDs); err != nil && !errors.Is(err, kitannotation.ErrNotFound) {
			for submission := range reserved {
				_ = m.annotations.RollbackQueued(ctx, sessionID, submission)
			}
			return nil, fmt.Errorf("reserve queued annotations: %w", err)
		}
		reserved[followUp.annotationSubmission] = struct{}{}
	}
	return reserved, nil
}

// consumeQueuedAnnotations removes the drafts an accepted queued prompt
// submitted and returns the message that accepted them, or "" when it
// submitted none. Drafts it fails to remove stay pinned.
func (m *Manager) consumeQueuedAnnotations(ctx context.Context, loaded *runtime, sessionID string, followUp queuedFollowUp) (string, error) {
	if followUp.annotationSubmission == "" || m.annotations == nil {
		return "", nil
	}
	messageID, err := loaded.droid.AnnotationSubmissionMessageID(ctx, followUp.annotationSubmission)
	if err != nil || messageID == "" {
		return "", err
	}
	if err := m.annotations.ConsumeQueued(ctx, sessionID, followUp.annotationSubmission, followUp.input.AnnotationIDs); err != nil {
		return "", err
	}
	return string(messageID), nil
}

// restoreFollowUpsLocked loads a reopened droid's queue and pins the
// annotations its prompts submit. A turn the queue started that Kit had not
// finished following before it stopped consumes the annotations its prompt
// submitted, as following it would have. The caller holds loaded.mu.
func (m *Manager) restoreFollowUpsLocked(ctx context.Context, loaded *runtime, sessionID string) {
	if started, err := loaded.droid.StartedPrompts(ctx); err == nil {
		ids := make([]droids.QueuedPromptID, 0, len(started))
		for _, prompt := range started {
			// A prompt whose drafts could not be removed stays for the next load.
			if _, err := m.consumeQueuedAnnotations(ctx, loaded, sessionID, followUpFromContent(prompt.Prompt.Input.Content)); err == nil {
				ids = append(ids, prompt.Prompt.ID)
			}
		}
		_ = loaded.droid.AcknowledgeStarted(ctx, ids...)
	}
	queue := loaded.refreshFollowUpsLocked(ctx, sessionID, false)
	if queue.Count == 0 || m.annotations == nil {
		return
	}
	for _, entry := range loaded.queuedFollowUps {
		if len(entry.followUp.input.AnnotationIDs) > 0 {
			// A draft that is already pinned stays with its earlier owner.
			_ = m.annotations.PinQueued(sessionID, entry.followUp.input.AnnotationIDs)
		}
	}
}

// startRestoredQueue starts a queue the droid restored after a restart, as
// Kit resumes an interrupted turn when the session loads.
func (m *Manager) startRestoredQueue(loaded *runtime, sessionID string) {
	loaded.mu.Lock()
	queued := loaded.followUps.Count > 0
	loaded.mu.Unlock()
	if !queued {
		return
	}
	_, err := loaded.droid.StartQueue(context.Background())
	if err != nil {
		loaded.mu.Lock()
		loaded.refreshFollowUpsLocked(context.Background(), sessionID, true)
		loaded.mu.Unlock()
	}
}

// afterTurnFollowUps announces the queue once a followed turn ends, which
// reports a prompt that could not start.
func (m *Manager) afterTurnFollowUps(loaded *runtime, sessionID string) {
	loaded.mu.Lock()
	loaded.refreshFollowUpsLocked(context.Background(), sessionID, true)
	loaded.mu.Unlock()
}

// queueTurnFeed hands the turns a droid starts to Kit in order, so Kit can
// follow those its queue started. The droid reports turns while holding its
// own authority, so the feed only records them and starts a follower.
type queueTurnFeed struct {
	mu       sync.Mutex
	pending  []droids.TurnID
	draining bool
	follow   func() bool
}

// started records a turn the droid started. It is the droid's TurnStarted
// callback and never blocks.
func (f *queueTurnFeed) started(id droids.TurnID) {
	f.mu.Lock()
	f.pending = append(f.pending, id)
	f.startLocked()
	f.mu.Unlock()
}

// bind sets the function that starts a follower for pending turns.
func (f *queueTurnFeed) bind(follow func() bool) {
	f.mu.Lock()
	f.follow = follow
	f.startLocked()
	f.mu.Unlock()
}

func (f *queueTurnFeed) startLocked() {
	if f.draining || f.follow == nil || len(f.pending) == 0 {
		return
	}
	if f.follow() {
		f.draining = true
	} else {
		f.pending = nil
	}
}

// next returns the next pending turn, or ends the follower when there is none.
func (f *queueTurnFeed) next() (droids.TurnID, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		f.draining = false
		return "", false
	}
	id := f.pending[0]
	f.pending = f.pending[1:]
	return id, true
}

// startFollowingQueueTurns starts the follower of loaded's pending turns. It
// reports false once the manager is shutting down.
func (m *Manager) startFollowingQueueTurns(loaded *runtime, sessionID string) bool {
	m.followMu.Lock()
	defer m.followMu.Unlock()
	if m.followClosed {
		return false
	}
	m.follows.Add(1)
	go m.followQueueTurns(loaded, sessionID)
	return true
}

func (m *Manager) followQueueTurns(loaded *runtime, sessionID string) {
	defer m.follows.Done()
	for {
		turnID, ok := loaded.queueTurns.next()
		if !ok {
			return
		}
		m.followQueueTurn(loaded, sessionID, turnID)
	}
}

// followQueueTurn follows a turn the droid started from its queue as Kit
// follows the turns it starts: it announces the turn's start and prompt and
// the queue without the prompt, then tracks the turn to its end. Turns Kit
// started itself are already followed. Admission is taken after any turn
// before it has been followed to its end.
func (m *Manager) followQueueTurn(loaded *runtime, sessionID string, turnID droids.TurnID) {
	// Only a turn the queue started needs following; checking first keeps the
	// follower from contending for admission with the turns Kit starts.
	handle, err := loaded.droid.Execution(context.Background(), turnID)
	if err != nil {
		return
	}
	if execution, err := handle.Snapshot(context.Background()); err != nil || execution.QueuedPromptID == "" {
		return
	}
	loaded.admissionMu.Lock()
	loaded.mu.Lock()
	release := func() {
		loaded.mu.Unlock()
		loaded.admissionMu.Unlock()
	}
	if loaded.runs[string(turnID)] != nil || m.sessionDeleting(sessionID) {
		release()
		return
	}
	ctx := context.Background()
	handle, err = loaded.droid.Execution(ctx, turnID)
	if err != nil {
		release()
		return
	}
	execution, err := handle.Snapshot(ctx)
	if err != nil || execution.QueuedPromptID == "" {
		release()
		return
	}
	followUp, known := loaded.takeQueuedFollowUpLocked(execution.QueuedPromptID)
	if !known {
		if started, err := loaded.droid.StartedPrompts(ctx); err == nil {
			for _, prompt := range started {
				if prompt.TurnID == turnID {
					followUp = followUpFromContent(prompt.Prompt.Input.Content)
				}
			}
		}
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	if err != nil {
		loaded.events.invalidate()
		release()
		return
	}
	_ = m.touchSessionActivity(ctx, sessionID, time.Now().UTC())
	initialEvents := []NewEvent{
		{SessionID: sessionID, TurnID: string(turnID), RunID: string(turnID), Kind: EventRunStarted, Status: RunStatusRunning},
		{SessionID: sessionID, TurnID: string(turnID), RunID: string(turnID), Kind: EventUserMessage, Text: boundedLiveText(livePromptText(followUp.input))},
	}
	acceptedMessageID, consumeErr := m.consumeQueuedAnnotations(ctx, loaded, sessionID, followUp)
	if acceptedMessageID != "" {
		initialEvents = append(initialEvents, NewEvent{
			SessionID: sessionID, Kind: EventAnnotationSubmitted,
			AnnotationIDs: append([]uint64(nil), followUp.input.AnnotationIDs...), AcceptedMessageID: acceptedMessageID,
		})
	}
	// The queue leaves the prompt in the same step that announces its turn,
	// so no snapshot shows it both queued and running.
	queue := loaded.refreshFollowUpsLocked(ctx, sessionID, false)
	initialEvents = append(initialEvents, NewEvent{SessionID: sessionID, Kind: EventFollowUpsChanged, FollowUps: &queue})
	// launchAdmittedRunLocked releases loaded.mu, and admission on failure.
	if _, err := m.launchAdmittedRunLocked(loaded, sessionID, handle, subscription, false, nil, initialEvents); err == nil && consumeErr == nil {
		// The prompt is followed and its drafts are consumed. Until this, a
		// session that reloads settles them again.
		_ = loaded.droid.AcknowledgeStarted(ctx, execution.QueuedPromptID)
	}
}

// unpinFollowUpsLocked returns the annotations of loaded's queued prompts to
// normal editing. The caller holds loaded.mu.
func (m *Manager) unpinFollowUpsLocked(loaded *runtime, sessionID string) {
	if m.annotations == nil {
		return
	}
	for _, entry := range loaded.queuedFollowUps {
		if len(entry.followUp.input.AnnotationIDs) > 0 {
			m.annotations.UnpinQueued(sessionID, entry.followUp.input.AnnotationIDs)
		}
	}
}
