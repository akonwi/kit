package workingdiff

import (
	"context"
	"errors"
	"sort"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
)

// LineRangeInput identifies one source-side range in a retained diff observation.
type LineRangeInput struct {
	TargetID, TargetRevision, Path, FileRevision, Side string
	Target                                             *protocol.PinnedDiffTarget
	DeriveTarget                                       bool
	StartLine, EndLine                                 int
}

// LineRangeEvidence is authoritative source text for one diff-side range.
type LineRangeEvidence struct {
	// Content holds the selected source lines, each followed by "\n" even when
	// the source's last line has no terminator. A CRLF line keeps its "\r".
	Content string
	EndLine int
	Target  *protocol.PinnedDiffTarget
}

// ReadLineRange revalidates a retained observation and returns exact old/new
// source lines. It never derives evidence from rendered hunks.
//
// A working-tree range belongs to its file, not to the whole observation that
// named it: when that observation is gone or another part of the working tree
// changed, the range is revalidated against a current observation and is
// stale only if its own file revision no longer matches.
func (s *Service) ReadLineRange(ctx context.Context, session, cwd string, input LineRangeInput) (_ LineRangeEvidence, err error) {
	defer projectDeadline(ctx, &err)
	if input.Side != "old" && input.Side != "new" || input.StartLine <= 0 || input.EndLine < input.StartLine || input.Target != nil && input.Target.Validate() != nil {
		return LineRangeEvidence{}, &Error{Code: InvalidPath, Message: "diff evidence range is invalid"}
	}
	observation, page, err := s.readAnnotationFile(ctx, session, cwd, input)
	if err != nil {
		return LineRangeEvidence{}, err
	}
	if page.File.ContentState != "text" || page.Computation.State != "complete" {
		return LineRangeEvidence{}, &Error{Code: Unavailable, Message: "diff evidence is unavailable"}
	}
	index := sort.Search(len(observation.files), func(index int) bool { return observation.files[index].summary.Path >= input.Path })
	if index == len(observation.files) || observation.files[index].summary.Path != input.Path {
		return LineRangeEvidence{}, &Error{Code: NotFound, Message: "changed file is not in the observation"}
	}
	file := observation.files[index]
	source := file.old
	if input.Side == "new" {
		source = file.new
	}
	lines, reason := splitLines(source)
	if reason != "" || input.EndLine > len(lines) {
		return LineRangeEvidence{}, &Error{Code: Unavailable, Message: "diff evidence range is unavailable"}
	}
	oldLines, oldReason := splitLines(file.old)
	newLines, newReason := splitLines(file.new)
	if oldReason != "" || newReason != "" {
		return LineRangeEvidence{}, &Error{Code: Unavailable, Message: "diff evidence is unavailable"}
	}
	hunks, diffErr := semanticHunks(ctx, oldLines, newLines)
	if diffErr != nil {
		if ctx.Err() != nil {
			return LineRangeEvidence{}, ctx.Err()
		}
		return LineRangeEvidence{}, &Error{Code: Unavailable, Message: "diff evidence is unavailable"}
	}
	covered := make(map[int]struct{}, input.EndLine-input.StartLine+1)
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			coordinate := line.NewLine
			if input.Side == "old" {
				coordinate = line.OldLine
			}
			if coordinate != nil && *coordinate >= input.StartLine && *coordinate <= input.EndLine {
				covered[*coordinate] = struct{}{}
			}
		}
	}
	if len(covered) != input.EndLine-input.StartLine+1 {
		return LineRangeEvidence{}, &Error{Code: InvalidPath, Message: "diff evidence range is outside rendered hunks"}
	}
	var content strings.Builder
	for _, line := range lines[input.StartLine-1 : input.EndLine] {
		content.WriteString(line.text)
		content.WriteByte('\n')
	}
	var target *protocol.PinnedDiffTarget
	if observation.committed {
		definition := protocol.PinnedDiffTarget{WorkspaceID: observation.Target.WorkspaceID, Kind: observation.Target.Kind, Base: observation.Target.Base, Head: observation.Target.Head}
		target = &definition
	}
	return LineRangeEvidence{Content: content.String(), EndLine: input.EndLine, Target: target}, nil
}

// readAnnotationFile reads the anchored file from the observation the anchor
// names. A working-tree anchor whose observation is gone or no longer
// matches the live working tree is read from a current observation instead,
// so it is stale only when its own file revision changed.
func (s *Service) readAnnotationFile(ctx context.Context, session, cwd string, input LineRangeInput) (*observation, protocol.FileDiffPage, error) {
	observation, err := s.resolveAnnotationObservation(ctx, session, cwd, input)
	if err == nil {
		page, err := s.readAnnotationPage(ctx, session, cwd, input, observation)
		if err == nil || observation.committed || !isDiffError(err, StaleTarget) && !isDiffError(err, StaleFile) {
			return observation, page, err
		}
	} else if input.Target != nil || !isDiffError(err, StaleTarget) {
		return nil, protocol.FileDiffPage{}, err
	}
	stale := input.TargetRevision
	for attempt := 0; ; attempt++ {
		current, err := s.currentWorkingTree(ctx, session, cwd, input.TargetID, stale)
		if err != nil {
			return nil, protocol.FileDiffPage{}, err
		}
		page, err := s.readAnnotationPage(ctx, session, cwd, input, current)
		// Another part of the working tree can change between proving
		// a current observation and reading from it; observe again a bounded
		// number of times.
		if err == nil || attempt == 2 || !isDiffError(err, StaleTarget) {
			return current, page, err
		}
		stale = current.Revision
	}
}

