package protocol

import (
	"encoding/json"
	"testing"
)

const testWorkspace = "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const testTarget = "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const testRevision = "diffrev_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const testFileRevision = "diff_file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func validObservation() DiffObservation {
	return DiffObservation{SessionID: "session_test", Target: DiffTarget{ID: testTarget, WorkspaceID: testWorkspace, Kind: "working_tree", RepositoryPath: ""}, Revision: testRevision, Head: DiffHead{State: "unborn"}, IndexSummary: "clean", Complete: true, Omissions: []DiffOmission{}}
}
func TestDiffProtocolExactValidation(t *testing.T) {
	in := ObserveWorkingTreeInput{WorkspaceID: testWorkspace, PageSize: 200}
	if e := in.Validate(); e != nil {
		t.Fatal(e)
	}
	in.PageSize = 201
	if e := in.Validate(); e == nil {
		t.Fatal("accepted oversized page")
	}
	read := ReadFileDiffInput{TargetID: testTarget, TargetRevision: testRevision, Path: "a.txt", ExpectedFileRevision: testFileRevision, PageSize: 1000, MaxHunks: 20}
	if e := read.Validate(); e != nil {
		t.Fatal(e)
	}
	read.Path = "../a"
	if e := read.Validate(); e == nil {
		t.Fatal("accepted escaping path")
	}
	o := validObservation()
	if e := o.Validate(); e != nil {
		t.Fatal(e)
	}
	o.Head = DiffHead{State: "commit"}
	if e := o.Validate(); e == nil {
		t.Fatal("accepted commit without oid")
	}
}
func TestDiffLineCoordinatesAndWireValues(t *testing.T) {
	old, newLine := 1, 1
	p := FileDiffPage{Observation: validObservation(), File: DiffFileSummary{Path: "a", FileRevision: testFileRevision, Change: "modified", Old: DiffSide{Kind: "regular", Mode: 0100644}, New: DiffSide{Kind: "regular", Mode: 0100644}, ContentState: "text"}, Computation: DiffComputation{State: "complete"}, Hunks: []DiffHunk{{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1, Lines: []DiffLine{{Kind: "context", OldLine: &old, NewLine: &newLine, Content: "x", HasTerminatingLF: false}}}}}
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
	p.Hunks[0].Lines[0].NewLine = nil
	if e := p.Validate(); e == nil {
		t.Fatal("accepted invalid context coordinates")
	}
	b, e := json.Marshal(DiffLine{Kind: "deletion", OldLine: &old, Content: "x", HasTerminatingLF: true})
	if e != nil {
		t.Fatal(e)
	}
	if string(b) != "{\"kind\":\"deletion\",\"oldLine\":1,\"content\":\"x\",\"hasTerminatingLF\":true}" {
		t.Fatalf("wire=%s", b)
	}
}
func TestDiffErrorClosedDetails(t *testing.T) {
	e := DiffError{Code: DiffErrorUnsupportedRepository, Message: "unsupported", Details: map[string]string{"reason": "sparse_index"}}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Details["reason"] = "whatever"
	if err := e.Validate(); err == nil {
		t.Fatal("accepted open reason")
	}
	e = DiffError{Code: DiffErrorStaleFile, Message: "stale", Details: map[string]string{"path": "secret"}}
	if err := e.Validate(); err == nil {
		t.Fatal("accepted unsafe detail")
	}
}
