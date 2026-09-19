package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

const toolFileNavigationTimeout = 5 * time.Second

type toolFileTarget struct {
	SessionID   string
	WorkspaceID string
	CWD         string
	Path        string
	StartLine   int
	EndLine     int
}

func (s *appState) openToolFile(target toolFileTarget) {
	s.openToolFileWithDispatch(target, s.Context().Runtime().Dispatch)
}

func (s *appState) openToolFileWithDispatch(target toolFileTarget, dispatch func(func())) {
	s.openToolFileWithDispatchTimeout(target, dispatch, toolFileNavigationTimeout)
}

func (s *appState) openToolFileWithDispatchTimeout(target toolFileTarget, dispatch func(func()), timeout time.Duration) {
	if s.toolFileNavigationCancel != nil {
		s.toolFileNavigationCancel()
		s.toolFileNavigationCancel = nil
	}
	s.toolFileNavigationGeneration++
	generation := s.toolFileNavigationGeneration

	if s.inputOwner().trapsFocus() {
		s.toolFileNavigationUnavailable("Finish the current interaction before opening a file.")
		return
	}
	if !s.toolFileTargetCurrent(target, s.operation) {
		s.toolFileNavigationUnavailable("The file link belongs to a stale session or workspace.")
		return
	}
	path, err := resolveToolFilePath(target.CWD, target.Path)
	if err != nil {
		s.toolFileNavigationUnavailable("The tool reported a path outside the current workspace.")
		return
	}
	startLine, endLine, err := toolFileRange(target.StartLine, target.EndLine)
	if err != nil {
		s.toolFileNavigationUnavailable("The tool reported an invalid source range.")
		return
	}
	files, ok := s.bound.(sessionclient.WorkspaceFilesSession)
	if !ok {
		s.toolFileNavigationUnavailable("This session does not expose workspace files.")
		return
	}

	operation := s.operation
	baseContext := s.attachmentCtx
	if baseContext == nil {
		baseContext = s.ctx
	}
	if baseContext == nil {
		baseContext = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseContext, timeout)
	s.toolFileNavigationCancel = cancel
	go func() {
		defer cancel()
		workspace, workspaceErr := files.Workspace(ctx)
		if workspaceErr != nil {
			s.dispatchToolFileFailure(dispatch, target, operation, generation, ctx, workspaceErr)
			return
		}
		if err := workspace.Validate(); err != nil || workspace.State != protocol.WorkspaceReady || workspace.SessionID != target.SessionID || workspace.WorkspaceID != target.WorkspaceID || workspace.CWD != target.CWD {
			s.dispatchToolFileFailure(dispatch, target, operation, generation, ctx, fmt.Errorf("workspace association changed"))
			return
		}
		read, readErr := files.ReadWorkspaceFile(ctx, protocol.ReadWorkspaceFileInput{WorkspaceID: workspace.WorkspaceID, Path: path})
		if readErr != nil {
			s.dispatchToolFileFailure(dispatch, target, operation, generation, ctx, readErr)
			return
		}
		if err := read.Validate(); err != nil || read.SessionID != target.SessionID || read.Workspace.WorkspaceID != target.WorkspaceID || read.Workspace.CWD != target.CWD || read.Path != path {
			s.dispatchToolFileFailure(dispatch, target, operation, generation, ctx, fmt.Errorf("file association changed"))
			return
		}
		if ctx.Err() != nil {
			s.dispatchToolFileFailure(dispatch, target, operation, generation, ctx, ctx.Err())
			return
		}
		startLine, endLine = toolFileRangeWithinRead(startLine, endLine, read.ReturnedLines)
		dispatch(func() {
			if !s.toolFileRequestCurrent(target, operation, generation) {
				return
			}
			if s.inputOwner().trapsFocus() {
				s.toolFileNavigationUnavailable("Finish the current interaction before opening a file.")
				return
			}
			descriptor := fileWorkspacePane(read.Workspace.WorkspaceID, read.Path)
			descriptor.ExpectedRevision = read.Revision
			descriptor.RevealStartLine = startLine
			descriptor.RevealEndLine = endLine
			var openErr error
			s.SetState(func() {
				_, _, openErr = s.workspace.Open(descriptor)
				if openErr == nil {
					s.syncWorkspaceSelection()
				}
			})
			if openErr != nil {
				s.toolFileNavigationUnavailable(openErr.Error())
			}
		})
	}()
}

func (s *appState) toolFileTargetCurrent(target toolFileTarget, operation uint64) bool {
	if s.ctx != nil && s.ctx.Err() != nil || s.attachmentCtx != nil && s.attachmentCtx.Err() != nil {
		return false
	}
	return s.phase == phaseReady && operation == s.operation && target.SessionID != "" && target.SessionID == s.session.ID &&
		target.WorkspaceID != "" && target.WorkspaceID == s.workspaceID && target.CWD != "" && target.CWD == s.session.CWD
}

func (s *appState) toolFileRequestCurrent(target toolFileTarget, operation, generation uint64) bool {
	return generation == s.toolFileNavigationGeneration && s.toolFileTargetCurrent(target, operation)
}

func (s *appState) dispatchToolFileFailure(dispatch func(func()), target toolFileTarget, operation, generation uint64, ctx context.Context, err error) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	subtitle := err.Error()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		subtitle = "Timed out while validating the workspace file."
	}
	dispatch(func() {
		if s.toolFileRequestCurrent(target, operation, generation) {
			s.toolFileNavigationUnavailable(subtitle)
		}
	})
}

func (s *appState) toolFileNavigationUnavailable(subtitle string) {
	s.showToast(toastInput{Title: "Could not open file", Subtitle: subtitle, Variant: toastWarning})
}

func resolveToolFilePath(cwd, toolPath string) (string, error) {
	if cwd == "" || !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd || strings.TrimSpace(toolPath) == "" {
		return "", fmt.Errorf("invalid tool file path")
	}
	absolute := toolPath
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(cwd, absolute)
	}
	absolute = filepath.Clean(absolute)
	relative, err := filepath.Rel(cwd, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("tool file path escapes workspace")
	}
	canonical := filepath.ToSlash(relative)
	if err := protocol.ValidateWorkspacePath(canonical, false); err != nil {
		return "", err
	}
	return canonical, nil
}

func toolFileRangeWithinRead(startLine, endLine, returnedLines int) (int, int) {
	if startLine <= 0 {
		return 0, 0
	}
	lastLine := max(1, returnedLines)
	startLine = min(startLine, lastLine)
	endLine = min(max(startLine, endLine), lastLine)
	return startLine, endLine
}

func toolFileRange(startLine, endLine int) (int, int, error) {
	if startLine < 0 || endLine < 0 || startLine == 0 && endLine != 0 || startLine > 0 && endLine > 0 && endLine < startLine {
		return 0, 0, fmt.Errorf("invalid tool file range")
	}
	if startLine > 0 && endLine == 0 {
		endLine = startLine
	}
	return startLine, endLine, nil
}
