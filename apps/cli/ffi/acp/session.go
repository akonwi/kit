package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/apps/cli/ffi/sessionbridge"
)

// requestTimeout bounds one session API call made outside a client request.
const requestTimeout = 30 * time.Second

// session is one Kit session open on the connection. It watches the
// session's events, translates them into session updates, and settles the
// prompt this connection is running in it.
type session struct {
	agent     *agent
	backend   Backend
	handle    SessionHandle
	id        string
	temporary bool

	// ctx ends when the session is detached.
	ctx    context.Context
	cancel context.CancelFunc
	stream UpdateStream
	// initial is the snapshot the session was opened with.
	initial  protocol.SessionSnapshot
	started  bool
	pumpDone chan struct{}

	// admission is held while a prompt starts a turn and while the pump
	// handles a batch, so the pump knows each turn this connection starts
	// before it handles that turn's events.
	admission sync.Mutex
	// output orders this session's updates, so a delayed tool output cannot
	// follow a later update.
	output sync.Mutex

	mu           sync.Mutex
	info         protocol.SessionInfo
	catalog      protocol.ModelCatalog
	commands     []protocol.PromptCommand
	mcpServers   []string
	translator   *translator
	prompt       *promptState
	interactions map[string]*presentation
	ended        error
	// followers holds the turns being followed through their own streams;
	// doneTurns holds the turns known to have ended.
	followers map[string]*follower
	doneTurns map[string]bool
	// closing is set when detach begins; workers then admits no more
	// goroutines, and detach waits for those it admitted.
	closing bool
	workers sync.WaitGroup
}

// spawnLocked runs work on a goroutine detach waits for, unless detach has
// begun. s.mu is held.
func (s *session) spawnLocked(work func()) bool {
	if s.closing {
		return false
	}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		work()
	}()
	return true
}

// presentation is an interaction the client is being asked to answer.
type presentation struct {
	turnID string
	cancel context.CancelFunc
}

// follower follows one turn through its own event stream. prompt is the
// session/prompt that started it, if this connection started it.
type follower struct {
	turnID string
	prompt *promptState
	// stop ends the current attempt to stream the turn, as when another
	// turn has started and this one must have ended.
	stop context.CancelFunc
}

// promptState is the session/prompt this connection is running.
type promptState struct {
	turnID    string
	cancelled bool
	settled   chan turnOutcome
}

// turnOutcome is how a turn ended, or why its end cannot be known.
type turnOutcome struct {
	status            protocol.TurnStatus
	kind              protocol.ProviderErrorKind
	message           string
	compactionFailure protocol.CompactionFailureReason
	err               error
}

func (p *promptState) settle(outcome turnOutcome) {
	select {
	case p.settled <- outcome:
	default:
	}
}

func newSession(a *agent, backend Backend, handle SessionHandle, id string, temporary bool) *session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{
		agent: a, backend: backend, handle: handle, id: id, temporary: temporary,
		ctx: ctx, cancel: cancel, pumpDone: make(chan struct{}),
		translator: newTranslator(id), interactions: map[string]*presentation{},
		followers: map[string]*follower{}, doneTurns: map[string]bool{},
	}
	capabilities := a.clientCapabilities()
	if capabilities.Session != nil {
		s.translator.compaction = advertised(capabilities.Session.Compaction)
		s.translator.notices = advertised(capabilities.Session.Notices)
	}
	return s
}

