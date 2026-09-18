package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"github.com/akonwi/kit/internal/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type diffRouteService struct {
	sessionService
	catalog protocol.DiffTargetCatalog
	observe protocol.WorkingTreePage
	diff    protocol.FileDiffPage
	err     error
}

func (s diffRouteService) ListDiffTargets(context.Context, string, protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	return s.catalog, s.err
}
func (s diffRouteService) ObserveDiff(context.Context, string, protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	return s.observe, s.err
}
func (s diffRouteService) ObserveWorkingTree(context.Context, string, protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	return s.observe, s.err
}
func (s diffRouteService) ReadFileDiff(context.Context, string, protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	return s.diff, s.err
}
func routeObservation() protocol.DiffObservation {
	return protocol.DiffObservation{SessionID: "session_test", Target: protocol.DiffTarget{ID: "difftarget_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", WorkspaceID: "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Kind: "working_tree", RepositoryPath: ""}, Revision: "diffrev_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Head: protocol.DiffHead{State: "unborn"}, IndexSummary: "clean", Complete: true, Omissions: []protocol.DiffOmission{}}
}
func routeTargetReference() string {
	return "difftargetref_" + base64.RawURLEncoding.EncodeToString([]byte(`{"k":"working_tree"}`)) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
}

func TestGeneralizedDiffRoutes(t *testing.T) {
	observation := routeObservation()
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	endpoint := protocol.DiffEndpoint{Kind: "commit", OID: oid}
	entry := protocol.DiffTargetEntry{Reference: routeTargetReference(), TargetID: observation.Target.ID, Kind: protocol.DiffTargetWorkingTree, Base: endpoint, Head: endpoint, Metadata: protocol.DiffTargetMetadata{Label: "Working tree"}}
	catalog := protocol.DiffTargetCatalog{SessionID: observation.SessionID, WorkspaceID: observation.Target.WorkspaceID, Targets: []protocol.DiffTargetEntry{entry}, Diagnostics: []protocol.DiffTargetDiagnostic{}}
	page := protocol.WorkingTreePage{Observation: observation, Files: []protocol.DiffFileSummary{}}
	mux := http.NewServeMux()
	registerSessionRoutes(mux, diffRouteService{catalog: catalog, observe: page})
	for path, body := range map[string]string{
		"/v1/sessions/session_test/diff/targets":      `{"workspaceId":"` + observation.Target.WorkspaceID + `"}`,
		"/v1/sessions/session_test/diff/observations": `{"workspaceId":"` + observation.Target.WorkspaceID + `","targetReference":"` + entry.Reference + `","expectedTargetId":"` + entry.TargetID + `"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s response=%d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestGeneralizedDiffClientResponsesRequireRequestedIdentity(t *testing.T) {
	observation := routeObservation()
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	endpoint := protocol.DiffEndpoint{Kind: "commit", OID: oid}
	entry := protocol.DiffTargetEntry{Reference: routeTargetReference(), TargetID: observation.Target.ID, Kind: protocol.DiffTargetWorkingTree, Base: endpoint, Head: endpoint, Metadata: protocol.DiffTargetMetadata{Label: "Working tree"}}
	catalog := protocol.DiffTargetCatalog{SessionID: observation.SessionID, WorkspaceID: observation.Target.WorkspaceID, Targets: []protocol.DiffTargetEntry{entry}, Diagnostics: []protocol.DiffTargetDiagnostic{}}
	catalogInput := protocol.ListDiffTargetsInput{WorkspaceID: observation.Target.WorkspaceID}
	if err := validateDiffTargetCatalogResponse(observation.SessionID, catalogInput, catalog); err != nil {
		t.Fatal(err)
	}
	catalog.SessionID = "other_session"
	if err := validateDiffTargetCatalogResponse(observation.SessionID, catalogInput, catalog); err == nil {
		t.Fatal("catalog session mismatch was accepted")
	}
	page := protocol.DiffPage{Observation: observation, Files: []protocol.DiffFileSummary{}}
	observeInput := protocol.ObserveDiffInput{WorkspaceID: observation.Target.WorkspaceID, TargetReference: entry.Reference, ExpectedTargetID: entry.TargetID}
	if err := validateObserveDiffResponse(observation.SessionID, observeInput, page); err != nil {
		t.Fatal(err)
	}
	page.Observation.Target.WorkspaceID = "workspace_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := validateObserveDiffResponse(observation.SessionID, observeInput, page); err == nil {
		t.Fatal("observation workspace mismatch was accepted")
	}
}

func TestGeneralizedDiffRoutesRejectServiceIdentityMismatch(t *testing.T) {
	observation := routeObservation()
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	endpoint := protocol.DiffEndpoint{Kind: "commit", OID: oid}
	entry := protocol.DiffTargetEntry{Reference: routeTargetReference(), TargetID: observation.Target.ID, Kind: protocol.DiffTargetWorkingTree, Base: endpoint, Head: endpoint, Metadata: protocol.DiffTargetMetadata{Label: "Working tree"}}
	catalog := protocol.DiffTargetCatalog{SessionID: "other_session", WorkspaceID: observation.Target.WorkspaceID, Targets: []protocol.DiffTargetEntry{entry}, Diagnostics: []protocol.DiffTargetDiagnostic{}}
	mux := http.NewServeMux()
	registerSessionRoutes(mux, diffRouteService{catalog: catalog})
	body := `{"workspaceId":"` + observation.Target.WorkspaceID + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/diff/targets", strings.NewReader(body))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("catalog response=%d %s", response.Code, response.Body.String())
	}

	observation.Target.ID = "difftarget_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	page := protocol.DiffPage{Observation: observation, Files: []protocol.DiffFileSummary{}}
	mux = http.NewServeMux()
	registerSessionRoutes(mux, diffRouteService{observe: page})
	body = `{"workspaceId":"` + observation.Target.WorkspaceID + `","targetReference":"` + entry.Reference + `","expectedTargetId":"` + entry.TargetID + `"}`
	request = httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/diff/observations", strings.NewReader(body))
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("observation response=%d %s", response.Code, response.Body.String())
	}
}

