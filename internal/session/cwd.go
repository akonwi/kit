package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/identifier"
)

const ChangeCWDToolName = "change_cwd"

type workspaceScope struct {
	mutationMu sync.Mutex
	state      atomic.Pointer[workspaceState]
	mutations  map[string]CWDMutation
	// rediscovered holds the mutations whose change reloaded this runtime
	// because it was built while its cwd was missing, so a replay reports
	// the same outcome. Guarded by mutationMu.
	rediscovered map[string]struct{}
}

type workspaceState struct {
	cwd        string
	generation uint64
}

func newWorkspaceScope(cwd string) *workspaceScope {
	scope := &workspaceScope{mutations: make(map[string]CWDMutation), rediscovered: make(map[string]struct{})}
	scope.state.Store(&workspaceState{cwd: filepath.Clean(cwd)})
	return scope
}

func (scope *workspaceScope) CWD() string {
	return scope.state.Load().cwd
}

func (scope *workspaceScope) snapshot() (string, uint64) {
	state := scope.state.Load()
	return state.cwd, state.generation
}

func (scope *workspaceScope) publish(cwd string) {
	current := scope.state.Load()
	if current.cwd != cwd {
		scope.state.Store(&workspaceState{cwd: cwd, generation: current.generation + 1})
	}
}

// ChangeCWDResult describes one session filesystem-scope mutation.
type ChangeCWDResult struct {
	Session     SessionRecord
	PreviousCWD string
	CWD         string
	Changed     bool
	// Rediscovered reports that the change reloaded project context, skills,
	// prompt commands, and tools because the runtime had been built while its
	// working directory was missing.
	Rediscovered bool
}

// ChangeCWD changes the filesystem scope used by subsequent relative coding
// tool and direct bash executions. It reloads session context only when the
// runtime was built while its working directory was missing, so project
// discovery resumes in the new, existing directory.
func (m *Manager) ChangeCWD(ctx context.Context, sessionID, targetPath string) (ChangeCWDResult, error) {
	mutationID, err := identifier.New("cwd_")
	if err != nil {
		return ChangeCWDResult{}, err
	}
	return m.ChangeCWDWithID(ctx, sessionID, mutationID, targetPath)
}

// ChangeCWDWithID changes a session filesystem scope idempotently. Repeating a
// mutation id returns its original outcome without resolving a relative target
// against the new cwd a second time.
func (m *Manager) ChangeCWDWithID(ctx context.Context, sessionID, mutationID, targetPath string) (ChangeCWDResult, error) {
	return m.changeCWDWithID(ctx, sessionID, mutationID, targetPath, true)
}

