package tui

import (
	"context"
	"errors"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

func (o Options) listSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	if o.Client != nil {
		return o.Client.ListSessions(ctx, kit.ListSessionsOptions{CWD: cwd})
	}
	return o.Server.ListSessions(ctx, cwd)
}

func (o Options) createSession(ctx context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	if o.Client != nil {
		return o.Client.CreateSession(ctx, input)
	}
	return o.Server.CreateSession(ctx, input)
}

func (o Options) forkSession(ctx context.Context, sourceID string, input protocol.ForkSessionInput) (protocol.SessionInfo, error) {
	if o.Client != nil {
		return o.Client.ForkSession(ctx, sourceID, input)
	}
	return o.Server.ForkSession(ctx, sourceID, input)
}

func (o Options) renameSession(ctx context.Context, sessionID, name string) (protocol.SessionInfo, error) {
	if o.Client != nil {
		return o.Client.RenameSession(ctx, sessionID, name)
	}
	return o.Server.RenameSession(ctx, sessionID, name)
}

func (o Options) deleteSession(ctx context.Context, sessionID string) error {
	if o.Client != nil {
		return o.Client.DeleteSession(ctx, sessionID)
	}
	return o.Server.DeleteSession(ctx, sessionID)
}

func (o Options) models(ctx context.Context) (protocol.ModelCatalog, error) {
	if o.Client != nil {
		return o.Client.Models(ctx)
	}
	return o.Server.Models(ctx)
}

func (o Options) refreshModels(ctx context.Context) (protocol.ModelCatalog, error) {
	if o.Client != nil {
		return o.Client.RefreshModels(ctx)
	}
	refresher, ok := o.Server.(ModelCatalogRefresher)
	if !ok {
		return protocol.ModelCatalog{}, errors.New("model catalog refresh is unavailable")
	}
	return refresher.RefreshModels(ctx)
}

func (o Options) attach(ctx context.Context, sessionID string) (boundSession, error) {
	if o.Client != nil {
		return o.Client.Attach(ctx, sessionID)
	}
	return o.Server.Attach(ctx, sessionID)
}

func (o Options) probeCompatibility(ctx context.Context) error {
	if o.Client != nil {
		return o.Client.ProbeCompatibility(ctx)
	}
	probe, ok := o.Server.(CompatibilityProber)
	if !ok {
		return errors.New("server compatibility probe is unavailable")
	}
	return probe.ProbeCompatibility(ctx)
}

// boundSession is the direct operation subset shared by public sessions and
// local TUI test doubles. Handle-producing operations use the helpers below so
// production keeps the concrete public handles without a forwarding wrapper.
type boundSession interface {
	ID() string
	Snapshot(context.Context) (protocol.SessionSnapshot, error)
	VCSStatus(context.Context) (protocol.SessionVCSStatus, error)
	WatchVCS(context.Context, func(protocol.SessionVCSStatus)) error
	FileIndex(context.Context) (protocol.SessionFileIndex, error)
	ChangeCWD(context.Context, string) (protocol.SessionInfo, error)
	Reload(context.Context) (protocol.ReloadSessionResult, error)
	Configure(context.Context, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	Compact(context.Context, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	Turn(context.Context, string) (protocol.TurnInfo, error)
	AbortBash(context.Context, string) error
	Abort(context.Context, string) error
	Subagent(context.Context, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error)
	SubagentTranscript(context.Context, string, string) (protocol.SubagentTranscript, error)
}

var _ boundSession = (*kit.Session)(nil)

func streamTurn(ctx context.Context, bound boundSession, turnID string) (EventStream, error) {
	if public, ok := bound.(*kit.Session); ok {
		return public.Stream(ctx, turnID)
	}
	return bound.(interface {
		Stream(context.Context, string) (EventStream, error)
	}).Stream(ctx, turnID)
}

func startPrompt(ctx context.Context, bound boundSession, text string) (Turn, error) {
	if public, ok := bound.(*kit.Session); ok {
		return public.StartPrompt(ctx, text)
	}
	return bound.(interface {
		StartPrompt(context.Context, string) (Turn, error)
	}).StartPrompt(ctx, text)
}

func supportsStructuredPrompts(bound boundSession) bool {
	if _, ok := bound.(*kit.Session); ok {
		return true
	}
	_, ok := bound.(StructuredPromptSession)
	return ok
}

func submitPromptInput(ctx context.Context, bound boundSession, input protocol.PromptInput) (PromptSubmission, error) {
	if public, ok := bound.(*kit.Session); ok {
		result, err := public.SubmitPromptInput(ctx, input)
		return PromptSubmission{Turn: result.Turn, Queued: result.Queued, Queue: result.Queue}, err
	}
	return bound.(StructuredPromptSession).SubmitPromptInput(ctx, input)
}

func supportsFollowUps(bound boundSession) bool {
	if _, ok := bound.(*kit.Session); ok {
		return true
	}
	_, ok := bound.(FollowUpSession)
	return ok
}

func submitFollowUp(ctx context.Context, bound boundSession, text string) (PromptSubmission, error) {
	if public, ok := bound.(*kit.Session); ok {
		result, err := public.SubmitPrompt(ctx, text)
		return PromptSubmission{Turn: result.Turn, Queued: result.Queued, Queue: result.Queue}, err
	}
	return bound.(FollowUpSession).SubmitPrompt(ctx, text)
}

func restoreFollowUps(ctx context.Context, bound boundSession) (protocol.RestoreFollowUpsResult, error) {
	if public, ok := bound.(*kit.Session); ok {
		return public.RestoreFollowUps(ctx)
	}
	return bound.(FollowUpSession).RestoreFollowUps(ctx)
}

func promoteFollowUps(ctx context.Context, bound boundSession) (protocol.PromoteFollowUpsResult, error) {
	if public, ok := bound.(*kit.Session); ok {
		return public.PromoteFollowUps(ctx)
	}
	return bound.(FollowUpSession).PromoteFollowUps(ctx)
}

func submitPromptCommand(ctx context.Context, bound boundSession, name, args string) (PromptSubmission, error) {
	if public, ok := bound.(*kit.Session); ok {
		result, err := public.SubmitPromptCommand(ctx, name, args)
		return PromptSubmission{Turn: result.Turn, Queued: result.Queued, Queue: result.Queue}, err
	}
	if submitter, ok := bound.(interface {
		SubmitPromptCommand(context.Context, string, string) (PromptSubmission, error)
	}); ok {
		return submitter.SubmitPromptCommand(ctx, name, args)
	}
	turn, err := bound.(interface {
		StartPromptCommand(context.Context, string, string) (Turn, error)
	}).StartPromptCommand(ctx, name, args)
	return PromptSubmission{Turn: turn}, err
}

func lookupBash(ctx context.Context, bound boundSession, executionID string) (BashExecution, error) {
	if public, ok := bound.(*kit.Session); ok {
		return public.Bash(ctx, executionID)
	}
	return bound.(interface {
		Bash(context.Context, string) (BashExecution, error)
	}).Bash(ctx, executionID)
}

func startBash(ctx context.Context, bound boundSession, executionID, command string, exclude bool) (BashExecution, error) {
	if public, ok := bound.(*kit.Session); ok {
		return public.StartBash(ctx, executionID, command, exclude)
	}
	return bound.(interface {
		StartBash(context.Context, string, string, bool) (BashExecution, error)
	}).StartBash(ctx, executionID, command, exclude)
}