func TestFileDiffClientResponseRequiresRequestedIdentity(t *testing.T) {
	observation := routeObservation()
	file := protocol.DiffFileSummary{
		Path: "main.go", FileRevision: "diff_file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Change: "modified",
		Old: protocol.DiffSide{Kind: "regular", Mode: 0100644}, New: protocol.DiffSide{Kind: "regular", Mode: 0100644}, ContentState: "text",
	}
	page := protocol.FileDiffPage{Observation: observation, File: file, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{}}
	input := protocol.ReadFileDiffInput{TargetID: observation.Target.ID, TargetRevision: observation.Revision, Path: file.Path, ExpectedFileRevision: file.FileRevision}
	if err := validateFileDiffResponse(observation.SessionID, input, page); err != nil {
		t.Fatalf("matching response: %v", err)
	}
	page.Observation.Revision = "diffrev_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := validateFileDiffResponse(observation.SessionID, input, page); err == nil {
		t.Fatal("mismatched target revision was accepted")
	}
}

func TestDiffRoutesValidateAndProjectTypedErrors(t *testing.T) {
	mux := http.NewServeMux()
	registerSessionRoutes(mux, diffRouteService{err: &protocol.DiffError{Code: protocol.DiffErrorUnsupportedRepository, Message: "repository is unsupported", Details: map[string]string{"reason": "sparse_index"}}})
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/diff/working-tree", strings.NewReader(`{"workspaceId":"workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"code":"unsupported_repository"`) {
		t.Fatalf("response=%d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/diff/working-tree", strings.NewReader(`{"workspaceId":"bad","unknown":true}`))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestDiffRoutesPreserveNullableCoordinates(t *testing.T) {
	o := routeObservation()
	old := 1
	file := protocol.DiffFileSummary{Path: "a", FileRevision: "diff_file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Change: "deleted", Old: protocol.DiffSide{Kind: "regular", Mode: 0100644}, New: protocol.DiffSide{Kind: "absent"}, ContentState: "text"}
	page := protocol.FileDiffPage{Observation: o, File: file, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{{OldStart: 1, OldCount: 1, NewStart: 0, NewCount: 0, Lines: []protocol.DiffLine{{Kind: "deletion", OldLine: &old, Content: "x", HasTerminatingLF: false}}}}}
	mux := http.NewServeMux()
	registerSessionRoutes(mux, diffRouteService{diff: page})
	body := `{"targetId":"` + o.Target.ID + `","targetRevision":"` + o.Revision + `","path":"a"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/diff/files/read", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("response=%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"newLine"`) {
		t.Fatalf("nullable coordinate leaked sentinel: %s", w.Body.String())
	}
}
func TestRuntimeDiffUnavailableWithoutService(t *testing.T) {
	_, e := (runtimeSessionService{}).ObserveWorkingTree(t.Context(), "session", protocol.ObserveWorkingTreeInput{})
	var de *protocol.DiffError
	if !errors.As(e, &de) || de.Code != protocol.DiffErrorUnavailable {
		t.Fatalf("error=%v", e)
	}
}