func (m *Manager) changeCWDWithID(ctx context.Context, sessionID, mutationID, targetPath string, informDroid bool) (ChangeCWDResult, error) {
	if err := m.beginOperation(); err != nil {
		return ChangeCWDResult{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ChangeCWDResult{}, err
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return ChangeCWDResult{}, err
	}
	if !informDroid {
		// The model's change_cwd runs inside its own turn, so it neither waits
		// for a runtime transition nor reports its move as a boundary.
		loaded.workspace.mutationMu.Lock()
		result, err := m.changeRuntimeCWD(ctx, sessionID, loaded, mutationID, targetPath)
		_, result.Rediscovered = loaded.workspace.rediscovered[mutationID]
		loaded.workspace.mutationMu.Unlock()
		if err == nil && !result.Rediscovered && m.projectReturned(loaded, result.CWD) && loaded.transitionMu.TryLock() {
			result.Rediscovered = m.rediscover(ctx, sessionID, loaded, mutationID)
			loaded.transitionMu.Unlock()
		}
		return result, err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	loaded.mu.Lock()
	loaded.workspace.mutationMu.Lock()
	result, err := m.changeRuntimeCWD(ctx, sessionID, loaded, mutationID, targetPath)
	_, result.Rediscovered = loaded.workspace.rediscovered[mutationID]
	loaded.workspace.mutationMu.Unlock()
	loaded.mu.Unlock()
	if err != nil {
		return result, err
	}
	if !result.Rediscovered && m.projectReturned(loaded, result.CWD) {
		result.Rediscovered = m.rediscover(ctx, sessionID, loaded, mutationID)
	}
	if !result.Changed {
		return result, nil
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	loaded.workspace.mutationMu.Lock()
	defer loaded.workspace.mutationMu.Unlock()
	informContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := informDroidOfCWDChange(informContext, sessionID, loaded, mutationID, result); err != nil {
		return result, fmt.Errorf("inform droid of cwd change: %w", err)
	}
	return result, nil
}

// rediscover reloads loaded after mutationID moved it from a missing cwd and
// records the outcome for replays. The caller holds loaded.transitionMu.
func (m *Manager) rediscover(ctx context.Context, sessionID string, loaded *runtime, mutationID string) bool {
	if _, err := m.reloadTransitioning(ctx, sessionID, loaded); err != nil {
		return false
	}
	loaded.workspace.mutationMu.Lock()
	loaded.workspace.rediscovered[mutationID] = struct{}{}
	loaded.workspace.mutationMu.Unlock()
	return true
}

// projectReturned reports whether loaded's runtime bundle omitted project
// discovery because its cwd was missing, and cwd now exists to discover from.
func (m *Manager) projectReturned(loaded *runtime, cwd string) bool {
	loaded.mu.Lock()
	unavailable := loaded.bundle.ProjectUnavailable
	loaded.mu.Unlock()
	return unavailable && existingDirectory(cwd)
}

func (m *Manager) changeRuntimeCWD(ctx context.Context, sessionID string, loaded *runtime, mutationID, targetPath string) (ChangeCWDResult, error) {
	previous := loaded.workspace.CWD()
	targetPath = strings.TrimSpace(targetPath)
	if !validCWDMutationID(mutationID) {
		return ChangeCWDResult{PreviousCWD: previous}, fmt.Errorf("%w: cwd mutation id is invalid", ErrInvalidInput)
	}
	if m.sessionDeleting(sessionID) {
		return ChangeCWDResult{PreviousCWD: previous}, ErrDeleteBusy
	}
	m.mu.Lock()
	temporary, isTemporary := m.temporary[sessionID]
	cached, cachedMutation := loaded.workspace.mutations[mutationID]
	m.mu.Unlock()
	if cachedMutation {
		if cached.TargetPath != targetPath {
			return ChangeCWDResult{PreviousCWD: previous}, fmt.Errorf("%w: cwd mutation id was reused", ErrInvalidInput)
		}
		return ChangeCWDResult{Session: temporary, PreviousCWD: cached.PreviousCWD, CWD: cached.CWD, Changed: cached.Changed}, nil
	}
	if !isTemporary {
		result, found, err := m.persistedCWDMutationResult(ctx, loaded, sessionID, mutationID, targetPath)
		if err != nil {
			return ChangeCWDResult{PreviousCWD: previous}, err
		}
		if found {
			return result, nil
		}
	}
	target, err := resolveCWDTarget(previous, targetPath)
	if err != nil {
		return ChangeCWDResult{PreviousCWD: previous}, err
	}
	if err := ctx.Err(); err != nil {
		return ChangeCWDResult{PreviousCWD: previous}, err
	}
	if m.sessionDeleting(sessionID) {
		return ChangeCWDResult{PreviousCWD: previous}, ErrDeleteBusy
	}
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangeCWDResult{PreviousCWD: previous}, fmt.Errorf("%w: working directory does not exist: %s", ErrInvalidInput, target)
		}
		return ChangeCWDResult{PreviousCWD: previous}, fmt.Errorf("inspect working directory %q: %w", target, err)
	}
	if !info.IsDir() {
		return ChangeCWDResult{PreviousCWD: previous}, fmt.Errorf("%w: working directory is not a directory: %s", ErrInvalidInput, target)
	}
	mutation := CWDMutation{
		ID: mutationID, SessionID: sessionID, TargetPath: targetPath,
		PreviousCWD: previous, CWD: target, Changed: target != previous,
	}
	var record SessionRecord
	if isTemporary {
		m.mu.Lock()
		current, exists := m.temporary[sessionID]
		if !exists || m.deleting[sessionID] {
			m.mu.Unlock()
			return ChangeCWDResult{PreviousCWD: previous}, ErrDeleteBusy
		}
		temporary = current
		if mutation.Changed {
			temporary.CWD = target
		}
		activityAt := time.Now().UTC()
		if activityAt.After(temporary.UpdatedAt) {
			temporary.UpdatedAt = activityAt
		}
		m.temporary[sessionID] = temporary
		m.mu.Unlock()
		loaded.workspace.mutations[mutationID] = mutation
		record = temporary
	} else {
		var applied CWDMutation
		record, applied, err = m.store.ApplySessionCWDMutation(ctx, mutation)
		if err != nil {
			reconcileContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			result, found, reconcileErr := m.persistedCWDMutationResult(reconcileContext, loaded, sessionID, mutationID, targetPath)
			if reconcileErr != nil {
				return ChangeCWDResult{PreviousCWD: previous}, errors.Join(err, reconcileErr)
			}
			if !found {
				return ChangeCWDResult{PreviousCWD: previous}, err
			}
			return result, nil
		}
		mutation = applied
	}
	loaded.workspace.publish(record.CWD)
	if loaded.plugins != nil {
		loaded.plugins.ChangeCWD(record.CWD)
	}
	if mutation.Changed {
		if err := loaded.events.append([]NewEvent{{SessionID: sessionID, Kind: EventSessionCWDChanged, CWD: record.CWD}}); err != nil {
			loaded.events.invalidate()
		}
	}
	return ChangeCWDResult{Session: record, PreviousCWD: mutation.PreviousCWD, CWD: mutation.CWD, Changed: mutation.Changed}, nil
}

func (m *Manager) persistedCWDMutationResult(
	ctx context.Context,
	loaded *runtime,
	sessionID, mutationID, targetPath string,
) (ChangeCWDResult, bool, error) {
	persisted, err := m.store.GetSessionCWDMutation(ctx, sessionID, mutationID)
	if errors.Is(err, ErrNotFound) {
		return ChangeCWDResult{}, false, nil
	}
	if err != nil {
		return ChangeCWDResult{}, false, err
	}
	if persisted.TargetPath != targetPath {
		return ChangeCWDResult{}, false, fmt.Errorf("%w: cwd mutation id was reused", ErrInvalidInput)
	}
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return ChangeCWDResult{}, false, err
	}
	loaded.workspace.publish(record.CWD)
	if loaded.plugins != nil {
		loaded.plugins.ChangeCWD(record.CWD)
	}
	return ChangeCWDResult{
		Session: record, PreviousCWD: persisted.PreviousCWD,
		CWD: persisted.CWD, Changed: persisted.Changed,
	}, true, nil
}

