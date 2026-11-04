package workingdiff

import (
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/workspace"
)

type evidenceRangeCase struct {
	name       string
	side       string
	start, end int
	content    string
	preview    string
}

// evidenceFixture commits base, writes or commits head, and returns the
// observed diff for path. Committed fixtures observe the head commit.
func evidenceFixture(t *testing.T, path, base, head string, committed bool) (string, *Service, protocol.DiffObservation, protocol.DiffFileSummary) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, path), []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", path)
	git(t, dir, "commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(dir, path), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaces := workspace.NewService()
	service, err := NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces.Ref("session_test", dir).WorkspaceID
	if !committed {
		page, err := service.Observe(t.Context(), "session_test", dir, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaceID})
		if err != nil || len(page.Files) != 1 {
			t.Fatalf("working tree observation = %+v, %v", page, err)
		}
		return dir, service, page.Observation, page.Files[0]
	}
	git(t, dir, "add", path)
	git(t, dir, "commit", "-qm", "head")
	catalog, err := service.ListTargets(t.Context(), "session_test", dir, protocol.ListDiffTargetsInput{WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	target := findTarget(t, catalog, protocol.DiffTargetCommit, func(entry protocol.DiffTargetEntry) bool { return entry.Metadata.Subject == "head" })
	page, err := service.ObserveTarget(t.Context(), "session_test", dir, protocol.ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: target.Reference, ExpectedTargetID: target.TargetID})
	if err != nil || len(page.Files) != 1 {
		t.Fatalf("committed observation = %+v, %v", page, err)
	}
	return dir, service, page.Observation, page.Files[0]
}

// checkEvidenceRanges asserts both the raw newline-terminated evidence and the
// preview an annotation derives from it for each range.
func checkEvidenceRanges(t *testing.T, path, base, head string, cases []evidenceRangeCase) {
	t.Helper()
	for _, committed := range []bool{false, true} {
		dir, service, observation, file := evidenceFixture(t, path, base, head, committed)
		annotations, err := kitannotation.NewService(kitannotation.NewMemoryRepository(), reconstructionFileReader{}, reconstructionDiffReader{service: service})
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range cases {
			input := LineRangeInput{
				TargetID: observation.Target.ID, TargetRevision: observation.Revision, Path: file.Path,
				FileRevision: file.FileRevision, Side: test.side, StartLine: test.start, EndLine: test.end, DeriveTarget: true,
			}
			anchor := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{
				TargetID: observation.Target.ID, TargetRevision: observation.Revision, Path: file.Path,
				FileRevision: file.FileRevision, Side: test.side, StartLine: test.start, EndLine: test.end,
			}}
			created, err := annotations.Create(t.Context(), "session_test", dir, anchor, "note")
			if err != nil {
				t.Fatalf("committed=%v %s create: %v", committed, test.name, err)
			}
			want := kitannotation.Preview{StartLine: test.start, EndLine: test.end, Text: test.preview}
			if created.Preview != want {
				t.Fatalf("committed=%v %s preview = %+v, want %+v", committed, test.name, created.Preview, want)
			}
			evidence, err := service.ReadLineRange(t.Context(), "session_test", dir, input)
			if err != nil || evidence.Content != test.content || evidence.EndLine != test.end {
				t.Fatalf("committed=%v %s evidence = %+v, %v; want content %q", committed, test.name, evidence, err, test.content)
			}
		}
	}
}

func TestLineRangeEvidenceKeepsBlankLinesAtEitherEdgeOfARange(t *testing.T) {
	base := "# Demo\n\nintro\n\ncontext\n"
	head := base + "\nadded one\nadded two\nadded three\n"
	checkEvidenceRanges(t, "README.md", base, head, []evidenceRangeCase{
		{name: "ends on added blank", side: "new", start: 5, end: 6, content: "context\n\n", preview: "context\n"},
		{name: "ends on context blank", side: "new", start: 3, end: 4, content: "intro\n\n", preview: "intro\n"},
		{name: "spans into added blank", side: "new", start: 4, end: 6, content: "\ncontext\n\n", preview: "\ncontext\n"},
		{name: "starts on context blank", side: "new", start: 4, end: 5, content: "\ncontext\n", preview: "\ncontext"},
		{name: "starts on added blank", side: "new", start: 6, end: 7, content: "\nadded one\n", preview: "\nadded one"},
		{name: "single blank", side: "new", start: 6, end: 6, content: "\n", preview: ""},
		{name: "hunk end", side: "new", start: 7, end: 9, content: "added one\nadded two\nadded three\n", preview: "added one\nadded two\nadded three"},
		{name: "old side ends on blank", side: "old", start: 3, end: 4, content: "intro\n\n", preview: "intro\n"},
	})
}

func TestLineRangeEvidenceKeepsATrailingBlankLineAtEndOfFile(t *testing.T) {
	base := "one\ntwo\n"
	head := "one\ntwo\n\n"
	checkEvidenceRanges(t, "a.txt", base, head, []evidenceRangeCase{
		{name: "hunk start through trailing blank", side: "new", start: 1, end: 3, content: "one\ntwo\n\n", preview: "one\ntwo\n"},
		{name: "trailing blank alone", side: "new", start: 3, end: 3, content: "\n", preview: ""},
	})
}

func TestLineRangeEvidenceTerminatesALastLineWithoutANewline(t *testing.T) {
	base := "one\ntwo\n"
	head := "one\ntwo\n\nthree"
	checkEvidenceRanges(t, "a.txt", base, head, []evidenceRangeCase{
		{name: "blank then unterminated", side: "new", start: 3, end: 4, content: "\nthree\n", preview: "\nthree"},
		{name: "unterminated alone", side: "new", start: 4, end: 4, content: "three\n", preview: "three"},
	})
}

func TestLineRangeEvidencePreviewsCRLFLinesWithoutCarriageReturns(t *testing.T) {
	base := "one\r\ntwo\r\n"
	head := "one\r\ntwo\r\n\r\nthree\r\n"
	checkEvidenceRanges(t, "a.txt", base, head, []evidenceRangeCase{
		{name: "ends on CRLF blank", side: "new", start: 2, end: 3, content: "two\r\n\r\n", preview: "two\n"},
		{name: "starts on CRLF blank", side: "new", start: 3, end: 4, content: "\r\nthree\r\n", preview: "\nthree"},
	})
}