// currentWorkingTree returns an observation of the session's working-tree
// target that matched the live working tree when checked: the session's
// latest retained one when it is still current, or else a new one. stale is
// a revision already found outdated.
func (s *Service) currentWorkingTree(ctx context.Context, session, cwd, target, stale string) (*observation, error) {
	workspace := s.workspaces.Ref(session, cwd)
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if targetID(session, workspace.WorkspaceID, repo, protocol.DiffTargetWorkingTree, protocol.DiffEndpoint{}, protocol.DiffEndpoint{}) != target {
		return nil, &Error{Code: StaleTarget, Message: "diff observation is unavailable"}
	}
	release, err := s.acquire(ctx, session)
	if err != nil {
		return nil, err
	}
	defer release()
	if latest := s.latestWorkingTree(session, target); latest != nil && latest.Revision != stale {
		err := s.revalidateWorkingTree(ctx, session, cwd, latest, "")
		if err == nil {
			return latest, nil
		}
		if !isDiffError(err, StaleTarget) {
			return nil, err
		}
	}
	current, err := s.observeWorkingTree(ctx, session, cwd, workspace.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if current.Target.ID != target {
		return nil, &Error{Code: StaleTarget, Message: "diff observation is unavailable"}
	}
	return current, nil
}

// readAnnotationPage reads the anchored file's first page from observation,
// guarded by the anchor's file revision.
func (s *Service) readAnnotationPage(ctx context.Context, session, cwd string, input LineRangeInput, observation *observation) (protocol.FileDiffPage, error) {
	page, _, err := s.readFile(ctx, session, cwd, protocol.ReadFileDiffInput{
		TargetID: input.TargetID, TargetRevision: observation.Revision, Path: input.Path,
		ExpectedFileRevision: input.FileRevision,
	}, observation)
	return page, err
}

func isDiffError(err error, code protocol.DiffErrorCode) bool {
	var diffErr *Error
	return errors.As(err, &diffErr) && diffErr.Code == code
}

func (s *Service) resolveAnnotationObservation(ctx context.Context, session, cwd string, input LineRangeInput) (*observation, error) {
	if observation, ok := s.get(input.TargetRevision, session); ok {
		if observation.Target.ID != input.TargetID {
			return nil, &Error{Code: StaleTarget, Message: "diff annotation target identity does not match"}
		}
		if observation.committed {
			if input.Target == nil && input.DeriveTarget {
				return observation, nil
			}
			if input.Target == nil || input.Target.Validate() != nil || input.Target.WorkspaceID != observation.Target.WorkspaceID || input.Target.Kind != observation.Target.Kind || input.Target.Base != observation.Target.Base || input.Target.Head != observation.Target.Head {
				return nil, &Error{Code: StaleTarget, Message: "diff annotation target definition does not match"}
			}
		} else if input.Target != nil {
			return nil, &Error{Code: StaleTarget, Message: "working-tree annotation cannot carry committed target evidence"}
		}
		return observation, nil
	}
	if input.Target == nil || input.Target.Validate() != nil {
		return nil, &Error{Code: StaleTarget, Message: "diff observation is unavailable"}
	}
	workspace := s.workspaces.Ref(session, cwd)
	if workspace.WorkspaceID != input.Target.WorkspaceID {
		return nil, &Error{Code: StaleWorkspace, Message: "the annotation workspace changed"}
	}
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if targetID(session, input.Target.WorkspaceID, repo, input.Target.Kind, input.Target.Base, input.Target.Head) != input.TargetID {
		return nil, &Error{Code: StaleTarget, Message: "persisted diff target does not match repository authority"}
	}
	ref := targetReference{Session: session, Workspace: input.Target.WorkspaceID, Authority: authorityToken(repo), Kind: input.Target.Kind, Base: input.Target.Base, Head: input.Target.Head}
	observation, err := s.observeCommittedObservation(ctx, session, cwd, input.Target.WorkspaceID, ref)
	if err != nil {
		return nil, err
	}
	if observation.Target.ID != input.TargetID || observation.Revision != input.TargetRevision {
		return nil, &Error{Code: StaleTarget, Message: "reconstructed diff observation does not match annotation"}
	}
	return observation, nil
}

// ReadFileForAnnotation reconstructs and pins one server-authorized committed
// observation for the duration of a guarded file read.
func (s *Service) ReadFileForAnnotation(ctx context.Context, session, cwd string, input protocol.ReadFileDiffInput, target *protocol.PinnedDiffTarget) (_ protocol.FileDiffPage, err error) {
	defer projectDeadline(ctx, &err)
	observation, err := s.resolveAnnotationObservation(ctx, session, cwd, LineRangeInput{
		TargetID: input.TargetID, TargetRevision: input.TargetRevision, Path: input.Path,
		FileRevision: input.ExpectedFileRevision, Target: target,
	})
	if err != nil {
		return protocol.FileDiffPage{}, err
	}
	return s.readHighlightedFile(ctx, session, cwd, input, observation)
}
