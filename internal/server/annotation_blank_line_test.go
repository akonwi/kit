package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/httpapi"
	kitworkingdiff "github.com/akonwi/kit/internal/workingdiff"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

// annotationRouteService serves annotation creation from a real annotation
// service rooted at one working directory.
type annotationRouteService struct {
	sessionService
	annotations *kitannotation.Service
	cwd         string
}

func (s annotationRouteService) CreateAnnotation(ctx context.Context, sessionID string, input protocol.CreateAnnotationInput) (protocol.Annotation, error) {
	created, err := s.annotations.Create(ctx, sessionID, s.cwd, input.Anchor, input.Body)
	if err != nil {
		return protocol.Annotation{}, err
	}
	return projectAnnotation(created, ""), nil
}

// shortDiffReader returns authoritative evidence that no longer covers the
// requested range, as when the source shrank between observation and read.
type shortDiffReader struct{}

func (shortDiffReader) ReadDiff(_ context.Context, _, _ string, anchor kitannotation.WorkingTreeDiffAnchor, _ *protocol.PinnedDiffTarget, _ bool) (kitannotation.FileEvidence, error) {
	return kitannotation.FileEvidence{Content: "context\n", ContentStartLine: anchor.StartLine, CompleteLineCount: anchor.EndLine}, nil
}

func postAnnotation(t *testing.T, mux *http.ServeMux, sessionID string, anchor protocol.AnnotationAnchor) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(protocol.CreateAnnotationInput{Anchor: anchor, Body: "note"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+sessionID+"/annotations", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestCreateWorkingTreeAnnotationRangesEndingOnBlankLines(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	dir := t.TempDir()
	annotationGit(t, dir, "init", "-q")
	annotationGit(t, dir, "config", "user.name", "Test")
	annotationGit(t, dir, "config", "user.email", "test@example.com")
	readme := filepath.Join(dir, "README.md")
	base := "# Demo\n\nintro\n\ncontext\n"
	if err := os.WriteFile(readme, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	annotationGit(t, dir, "add", "README.md")
	annotationGit(t, dir, "commit", "-qm", "base")
	if err := os.WriteFile(readme, []byte(base+"\nadded one\nadded two\nadded three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaces := kitworkspace.NewService()
	diffs, err := kitworkingdiff.NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces.Ref(sessionID, dir).WorkspaceID
	page, err := diffs.Observe(t.Context(), sessionID, dir, protocol.ObserveWorkingTreeInput{WorkspaceID: workspaceID})
	if err != nil || len(page.Files) != 1 {
		t.Fatalf("observation = %+v, %v", page, err)
	}
	annotations, err := kitannotation.NewService(kitannotation.NewMemoryRepository(), annotationWorkspaceReader{service: workspaces}, annotationDiffReader{service: diffs})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerSessionRoutes(mux, annotationRouteService{annotations: annotations, cwd: dir})
	anchor := func(start, end int) protocol.AnnotationAnchor {
		return protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{
			TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "README.md",
			FileRevision: page.Files[0].FileRevision, Side: "new", StartLine: start, EndLine: end,
		}}
	}

	for _, test := range []struct {
		start, end int
		preview    string
	}{
		{5, 6, "context\n"}, {3, 4, "intro\n"}, {4, 6, "\ncontext\n"},
		{4, 5, "\ncontext"}, {6, 7, "\nadded one"}, {7, 9, "added one\nadded two\nadded three"},
	} {
		response := postAnnotation(t, mux, sessionID, anchor(test.start, test.end))
		if response.Code != http.StatusCreated {
			t.Fatalf("lines %d-%d response = %d %s", test.start, test.end, response.Code, response.Body.String())
		}
		var created protocol.Annotation
		if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		want := protocol.AnnotationPreview{StartLine: test.start, EndLine: test.end, Text: test.preview}
		if created.Preview != want {
			t.Fatalf("lines %d-%d preview = %+v, want %+v", test.start, test.end, created.Preview, want)
		}
	}

	if err := os.WriteFile(readme, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	response := postAnnotation(t, mux, sessionID, anchor(5, 6))
	wantStale := `{"error":{"code":"stale_file","message":"the file changed; refresh it and try again"}}`
	if response.Code != http.StatusConflict || strings.TrimSpace(response.Body.String()) != wantStale {
		t.Fatalf("edited file response = %d %s", response.Code, response.Body.String())
	}

	read, err := workspaces.Read(t.Context(), sessionID, dir, protocol.ReadWorkspaceFileInput{WorkspaceID: workspaceID, Path: "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	response = postAnnotation(t, mux, sessionID, protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
		WorkspaceID: workspaceID, Path: "README.md", FileRevision: read.Revision, StartLine: 3, EndLine: 4,
	}})
	var fileAnnotation protocol.Annotation
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &fileAnnotation) != nil ||
		fileAnnotation.Preview != (protocol.AnnotationPreview{StartLine: 3, EndLine: 4, Text: "intro\n"}) {
		t.Fatalf("workspace file response = %d %s", response.Code, response.Body.String())
	}

	shortAnnotations, err := kitannotation.NewService(kitannotation.NewMemoryRepository(), annotationWorkspaceReader{service: workspaces}, shortDiffReader{})
	if err != nil {
		t.Fatal(err)
	}
	shortMux := http.NewServeMux()
	registerSessionRoutes(shortMux, annotationRouteService{annotations: shortAnnotations, cwd: dir})
	response = postAnnotation(t, shortMux, sessionID, anchor(5, 6))
	if response.Code != http.StatusConflict || strings.TrimSpace(response.Body.String()) != wantStale {
		t.Fatalf("stale range response = %d %s", response.Code, response.Body.String())
	}
}

func TestAnnotationAPIErrorProjectsUnclassifiedStaleAnchorsAsConflicts(t *testing.T) {
	err := annotationAPIError(fmt.Errorf("%w: annotation 7: %w", kitannotation.ErrStale, errors.New("evidence read failed")))
	var apiErr *httpapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict || apiErr.Code != string(httpapi.ErrorConflict) ||
		apiErr.Message != "the annotation is out of date; refresh the file or diff and try again" {
		t.Fatalf("stale anchor error = %#v", err)
	}
}