// open watches the session, checks it, and when replay is set sends its
// transcript as session updates.
func (s *session) open(ctx context.Context, replay bool, checks ...func(protocol.SessionInfo) error) error {
	stream, err := s.handle.Watch(s.ctx)
	if err != nil {
		return projectBackendError(err)
	}
	s.stream = stream
	var snapshot *protocol.SessionSnapshot
	select {
	case update, ok := <-stream.Updates():
		if !ok {
			err := stream.Err()
			if err == nil {
				err = errors.New("the session stream ended")
			}
			return projectBackendError(err)
		}
		snapshot = update.Snapshot
	case <-ctx.Done():
		return ctx.Err()
	}
	if snapshot == nil {
		return newError(codeInternalError, "the session stream did not start with a snapshot")
	}
	for _, check := range checks {
		if err := check(snapshot.Session); err != nil {
			return err
		}
	}
	catalog, err := s.backend.Models(ctx)
	if err != nil {
		return projectBackendError(err)
	}
	s.mu.Lock()
	s.catalog = catalog
	s.applySnapshot(*snapshot)
	for _, message := range snapshot.Messages {
		if message.TurnID != "" && message.TurnID != snapshot.ActiveTurnID {
			s.doneTurns[message.TurnID] = true
		}
	}
	if !replay {
		// The client already has the conversation so far.
		s.translator.reconcile(snapshot.Messages)
	}
	s.mu.Unlock()
	s.initial = *snapshot
	if !replay {
		return nil
	}
	return s.replayFrom(ctx, *snapshot)
}

// replay sends the session's current transcript again.
func (s *session) replay(ctx context.Context) error {
	snapshot, err := s.handle.Snapshot(ctx)
	if err != nil {
		return projectBackendError(err)
	}
	return s.replayFrom(ctx, snapshot)
}

func (s *session) replayFrom(ctx context.Context, snapshot protocol.SessionSnapshot) error {
	messages, err := transcript(ctx, s.handle, snapshot)
	if err != nil {
		return err
	}
	s.output.Lock()
	defer s.output.Unlock()
	s.mu.Lock()
	updates := s.translator.reconcile(messages)
	s.mu.Unlock()
	for _, update := range updates {
		s.send(update)
	}
	return nil
}

// applySnapshot records what a snapshot says about the session. s.mu is
// held.
func (s *session) applySnapshot(snapshot protocol.SessionSnapshot) {
	s.info = snapshot.Session
	s.commands = snapshot.PromptCommands
	s.translator.cwd = snapshot.Session.CWD
	if snapshot.Workspace != nil && snapshot.Workspace.CWD != "" {
		s.translator.cwd = snapshot.Workspace.CWD
	}
	s.translator.servers = s.translator.servers[:0]
	for _, server := range snapshot.MCPServers {
		s.translator.servers = append(s.translator.servers, server.Name)
	}
	s.translator.contextTokens, s.translator.contextWindow = snapshot.ContextTokens, snapshot.ContextWindow
	total := snapshot.Usage.Cost.Total
	s.translator.cost = &total
}

// start begins translating the session's events and presents the
// interactions it is waiting on.
func (s *session) start() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	go s.pump()
	if active := s.initial.ActiveTurnID; active != "" {
		s.followTurn(active, nil)
	}
	s.presentPending(s.initial.PendingInteractions)
}

// reportMCPServers notes the MCP servers a client asked for, which Kit does
// not connect to.
func (s *session) reportMCPServers(servers []mcpServer) {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		name := server.Name
		if name == "" {
			name = "(unnamed)"
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return
	}
	s.mu.Lock()
	s.mcpServers = names
	s.mu.Unlock()
	s.agent.logger.Warn("kit acp does not connect to MCP servers from the client; Kit uses the MCP servers configured in Kit",
		"session", s.id, "servers", strings.Join(names, ", "))
}

// announce sends what a client learns about a session after opening it:
// its commands, its context usage, and the MCP servers Kit did not connect.
func (s *session) announce() {
	s.output.Lock()
	defer s.output.Unlock()
	s.mu.Lock()
	commands := availableCommands(s.commands)
	usage := s.translator.usage()
	servers := s.mcpServers
	notices := s.translator.notices
	s.mu.Unlock()
	s.send(availableCommandsUpdate{SessionUpdate: "available_commands_update", AvailableCommands: commands})
	for _, update := range usage {
		s.send(update)
	}
	if notices && len(servers) > 0 {
		s.send(noticeUpdate{
			SessionUpdate: "notice", Severity: "warning",
			Title:       "Kit did not connect to this client's MCP servers",
			Description: "Kit uses the MCP servers configured in Kit, not " + strings.Join(servers, ", ") + ".",
		})
	}
}

