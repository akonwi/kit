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
	StartLine, EndLine                                 int
}

// LineRangeEvidence is authoritative source text for one diff-side range.
type LineRangeEvidence struct {
	Content string
	EndLine int
}

// ReadLineRange revalidates a retained observation and returns exact old/new
// source lines. It never derives evidence from rendered hunks.
func (s *Service) ReadLineRange(ctx context.Context, session, cwd string, input LineRangeInput) (LineRangeEvidence, error) {
	if input.Side != "old" && input.Side != "new" || input.StartLine <= 0 || input.EndLine < input.StartLine {
		return LineRangeEvidence{}, &Error{Code: InvalidPath, Message: "diff evidence range is invalid"}
	}
	page, err := s.ReadFile(ctx, session, cwd, protocol.ReadFileDiffInput{
		TargetID: input.TargetID, TargetRevision: input.TargetRevision, Path: input.Path,
		ExpectedFileRevision: input.FileRevision,
	})
	if err != nil {
		return LineRangeEvidence{}, err
	}
	if page.File.ContentState != "text" || page.Computation.State != "complete" {
		return LineRangeEvidence{}, &Error{Code: Unavailable, Message: "diff evidence is unavailable"}
	}
	observation, ok := s.get(input.TargetRevision, session)
	if !ok {
		return LineRangeEvidence{}, &Error{Code: StaleTarget, Message: "diff observation is unavailable"}
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
	return LineRangeEvidence{Content: strings.Join(selected, "\n"), EndLine: input.EndLine}, nil
}