func validCWDMutationID(id string) bool {
	return id != "" && len(id) <= 256 && utf8.ValidString(id) && strings.IndexByte(id, 0) < 0
}

func validWorkspacePath(path string) bool {
	if path == "" || len(path) > 4096 || !utf8.ValidString(path) || strings.IndexByte(path, 0) >= 0 {
		return false
	}
	for _, character := range path {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func resolveCWDTarget(base, target string) (string, error) {
	target = strings.TrimSpace(target)
	if !validWorkspacePath(target) {
		return "", fmt.Errorf("%w: working directory path is required and must be renderer-safe UTF-8 at most 4096 bytes", ErrInvalidInput)
	}
	if target == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		target = home
	} else if strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		target = filepath.Join(home, target[2:])
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) || !validWorkspacePath(target) {
		return "", fmt.Errorf("%w: resolved working directory is not a safe bounded absolute path", ErrInvalidInput)
	}
	return target, nil
}

type cwdBoundaryDetails struct {
	Version     int    `json:"version"`
	PreviousCWD string `json:"previousCwd"`
	CWD         string `json:"cwd"`
}

func informDroidOfCWDChange(ctx context.Context, sessionID string, loaded *runtime, mutationID string, result ChangeCWDResult) error {
	if loaded.workspace.CWD() != result.CWD {
		return nil
	}
	details, err := droids.EncodeDetails(cwdBoundaryDetails{Version: 1, PreviousCWD: result.PreviousCWD, CWD: result.CWD})
	if err != nil {
		return err
	}
	reloaded := "Session context, skills, and prompt commands have not been reloaded."
	if result.Rediscovered {
		reloaded = "Session context, skills, and prompt commands were reloaded from " + result.CWD + "."
	}
	message := fmt.Sprintf(
		"The user changed this session's working directory from %s to %s. Relative filesystem operations now resolve from %s. %s",
		result.PreviousCWD, result.CWD, result.CWD, reloaded,
	)
	return informBoundary(ctx, loaded.droid, loaded.events, sessionID, droids.BoundaryMessage{
		ID: "cwd:" + mutationID, Kind: "cwd", Source: "user",
		Content: []droids.InputContent{droids.TextInput{Text: message}}, Details: details,
	})
}

type changeCWDArgs struct {
	Path string `json:"path"`
}

type changeCWDDetails struct {
	PreviousCWD string `json:"previousCwd"`
	CWD         string `json:"cwd,omitempty"`
	Changed     bool   `json:"changed"`
}

func (m *Manager) changeCWDTool(sessionID string, scope *workspaceScope) droids.AnyTool {
	return droids.MustTool(droids.Tool[changeCWDArgs]{
		Name:        ChangeCWDToolName,
		Description: "Change the current working directory for this Kit session. Relative paths resolve from the session cwd.",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"path": map[string]any{
				"type": "string", "description": "Directory to make the current working directory for this session",
			}},
			"required": []string{"path"},
		},
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, call droids.ToolContext, args changeCWDArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			result, err := m.changeCWDWithID(ctx, sessionID, "tool:"+string(call.TurnID)+":"+string(call.ToolCallID), args.Path, false)
			if err != nil {
				if ctx.Err() != nil {
					return droids.ToolResult{}, ctx.Err()
				}
				details, _ := droids.EncodeDetails(changeCWDDetails{PreviousCWD: scope.CWD()})
				return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: "Error: " + err.Error()}}, Details: details, IsError: true}, nil
			}
			details, _ := droids.EncodeDetails(changeCWDDetails{
				PreviousCWD: result.PreviousCWD, CWD: result.CWD, Changed: result.Changed,
			})
			text := "Already in directory: " + result.CWD
			if result.Changed {
				text = "Changed session cwd to " + result.CWD
			}
			if result.Rediscovered {
				text += ". Reloaded project context, skills, and prompt commands from " + result.CWD + "."
			}
			return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: text}}, Details: details}, nil
		},
	})
}