func (s *session) send(update any) {
	if s.ctx.Err() != nil {
		return
	}
	_ = s.agent.conn.notify(methodSessionUpdate, sessionNotification{SessionID: s.id, Update: update})
}

func (s *session) configOptions() []configOption {
	s.mu.Lock()
	defer s.mu.Unlock()
	return configOptions(s.info, s.catalog)
}

// pump follows the session watch until it ends. The watch delivers the
// session's own events; each turn's events come from the turn's stream.
func (s *session) pump() {
	defer close(s.pumpDone)
	for update := range s.stream.Updates() {
		s.admission.Lock()
		if update.Snapshot != nil {
			s.resync(*update.Snapshot)
		} else {
			var sessionEvents []protocol.SessionEvent
			for _, event := range update.Events {
				if event.TurnID == "" {
					sessionEvents = append(sessionEvents, event)
				} else {
					s.followTurn(event.TurnID, nil)
				}
			}
			s.handleEvents(sessionEvents)
		}
		s.admission.Unlock()
	}
	err := s.stream.Err()
	if s.ctx.Err() != nil {
		return
	}
	if err == nil {
		err = errors.New("the session stream ended")
	}
	s.agent.logger.Warn("session stream ended", "session", s.id, "err", err)
	// A running prompt is left to its turn's follower, which streams the
	// turn on its own and settles the prompt after the turn's last update.
	s.mu.Lock()
	s.ended = err
	s.mu.Unlock()
}

// followTurn follows a turn that has not ended through its own stream, which
// replays it from its start. prompt, when set, is settled when it ends.
func (s *session) followTurn(turnID string, prompt *promptState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.followers[turnID]; existing != nil {
		if prompt != nil {
			existing.prompt = prompt
		}
		return
	}
	if s.doneTurns[turnID] || s.closing {
		return
	}
	// A session runs one turn at a time, so a turn still followed has ended.
	s.stopFollowersLocked(turnID)
	following := &follower{turnID: turnID, prompt: prompt, stop: func() {}}
	s.followers[turnID] = following
	s.spawnLocked(func() { s.follow(following) })
}

// follow translates a turn's events until it ends. A lost stream is followed
// again while the turn runs; a turn that ended meanwhile is caught up from a
// snapshot and settled from its status.
func (s *session) follow(following *follower) {
	defer func() {
		s.mu.Lock()
		delete(s.followers, following.turnID)
		s.mu.Unlock()
	}()
	streamID, after := "", int64(0)
	for failures := 0; s.ctx.Err() == nil; {
		attempt, stop := context.WithCancel(s.ctx)
		s.mu.Lock()
		following.stop = stop
		s.mu.Unlock()
		stream, err := s.handle.StreamTurn(attempt, following.turnID)
		if err == nil {
			for batch := range stream.Updates() {
				var fresh []protocol.SessionEvent
				for _, event := range batch {
					if event.TurnID != following.turnID || event.StreamID == streamID && event.Sequence <= after {
						continue
					}
					streamID, after = event.StreamID, event.Sequence
					fresh = append(fresh, event)
				}
				s.handleEvents(fresh)
				failures = 0
			}
			err = stream.Err()
		}
		stop()
		if s.turnDone(following.turnID) || s.ctx.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(s.ctx, requestTimeout)
		info, statusErr := s.handle.Turn(ctx, following.turnID)
		cancel()
		if statusErr == nil && info.Status != protocol.TurnStatusRunning && info.Status != protocol.TurnStatusQueued {
			s.catchUp(following.turnID)
			s.finishTurn(following.turnID, turnOutcome{status: info.Status, message: info.ErrorMessage, compactionFailure: info.CompactionFailureReason})
			return
		}
		failures++
		if failures > maxFollowFailures {
			if statusErr != nil {
				err = statusErr
			}
			s.agent.logger.Warn("lost a turn's events", "session", s.id, "turn", following.turnID, "err", err)
			s.finishTurn(following.turnID, turnOutcome{err: err})
			return
		}
		select {
		case <-time.After(followDelay(failures)):
		case <-s.ctx.Done():
		}
	}
}

