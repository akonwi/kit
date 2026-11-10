package acp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// Options configures one ACP connection.
type Options struct {
	// In and Out carry the protocol. Out receives protocol messages only.
	In  io.Reader
	Out io.Writer
	// Logger receives diagnostics; it must not write to Out.
	Logger *slog.Logger
	// Connect reaches the Kit server. It is called in the background when the
	// connection starts and again by a later request after a failure.
	Connect func(context.Context) (Backend, error)
	// Temporary makes session/new create temporary sessions, disposed when
	// they are closed or the connection ends.
	Temporary bool
	// NewSessionID returns a new Kit session ID for a temporary session.
	NewSessionID func() (string, error)
	// Version is Kit's version, reported in agentInfo.
	Version string
}

// Serve runs one ACP connection until In ends or ctx is cancelled. Sessions
// are detached when it returns; their running turns keep running, and
// temporary sessions are disposed.
func Serve(ctx context.Context, options Options) error {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	a := &agent{
		options:  options,
		logger:   logger,
		backend:  &lazyBackend{connect: options.Connect},
		sessions: map[string]*session{},
	}
	a.conn = newConn(options.In, options.Out, logger)
	a.conn.handler = a
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.backend.ctx = ctx
	a.backend.start()
	err := a.conn.serve(ctx)
	a.closeAll()
	return err
}

type agent struct {
	options Options
	logger  *slog.Logger
	conn    *conn
	backend *lazyBackend

	mu           sync.Mutex
	initialized  bool
	capabilities clientCapabilities
	sessions     map[string]*session
	opening      map[string]bool
}

const pageSize = 100

func (a *agent) accept(method string, params json.RawMessage) (acceptance, error) {
	a.mu.Lock()
	initialized := a.initialized
	a.mu.Unlock()
	if method == methodInitialize {
		return a.acceptInitialize(params)
	}
	if !initialized {
		return acceptance{}, newError(codeInvalidRequest, "the connection is not initialized")
	}
	switch method {
	case methodAuthenticate:
		return acceptance{}, invalidParams("Kit offers no authentication methods; log in with `kit auth` or the TUI's /login")
	case methodSessionNew:
		var input newSessionParams
		if err := decodeParams(params, &input); err != nil {
			return acceptance{}, err
		}
		if err := requireAbsolute("cwd", input.CWD); err != nil {
			return acceptance{}, err
		}
		var opened *session
		return acceptance{
			run: func(ctx context.Context) (any, error) {
				result, created, err := a.newSession(ctx, input)
				opened = created
				return result, err
			},
			after: func() { opened.announce() },
		}, nil
	case methodSessionLoad, methodSessionResume:
		var input loadSessionParams
		if err := decodeParams(params, &input); err != nil {
			return acceptance{}, err
		}
		if input.SessionID == "" {
			return acceptance{}, invalidParams("sessionId is required")
		}
		if err := requireAbsolute("cwd", input.CWD); err != nil {
			return acceptance{}, err
		}
		replay := method == methodSessionLoad
		var opened *session
		return acceptance{
			run: func(ctx context.Context) (any, error) {
				result, existing, err := a.openSession(ctx, input, replay)
				opened = existing
				return result, err
			},
			after: func() { opened.announce() },
		}, nil
	case methodSessionList:
		var input listSessionsParams
		if len(params) > 0 && string(params) != "null" {
			if err := decodeParams(params, &input); err != nil {
				return acceptance{}, err
			}
		}
		if input.CWD != "" {
			if err := requireAbsolute("cwd", input.CWD); err != nil {
				return acceptance{}, err
			}
		}
		offset := 0
		if input.Cursor != "" {
			decoded, err := decodeCursor(input.Cursor)
			if err != nil {
				return acceptance{}, err
			}
			offset = decoded
		}
		return acceptance{run: func(ctx context.Context) (any, error) { return a.listSessions(ctx, input.CWD, offset) }}, nil
	case methodSessionClose:
		var input sessionIDParams
		if err := decodeParams(params, &input); err != nil {
			return acceptance{}, err
		}
		if input.SessionID == "" {
			return acceptance{}, invalidParams("sessionId is required")
		}
		return acceptance{run: func(ctx context.Context) (any, error) {
			a.closeSession(input.SessionID, true)
			return struct{}{}, nil
		}}, nil
	case methodSessionPrompt:
		var input promptParams
		if err := decodeParams(params, &input); err != nil {
			return acceptance{}, err
		}
		if input.Prompt == nil {
			return acceptance{}, invalidParams("prompt is required")
		}
		target, err := a.session(input.SessionID)
		if err != nil {
			return acceptance{}, err
		}
		return target.acceptPrompt(input.Prompt)
	case methodSetConfigOption:
		var input setConfigOptionParams
		if err := decodeParams(params, &input); err != nil {
			return acceptance{}, err
		}
		target, err := a.session(input.SessionID)
		if err != nil {
			return acceptance{}, err
		}
		value, err := configValue(input)
		if err != nil {
			return acceptance{}, err
		}
		return acceptance{run: func(ctx context.Context) (any, error) {
			options, err := target.setConfigOption(ctx, input.ConfigID, value)
			if err != nil {
				return nil, err
			}
			return setConfigOptionResult{ConfigOptions: options}, nil
		}}, nil
	}
	return acceptance{}, newError(codeMethodNotFound, "method %q is not supported", method)
}

