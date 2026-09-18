package protocol

import (
	"encoding/base64"
	"testing"
)

func testTargetReference() string {
	return "difftargetref_" + base64.RawURLEncoding.EncodeToString([]byte(`{"k":"commit"}`)) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
}

func TestDiffTargetCatalogValidation(t *testing.T) {
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	workspaceID := "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	workingID := "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	commitID := "difftarget_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	catalog := DiffTargetCatalog{
		SessionID: "session_test", WorkspaceID: workspaceID, Diagnostics: []DiffTargetDiagnostic{},
		Targets: []DiffTargetEntry{
			{Reference: testTargetReference(), TargetID: workingID, Kind: DiffTargetWorkingTree, Base: DiffEndpoint{Kind: "commit", OID: oid}, Head: DiffEndpoint{Kind: "commit", OID: oid}, Metadata: DiffTargetMetadata{Label: "Working tree"}},
			{Reference: testTargetReference(), TargetID: commitID, Kind: DiffTargetCommit, Base: DiffEndpoint{Kind: "empty_tree"}, Head: DiffEndpoint{Kind: "commit", OID: oid}, Metadata: DiffTargetMetadata{Label: "aaaaaaaaaaaa  root", Subject: "root", Abbreviated: "aaaaaaaaaaaa", CommittedAt: 1}},
		},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	catalog.Targets[1].Head.OID = "short"
	if err := catalog.Validate(); err == nil {
		t.Fatal("malformed full object id was accepted")
	}
}

func TestObserveDiffInputRequiresOpaqueReference(t *testing.T) {
	workspaceID := "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := (ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: testTargetReference()}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"main", "HEAD~1", "difftargetref_bad.bad"} {
		if err := (ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: reference}).Validate(); err == nil {
			t.Fatalf("reference %q was accepted", reference)
		}
	}
}

func TestCommittedObservationRequiresPinnedEndpoints(t *testing.T) {
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	observation := DiffObservation{SessionID: "session_test", Target: DiffTarget{ID: "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", WorkspaceID: "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Kind: DiffTargetCommit, Base: DiffEndpoint{Kind: "empty_tree"}, Head: DiffEndpoint{Kind: "commit", OID: oid}}, Revision: "diffrev_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Head: DiffHead{State: "commit", OID: oid}, IndexSummary: "clean", Complete: true, Omissions: []DiffOmission{}}
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
	observation.Head.OID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := observation.Validate(); err == nil {
		t.Fatal("endpoint mismatch was accepted")
	}
}