// stopFollowersLocked stops streaming every followed turn but turnID. s.mu
// is held.
func (s *session) stopFollowersLocked(turnID string) {
	for id, following := range s.followers {
		if id != turnID {
			following.stop()
		}
	}
}

// maxFollowFailures bounds the attempts to follow a turn whose stream keeps
// failing.
const maxFollowFailures = 8

// followDelay backs off attempts to follow a turn: 100ms doubling to 1.6s.
func followDelay(failures int) time.Duration {
	return min(100*time.Millisecond<<min(failures-1, 4), 1600*time.Millisecond)
}

func (s *session) turnDone(turnID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doneTurns[turnID]
}

// catchUp sends what the client missed of a turn whose stream was lost,
// from the session's transcript.
func (s *session) catchUp(turnID string) {
	ctx, cancel := context.WithTimeout(s.ctx, requestTimeout)
	snapshot, err := s.handle.Snapshot(ctx)
	cancel()
	if err != nil {
		s.agent.logger.Warn("catch up a turn", "session", s.id, "turn", turnID, "err", err)
		return
	}
	var messages []protocol.TranscriptMessage
	for _, message := range snapshot.Messages {
		if message.TurnID == turnID {
			messages = append(messages, message)
		}
	}
	s.output.Lock()
	defer s.output.Unlock()
	s.mu.Lock()
	updates := s.translator.reconcile(messages)
	s.mu.Unlock()
	for _, update := range updates {
		s.send(update)
	}
}

// finishTurn records that a turn ended and settles its prompt.
func (s *session) finishTurn(turnID string, outcome turnOutcome) {
	s.output.Lock()
	defer s.output.Unlock()
	s.mu.Lock()
	settle := s.endTurnLocked(turnID)
	s.mu.Unlock()
	if settle != nil {
		settle.settle(outcome)
	}
}

// endTurnLocked records that a turn ended and returns its prompt. s.mu is
// held.
func (s *session) endTurnLocked(turnID string) *promptState {
	if s.doneTurns[turnID] {
		return nil
	}
	s.doneTurns[turnID] = true
	s.translator.endTurn()
	for id, presented := range s.interactions {
		if presented.turnID == turnID {
			presented.cancel()
			delete(s.interactions, id)
		}
	}
	if following := s.followers[turnID]; following != nil {
		return following.prompt
	}
	return nil
}

func (s *session) handleEvents(events []protocol.SessionEvent) {
	if len(events) == 0 {
		return
	}
	s.output.Lock()
	defer s.output.Unlock()
	for _, event := range events {
		s.agent.logger.Debug("session event", "session", s.id, "kind", event.Kind(), "turn", event.TurnID, "sequence", event.Sequence)
		s.mu.Lock()
		translated := s.translator.event(event, time.Now())
		if translated.flushIn > 0 {
			s.scheduleFlush(translated.flushFor, translated.flushIn)
		}
		var settle *promptState
		var outcome turnOutcome
		var interaction *protocol.InteractionRequest
		var configuration []configOption
		switch payload := event.Payload.(type) {
		case protocol.TurnCompletedEvent:
			settle = s.endTurnLocked(event.TurnID)
			outcome = turnOutcome{
				status: payload.Status, kind: payload.ErrorKind, message: payload.ErrorMessage,
				compactionFailure: payload.CompactionFailureReason,
			}
		case protocol.InteractionRequestedEvent:
			if _, presented := s.interactions[payload.Interaction.ID]; !presented {
				interaction = payload.Interaction
			}
		case protocol.InteractionResolvedEvent:
			if presented := s.interactions[payload.InteractionID]; presented != nil {
				presented.cancel()
				delete(s.interactions, payload.InteractionID)
			}
		case protocol.SessionConfigurationChangedEvent:
			s.info.Model = payload.Configuration.Model
			s.info.ThinkingLevel = payload.Configuration.ThinkingLevel
			s.info.Inputs = payload.Configuration.Inputs
			s.info.ConfigurationRevision = payload.Configuration.ConfigurationRevision
			configuration = configOptions(s.info, s.catalog)
		case protocol.SessionNameChangedEvent:
			s.info.Name = payload.SessionName
		}
		s.mu.Unlock()
		for _, update := range translated.updates {
			s.send(update)
		}
		if configuration != nil {
			s.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: configuration})
		}
		if interaction != nil {
			s.startInteraction(*interaction)
		}
		if settle != nil {
			settle.settle(outcome)
		}
	}
}