func (a *agent) notify(method string, params json.RawMessage) {
	a.mu.Lock()
	initialized := a.initialized
	a.mu.Unlock()
	if !initialized || method != methodSessionCancel {
		return
	}
	var input sessionIDParams
	if err := json.Unmarshal(params, &input); err != nil {
		a.logger.Warn("ignoring malformed session/cancel")
		return
	}
	a.mu.Lock()
	target := a.sessions[input.SessionID]
	a.mu.Unlock()
	if target != nil {
		target.cancelPrompt()
	}
}

func (a *agent) acceptInitialize(params json.RawMessage) (acceptance, error) {
	var input initializeParams
	if err := decodeParams(params, &input); err != nil {
		return acceptance{}, err
	}
	if input.ProtocolVersion == nil {
		return acceptance{}, invalidParams("protocolVersion is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized {
		return acceptance{}, newError(codeInvalidRequest, "the connection is already initialized")
	}
	a.initialized = true
	a.capabilities = input.ClientCapabilities
	if input.ClientInfo != nil {
		a.logger.Info("acp client connected", "client", input.ClientInfo.Name, "version", input.ClientInfo.Version, "protocolVersion", *input.ProtocolVersion)
	}
	result := initializeResult{
		ProtocolVersion: ProtocolVersion,
		AgentCapabilities: agentCapabilities{
			LoadSession:        true,
			PromptCapabilities: promptCapabilities{Image: true, EmbeddedContext: true},
			SessionCapabilities: sessionCapabilities{
				List: &empty{}, Resume: &empty{}, Close: &empty{},
			},
		},
		AgentInfo:   Implementation{Name: "kit", Title: "Kit", Version: a.options.Version},
		AuthMethods: []struct{}{},
	}
	return acceptance{run: func(context.Context) (any, error) { return result, nil }}, nil
}

func (a *agent) clientCapabilities() clientCapabilities {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.capabilities
}

func (a *agent) session(sessionID string) (*session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if target := a.sessions[sessionID]; target != nil {
		return target, nil
	}
	return nil, unknownSession(sessionID)
}

func unknownSession(sessionID string) *rpcError {
	return &rpcError{
		Code: codeNotFound, Message: fmt.Sprintf("session %q is not open on this connection", sessionID),
		Data: map[string]string{"sessionId": sessionID},
	}
}

func (a *agent) newSession(ctx context.Context, input newSessionParams) (any, *session, error) {
	backend, err := a.backend.get(ctx)
	if err != nil {
		return nil, nil, err
	}
	catalog, err := backend.Models(ctx)
	if err != nil {
		return nil, nil, projectBackendError(err)
	}
	if !anyModelAvailable(catalog) {
		return nil, nil, &rpcError{Code: codeAuthRequired, Message: "No model provider is logged in. Run `kit` and use /login, or `kit auth login openai-codex`, then try again."}
	}
	create := protocol.CreateSessionInput{CWD: filepath.Clean(input.CWD), Temporary: a.options.Temporary}
	if create.Temporary {
		create.ID, err = a.options.NewSessionID()
		if err != nil {
			return nil, nil, err
		}
	}
	info, err := backend.CreateSession(ctx, create)
	if err != nil {
		return nil, nil, projectBackendError(err)
	}
	opened, err := a.attach(ctx, backend, info.ID, create.Temporary, input.MCPServers, false)
	if err != nil {
		if create.Temporary {
			a.dispose(backend, info.ID)
		}
		return nil, nil, err
	}
	return newSessionResult{SessionID: info.ID, ConfigOptions: opened.configOptions()}, opened, nil
}

func (a *agent) openSession(ctx context.Context, input loadSessionParams, replay bool) (any, *session, error) {
	if existing, _ := a.session(input.SessionID); existing != nil {
		if replay {
			if err := existing.replay(ctx); err != nil {
				return nil, nil, err
			}
		}
		existing.reportMCPServers(input.MCPServers)
		return loadSessionResult{ConfigOptions: existing.configOptions()}, existing, nil
	}
	backend, err := a.backend.get(ctx)
	if err != nil {
		return nil, nil, err
	}
	opened, err := a.attachChecked(ctx, backend, input, replay)
	if err != nil {
		return nil, nil, err
	}
	return loadSessionResult{ConfigOptions: opened.configOptions()}, opened, nil
}

func (a *agent) attachChecked(ctx context.Context, backend Backend, input loadSessionParams, replay bool) (*session, error) {
	return a.attach(ctx, backend, input.SessionID, false, input.MCPServers, replay, func(info protocol.SessionInfo) error {
		if filepath.Clean(info.CWD) != filepath.Clean(input.CWD) {
			return invalidParams("session %q has working directory %s, not %s", input.SessionID, info.CWD, input.CWD)
		}
		return nil
	})
}

// attach opens a Kit session on this connection and starts translating its
// events. check, when given, validates the session before it is opened.
func (a *agent) attach(
	ctx context.Context, backend Backend, sessionID string, temporary bool,
	servers []mcpServer, replay bool, checks ...func(protocol.SessionInfo) error,
) (*session, error) {
	a.mu.Lock()
	if a.opening == nil {
		a.opening = map[string]bool{}
	}
	if a.opening[sessionID] || a.sessions[sessionID] != nil {
		a.mu.Unlock()
		return nil, newError(codeInvalidRequest, "session %q is already open or opening", sessionID)
	}
	a.opening[sessionID] = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.opening, sessionID)
		a.mu.Unlock()
	}()

	handle, err := backend.Attach(ctx, sessionID)
	if err != nil {
		if serverErrorCode(err) == kit.ErrorNotFound {
			return nil, unknownSession(sessionID)
		}
		return nil, projectBackendError(err)
	}
	opened := newSession(a, backend, handle, sessionID, temporary)
	if err := opened.open(ctx, replay, checks...); err != nil {
		opened.detach()
		return nil, err
	}
	a.mu.Lock()
	a.sessions[sessionID] = opened
	a.mu.Unlock()
	opened.start()
	opened.reportMCPServers(servers)
	return opened, nil
}

