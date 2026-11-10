package acp

import (
	"context"
	"errors"
	"io"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// Backend is the part of the Kit session API that kit acp uses. The public Go
// client implements it through ClientBackend; tests may substitute it.
type Backend interface {
	Models(ctx context.Context) (protocol.ModelCatalog, error)
	ListSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error)
	CreateSession(ctx context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error)
	DisposeTemporarySession(ctx context.Context, sessionID string) error
	Attach(ctx context.Context, sessionID string) (SessionHandle, error)
}

// SessionHandle is one attached Kit session.
type SessionHandle interface {
	Snapshot(ctx context.Context) (protocol.SessionSnapshot, error)
	Watch(ctx context.Context) (UpdateStream, error)
	// StreamTurn follows one turn's events from its start until it ends.
	StreamTurn(ctx context.Context, turnID string) (TurnStream, error)
	TranscriptPage(ctx context.Context, before string) (protocol.TranscriptPage, error)
	StartPrompt(ctx context.Context, input protocol.PromptInput) (string, error)
	StartPromptCommand(ctx context.Context, name, args string) (string, error)
	Turn(ctx context.Context, turnID string) (protocol.TurnInfo, error)
	Abort(ctx context.Context, turnID string) error
	RespondInteraction(ctx context.Context, response protocol.InteractionResponse) error
	Configure(ctx context.Context, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	Compact(ctx context.Context, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	UploadAttachment(ctx context.Context, filename string, content io.Reader) (protocol.AttachmentInfo, error)
}

// UpdateStream delivers a watched session's snapshots and event batches.
type UpdateStream interface {
	Updates() <-chan kit.SessionUpdate
	Err() error
	Close() error
}

// TurnStream delivers one turn's event batches. Updates closes when the turn
// ends or the stream is lost; Err then reports which.
type TurnStream interface {
	Updates() <-chan []protocol.SessionEvent
	Err() error
}

// ClientBackend adapts the public Go client to Backend.
type ClientBackend struct{ Client *kit.Client }

// Models returns the server's model catalog.
func (b ClientBackend) Models(ctx context.Context) (protocol.ModelCatalog, error) {
	return b.Client.Models(ctx)
}

// ListSessions lists persisted sessions, filtered by cwd when it is set.
func (b ClientBackend) ListSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	return b.Client.ListSessions(ctx, kit.ListSessionsOptions{CWD: cwd})
}

// CreateSession creates a session.
func (b ClientBackend) CreateSession(ctx context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	return b.Client.CreateSession(ctx, input)
}

// DisposeTemporarySession disposes a temporary session.
func (b ClientBackend) DisposeTemporarySession(ctx context.Context, sessionID string) error {
	return b.Client.DisposeTemporarySession(ctx, sessionID)
}

// Attach attaches to a session.
func (b ClientBackend) Attach(ctx context.Context, sessionID string) (SessionHandle, error) {
	session, err := b.Client.Attach(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return clientSession{session}, nil
}

type clientSession struct{ session *kit.Session }

func (s clientSession) Snapshot(ctx context.Context) (protocol.SessionSnapshot, error) {
	return s.session.Snapshot(ctx)
}

func (s clientSession) Watch(ctx context.Context) (UpdateStream, error) {
	stream, err := s.session.Watch(ctx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s clientSession) StreamTurn(ctx context.Context, turnID string) (TurnStream, error) {
	stream, err := s.session.Stream(ctx, turnID)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s clientSession) TranscriptPage(ctx context.Context, before string) (protocol.TranscriptPage, error) {
	return s.session.TranscriptPage(ctx, before)
}

func (s clientSession) StartPrompt(ctx context.Context, input protocol.PromptInput) (string, error) {
	turn, err := s.session.StartPromptInput(ctx, input)
	if err != nil {
		return "", err
	}
	return turn.ID(), nil
}

func (s clientSession) StartPromptCommand(ctx context.Context, name, args string) (string, error) {
	turn, err := s.session.StartPromptCommand(ctx, name, args)
	if err != nil {
		return "", err
	}
	return turn.ID(), nil
}

func (s clientSession) Turn(ctx context.Context, turnID string) (protocol.TurnInfo, error) {
	return s.session.Turn(ctx, turnID)
}

func (s clientSession) Abort(ctx context.Context, turnID string) error {
	return s.session.Abort(ctx, turnID)
}

func (s clientSession) RespondInteraction(ctx context.Context, response protocol.InteractionResponse) error {
	return s.session.RespondInteraction(ctx, response)
}

func (s clientSession) Configure(ctx context.Context, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	return s.session.Configure(ctx, input)
}

func (s clientSession) Compact(ctx context.Context, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	return s.session.Compact(ctx, input)
}

func (s clientSession) UploadAttachment(ctx context.Context, filename string, content io.Reader) (protocol.AttachmentInfo, error) {
	return s.session.UploadAttachment(ctx, filename, content)
}

// serverErrorCode returns the session API error code of err, if it has one.
func serverErrorCode(err error) kit.ErrorCode {
	var failure *kit.ServerError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

// projectBackendError maps a session API failure onto an ACP error.
func projectBackendError(err error) error {
	var failure *rpcError
	if errors.As(err, &failure) {
		return failure
	}
	// The client shows the server's own message, which says what failed.
	message := err.Error()
	var server *kit.ServerError
	if errors.As(err, &server) && server.Message != "" {
		message = server.Message
	}
	switch serverErrorCode(err) {
	case kit.ErrorInvalidRequest, kit.ErrorUnprocessable, kit.ErrorLimitExceeded:
		return &rpcError{Code: codeInvalidParams, Message: message}
	case kit.ErrorUnauthorized:
		return &rpcError{Code: codeAuthRequired, Message: message}
	}
	return &rpcError{Code: codeInternalError, Message: message}
}