// scheduleFlush sends a running tool's output after delay. s.mu is held.
func (s *session) scheduleFlush(toolCallID string, delay time.Duration) {
	tool := s.translator.tools[toolCallID]
	if tool == nil || tool.timer != nil {
		return
	}
	tool.timer = time.AfterFunc(delay, func() {
		s.output.Lock()
		defer s.output.Unlock()
		s.mu.Lock()
		tool.timer = nil
		update, ok := s.translator.flushOutput(toolCallID, time.Now())
		s.mu.Unlock()
		if ok && s.ctx.Err() == nil {
			s.send(update)
		}
	})
}

// resync applies a replacement snapshot after the watch lost its place,
// which happens when a turn starts a new event stream, and follows the
// active turn.
func (s *session) resync(snapshot protocol.SessionSnapshot) {
	s.output.Lock()
	s.mu.Lock()
	previousCommands := s.commands
	previousName := s.info.Name
	previousConfiguration := configOptions(s.info, s.catalog)
	s.applySnapshot(snapshot)
	commandsChanged := !slices.Equal(previousCommands, s.commands)
	configuration := configOptions(s.info, s.catalog)
	s.mu.Unlock()
	if commandsChanged {
		s.send(availableCommandsUpdate{SessionUpdate: "available_commands_update", AvailableCommands: availableCommands(snapshot.PromptCommands)})
	}
	if !configOptionsEqual(previousConfiguration, configuration) {
		s.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: configuration})
	}
	if snapshot.Session.Name != previousName && snapshot.Session.Name != "" {
		s.send(sessionInfoUpdate{SessionUpdate: "session_info_update", Title: snapshot.Session.Name})
	}
	s.output.Unlock()
	if snapshot.ActiveTurnID != "" {
		s.followTurn(snapshot.ActiveTurnID, nil)
	}
	// A followed turn the snapshot no longer shows as active has ended; its
	// own stream ends after delivering the turn's last events.
	s.presentPending(snapshot.PendingInteractions)
}

// presentPending presents the pending interactions a snapshot lists, except
// those of a followed turn, whose stream presents them in order.
func (s *session) presentPending(requests []protocol.InteractionRequest) {
	for _, request := range requests {
		s.mu.Lock()
		followed := request.TurnID != "" && s.followers[request.TurnID] != nil
		s.mu.Unlock()
		if !followed {
			s.startInteraction(request)
		}
	}
}

func configOptionsEqual(left, right []configOption) bool {
	return fmt.Sprintf("%#v", left) == fmt.Sprintf("%#v", right)
}

func (s *session) startInteraction(request protocol.InteractionRequest) {
	s.mu.Lock()
	if _, presented := s.interactions[request.ID]; presented || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	if request.TurnID != "" && s.doneTurns[request.TurnID] {
		s.mu.Unlock()
		return
	}
	s.agent.logger.Debug("present interaction", "session", s.id, "interaction", request.ID, "turn", request.TurnID, "followed", s.followers[request.TurnID] != nil)
	ctx, cancel := context.WithCancel(s.ctx)
	if !s.spawnLocked(func() {
		defer cancel()
		s.presentInteraction(ctx, request)
	}) {
		s.mu.Unlock()
		cancel()
		return
	}
	s.interactions[request.ID] = &presentation{turnID: request.TurnID, cancel: cancel}
	s.mu.Unlock()
}