// closeSession detaches a session. When cancel is set, the prompt this
// connection is running in it is cancelled first, as session/cancel does.
func (a *agent) closeSession(sessionID string, cancel bool) {
	a.mu.Lock()
	target := a.sessions[sessionID]
	delete(a.sessions, sessionID)
	a.mu.Unlock()
	if target == nil {
		return
	}
	if cancel {
		target.cancelAndAbort()
	}
	target.detach()
	if target.temporary {
		a.dispose(target.backend, sessionID)
	}
}

func (a *agent) closeAll() {
	a.mu.Lock()
	ids := make([]string, 0, len(a.sessions))
	for id := range a.sessions {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.closeSession(id, false)
	}
}

func (a *agent) dispose(backend Backend, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := backend.DisposeTemporarySession(ctx, sessionID); err != nil && serverErrorCode(err) != kit.ErrorNotFound {
		a.logger.Warn("dispose temporary session", "session", sessionID, "err", err)
	}
}

func (a *agent) listSessions(ctx context.Context, cwd string, offset int) (any, error) {
	backend, err := a.backend.get(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := backend.ListSessions(ctx, cwd)
	if err != nil {
		return nil, projectBackendError(err)
	}
	if offset > len(sessions) {
		return nil, invalidParams("cursor is out of range")
	}
	result := listSessionsResult{Sessions: []sessionInfo{}}
	end := min(offset+pageSize, len(sessions))
	for _, info := range sessions[offset:end] {
		if info.Temporary {
			continue
		}
		result.Sessions = append(result.Sessions, sessionInfo{
			SessionID: info.ID, CWD: info.CWD, Title: info.Name, UpdatedAt: info.UpdatedAt,
		})
	}
	if end < len(sessions) {
		result.NextCursor = encodeCursor(end)
	}
	return result, nil
}

const cursorPrefix = "kit-sessions:"

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strconv.Itoa(offset)))
}

