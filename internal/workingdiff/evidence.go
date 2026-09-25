package workingdiff

import (
	"context"
	"sort"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
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
	Content string
	EndLine int
	Target  *protocol.PinnedDiffTarget
}

// ReadLineRange revalidates a retained observation and returns exact old/new
// source lines. It never derives evidence from rendered hunks.
func (s *Service) ReadLineRange(ctx context.Context, session, cwd string, input LineRangeInput) (LineRangeEvidence, error) {
	if input.Side != "old" && input.Side != "new" || input.StartLine <= 0 || input.EndLine < input.StartLine || input.Target != nil && input.Target.Validate() != nil {
		return LineRangeEvidence{}, &Error{Code: InvalidPath, Message: "diff evidence range is invalid"}
	}
	observation, err := s.resolveAnnotationObservation(ctx, session, cwd, input)
	if err != nil {
		return LineRangeEvidence{}, err
	}
	page, err := s.readFile(ctx, session, cwd, protocol.ReadFileDiffInput{
		TargetID: input.TargetID, TargetRevision: input.TargetRevision, Path: input.Path,
		ExpectedFileRevision: input.FileRevision,
	}, observation)
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
	selected := make([]string, 0, input.EndLine-input.StartLine+1)
	for _, line := range lines[input.StartLine-1 : input.EndLine] {
		selected = append(selected, line.text)
	}
	var target *protocol.PinnedDiffTarget
	if observation.committed {
		definition := protocol.PinnedDiffTarget{WorkspaceID: observation.Target.WorkspaceID, Kind: observation.Target.Kind, Base: observation.Target.Base, Head: observation.Target.Head}
		target = &definition
	}
	return LineRangeEvidence{Content: strings.Join(selected, "\n"), EndLine: input.EndLine, Target: target}, nil
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
func (s *Service) ReadFileForAnnotation(ctx context.Context, session, cwd string, input protocol.ReadFileDiffInput, target *protocol.PinnedDiffTarget) (protocol.FileDiffPage, error) {
	observation, err := s.resolveAnnotationObservation(ctx, session, cwd, LineRangeInput{
		TargetID: input.TargetID, TargetRevision: input.TargetRevision, Path: input.Path,
		FileRevision: input.ExpectedFileRevision, Target: target,
	})
	if err != nil {
		return protocol.FileDiffPage{}, err
	}
	return s.readFile(ctx, session, cwd, input, observation)
}