// acceptPrompt registers a prompt in arrival order, so a session/cancel that
// follows it applies to it even before its turn starts.
func (s *session) acceptPrompt(blocks []contentBlock) (acceptance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended != nil {
		return acceptance{}, newError(codeInternalError, "the session is no longer available: %v", s.ended)
	}
	if s.prompt != nil {
		return acceptance{}, newError(codeInvalidRequest, "a prompt is already running in this session")
	}
	prompt := &promptState{settled: make(chan turnOutcome, 1)}
	s.prompt = prompt
	commands := slices.Clone(s.commands)
	inputs := slices.Clone(s.info.Inputs)
	return acceptance{
		run: func(ctx context.Context) (any, error) {
			defer func() {
				s.mu.Lock()
				if s.prompt == prompt {
					s.prompt = nil
				}
				s.mu.Unlock()
			}()
			return s.runPrompt(ctx, prompt, blocks, commands, inputs)
		},
		cancel: s.cancelPrompt,
	}, nil
}

func (s *session) runPrompt(ctx context.Context, prompt *promptState, blocks []contentBlock, commands []protocol.PromptCommand, inputs []protocol.ModelInputKind) (any, error) {
	request, err := preparePrompt(ctx, s.handle, blocks, commands, inputs)
	if err != nil {
		return nil, err
	}
	if s.promptCancelled(prompt) {
		return promptResult{StopReason: stopReasonCancelled}, nil
	}
	if request.command == compactCommand {
		return s.compact(ctx)
	}
	s.admission.Lock()
	if s.followingAnotherTurn() {
		s.admission.Unlock()
		return nil, errSessionBusy
	}
	startContext, cancel := context.WithTimeout(ctx, requestTimeout)
	var turnID string
	if request.command != "" {
		turnID, err = s.handle.StartPromptCommand(startContext, request.command, request.args)
	} else {
		turnID, err = s.handle.StartPrompt(startContext, request.input)
	}
	cancel()
	if err != nil {
		s.admission.Unlock()
		if serverErrorCode(err) == kit.ErrorConflict {
			return nil, errSessionBusy
		}
		return nil, projectBackendError(err)
	}
	s.mu.Lock()
	prompt.turnID = turnID
	s.translator.userTurns[turnID] = true
	cancelled := prompt.cancelled
	if cancelled {
		s.spawnLocked(func() { s.abort(turnID) })
	}
	s.mu.Unlock()
	s.followTurn(turnID, prompt)
	s.admission.Unlock()
	select {
	case outcome := <-prompt.settled:
		return s.promptOutcome(prompt, outcome)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// errSessionBusy answers a prompt while another client's turn runs.
var errSessionBusy = &rpcError{Code: codeInternalError, Message: "the session is busy: another Kit client is running a turn in it"}

// followingAnotherTurn reports a running turn this connection did not start.
func (s *session) followingAnotherTurn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for turnID := range s.followers {
		if !s.doneTurns[turnID] {
			return true
		}
	}
	return false
}

func (s *session) promptCancelled(prompt *promptState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return prompt.cancelled
}

func (s *session) promptOutcome(prompt *promptState, outcome turnOutcome) (any, error) {
	if outcome.err != nil {
		if s.promptCancelled(prompt) {
			return promptResult{StopReason: stopReasonCancelled}, nil
		}
		return nil, newError(codeInternalError, "lost track of the turn: %v", outcome.err)
	}
	switch outcome.status {
	case protocol.TurnStatusCompleted:
		return promptResult{StopReason: stopReasonEndTurn}, nil
	case protocol.TurnStatusAborted, protocol.TurnStatusInterrupted:
		return promptResult{StopReason: stopReasonCancelled}, nil
	}
	if s.promptCancelled(prompt) {
		return promptResult{StopReason: stopReasonCancelled}, nil
	}
	message := outcome.message
	if outcome.compactionFailure != "" {
		message = sessionbridge.CompactionFailedTurnText(string(outcome.compactionFailure))
	}
	if message == "" {
		message = "the turn " + string(outcome.status)
	}
	failure := &rpcError{Code: codeInternalError, Message: message}
	if outcome.kind != "" {
		failure.Data = map[string]string{"errorKind": string(outcome.kind)}
	}
	if outcome.kind == protocol.ProviderErrorAuthentication {
		failure.Code = codeAuthRequired
	}
	return nil, failure
}

func (s *session) compact(ctx context.Context) (any, error) {
	token := make([]byte, 8)
	_, _ = rand.Read(token)
	_, err := s.handle.Compact(ctx, protocol.CompactSessionInput{OperationID: "acp_compact_" + hex.EncodeToString(token)})
	if err != nil {
		if reason, ok := kit.CompactionFailure(err); ok {
			return nil, newError(codeInternalError, "%s", sessionbridge.CompactionFailedTurnText(string(reason)))
		}
		return nil, projectBackendError(err)
	}
	return promptResult{StopReason: stopReasonEndTurn}, nil
}

// cancelPrompt cancels the prompt this connection is running in the
// session, aborting its turn once it has one.
func (s *session) cancelPrompt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if turnID := s.markPromptCancelledLocked(); turnID != "" {
		s.spawnLocked(func() { s.abort(turnID) })
	}
}