func decodeCursor(cursor string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err == nil {
		if text, ok := strings.CutPrefix(string(raw), cursorPrefix); ok {
			if offset, err := strconv.Atoi(text); err == nil && offset >= 0 {
				return offset, nil
			}
		}
	}
	return 0, invalidParams("cursor is invalid")
}

func anyModelAvailable(catalog protocol.ModelCatalog) bool {
	for _, model := range catalog.Models {
		if model.Available {
			return true
		}
	}
	return false
}

// decodeParams decodes a method's params object into target.
func decodeParams(params json.RawMessage, target any) error {
	if len(params) == 0 || string(params) == "null" {
		return invalidParams("params are required")
	}
	if err := json.Unmarshal(params, target); err != nil {
		return invalidParams("invalid params: %v", err)
	}
	return nil
}

func requireAbsolute(field, path string) error {
	if path == "" {
		return invalidParams("%s is required", field)
	}
	if !filepath.IsAbs(path) {
		return invalidParams("%s must be an absolute path", field)
	}
	return nil
}

// lazyBackend connects to the Kit server once, in the background, and lets a
// later request try again after a failure.
type lazyBackend struct {
	connect func(context.Context) (Backend, error)
	// ctx bounds every connection attempt; it ends with the connection.
	ctx context.Context

	mu      sync.Mutex
	backend Backend
	attempt chan struct{}
	err     error
}

func (l *lazyBackend) start() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.attempt != nil {
		return l.attempt
	}
	done := make(chan struct{})
	l.attempt = done
	go func() {
		backend, err := l.connect(l.ctx)
		l.mu.Lock()
		defer l.mu.Unlock()
		l.backend, l.err = backend, err
		close(done)
	}()
	return done
}

func (l *lazyBackend) get(ctx context.Context) (Backend, error) {
	done := l.start()
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.backend != nil {
		return l.backend, nil
	}
	err := l.err
	if l.attempt == done {
		l.attempt = nil
	}
	if err == nil {
		err = errors.New("no server connection")
	}
	return nil, &rpcError{Code: codeInternalError, Message: "Kit server is unavailable: " + err.Error()}
}
