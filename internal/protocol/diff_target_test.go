package protocol

import (
	"encoding/base64"
	"testing"
)

func testTargetReference() string { return testTargetReferenceFor("commit") }

func testTargetReferenceFor(value string) string {
	return "difftargetref_" + base64.RawURLEncoding.EncodeToString([]byte(value)) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
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
			{Reference: testTargetReferenceFor("commit-2"), TargetID: commitID, Kind: DiffTargetCommit, Base: DiffEndpoint{Kind: "empty_tree"}, Head: DiffEndpoint{Kind: "commit", OID: oid}, Metadata: DiffTargetMetadata{Label: "aaaaaaaaaaaa  root", Subject: "root", Abbreviated: "aaaaaaaaaaaa", CommittedAt: 1}},
		},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	catalog.Targets[1].Reference = catalog.Targets[0].Reference
	if err := catalog.Validate(); err == nil {
		t.Fatal("duplicate target reference was accepted")
	}
	catalog.Targets[1].Reference = testTargetReferenceFor("commit-2")
	catalog.Targets[1].Head.OID = "short"
	if err := catalog.Validate(); err == nil {
		t.Fatal("malformed full object id was accepted")
	}
}

func TestObserveDiffInputRequiresOpaqueReference(t *testing.T) {
	workspaceID := "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := (ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: testTargetReference(), ExpectedTargetID: "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: testTargetReference(), ExpectedTargetID: "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Cursor: "cursor"}).Validate(); err == nil {
		t.Fatal("continuation without expected revision was accepted")
	}
	if err := (ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: testTargetReference(), ExpectedTargetID: "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ExpectedTargetRevision: "diffrev_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}).Validate(); err == nil {
		t.Fatal("initial request with expected revision was accepted")
	}
	for _, reference := range []string{"main", "HEAD~1", "difftargetref_bad.bad"} {
		if err := (ObserveDiffInput{WorkspaceID: workspaceID, TargetReference: reference, ExpectedTargetID: "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}).Validate(); err == nil {
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