// markPromptCancelledLocked marks this connection's prompt cancelled and
// returns its turn, if it has started one. s.mu is held.
func (s *session) markPromptCancelledLocked() string {
	if s.prompt == nil {
		return ""
	}
	s.prompt.cancelled = true
	return s.prompt.turnID
}

// cancelAndAbort cancels this connection's prompt and waits for its turn to
// be aborted, as session/close requires before the session is detached.
func (s *session) cancelAndAbort() {
	s.mu.Lock()
	turnID := s.markPromptCancelledLocked()
	s.mu.Unlock()
	if turnID != "" {
		s.abort(turnID)
	}
}

func (s *session) abort(turnID string) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := s.handle.Abort(ctx, turnID); err != nil {
		switch serverErrorCode(err) {
		case kit.ErrorConflict, kit.ErrorNotFound:
		default:
			s.agent.logger.Warn("abort turn", "session", s.id, "turn", turnID, "err", err)
		}
	}
}

// detach stops watching the session. A prompt still waiting is answered as
// cancelled.
func (s *session) detach() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.cancel()
	if s.stream != nil {
		_ = s.stream.Close()
		<-s.pumpDoneOrClosed()
	}
	s.workers.Wait()
	s.output.Lock()
	s.mu.Lock()
	s.translator.endTurn()
	for id, presented := range s.interactions {
		presented.cancel()
		delete(s.interactions, id)
	}
	prompt := s.prompt
	s.mu.Unlock()
	s.output.Unlock()
	if prompt != nil {
		prompt.settle(turnOutcome{status: protocol.TurnStatusAborted})
	}
}

// pumpDoneOrClosed is closed when the pump has finished or never started.
func (s *session) pumpDoneOrClosed() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.pumpDone
}

// setConfigOption configures the session's model or thinking level and
// returns every option.
func (s *session) setConfigOption(ctx context.Context, id, value string) ([]configOption, error) {
	for attempt := 0; ; attempt++ {
		s.mu.Lock()
		info := s.info
		options := configOptions(info, s.catalog)
		s.mu.Unlock()
		if !hasOption(options, id, value) {
			return nil, invalidParams("%q is not a value of config option %q", value, id)
		}
		if id == configOptionThinking && value == currentValue(options, id) {
			return options, nil
		}
		input := protocol.ConfigureSessionInput{ExpectedRevision: info.ConfigurationRevision, Model: info.Model}
		if id == configOptionModel {
			input.Model = value
		} else {
			level := protocol.ThinkingLevel(value)
			input.ThinkingLevel = &level
		}
		result, err := s.handle.Configure(ctx, input)
		if err == nil {
			s.mu.Lock()
			s.info = result.Session
			options = configOptions(s.info, s.catalog)
			s.mu.Unlock()
			return options, nil
		}
		if serverErrorCode(err) != kit.ErrorConflict || attempt > 0 {
			if reason, ok := kit.CompactionFailure(err); ok {
				return nil, newError(codeInternalError, "%s", sessionbridge.CompactionFailedTurnText(string(reason)))
			}
			return nil, projectBackendError(err)
		}
		snapshot, snapshotErr := s.handle.Snapshot(ctx)
		if snapshotErr != nil {
			return nil, projectBackendError(err)
		}
		s.mu.Lock()
		s.info = snapshot.Session
		s.mu.Unlock()
	}
}
