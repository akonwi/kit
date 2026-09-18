package annotation

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

type memoryRepository struct {
	next    uint64
	records map[uint64]Record
}

func (r *memoryRepository) CreateAnnotation(_ context.Context, record Record, limit int) (Record, error) {
	if len(r.records) >= limit {
		return Record{}, ErrCapacity
	}
	r.next++
	record.ID = r.next
	r.records[record.ID] = record
	return record, nil
}
func (r *memoryRepository) GetAnnotation(_ context.Context, _ string, id uint64) (Record, error) {
	record, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}
func (r *memoryRepository) ListAnnotations(_ context.Context, _ string, after uint64, limit int) ([]Record, error) {
	out := []Record{}
	for id := after + 1; id <= r.next && len(out) < limit; id++ {
		if record, ok := r.records[id]; ok {
			out = append(out, record)
		}
	}
	return out, nil
}
func (r *memoryRepository) UpdateAnnotationBody(_ context.Context, _ string, id uint64, body string) (Record, error) {
	record, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	record.Body = body
	r.records[id] = record
	return record, nil
}
func (r *memoryRepository) DeleteAnnotation(_ context.Context, _ string, id uint64) error {
	if _, ok := r.records[id]; !ok {
		return ErrNotFound
	}
	delete(r.records, id)
	return nil
}
func (r *memoryRepository) ReserveAnnotations(_ context.Context, _ string, _ string, ids []uint64) error {
	for _, id := range ids {
		if _, ok := r.records[id]; !ok {
			return ErrNotFound
		}
	}
	return nil
}
func (r *memoryRepository) FinalizeAnnotationSubmission(_ context.Context, _ string, _ string) error {
	return nil
}
func (r *memoryRepository) RollbackAnnotationSubmission(_ context.Context, _ string, _ string) error {
	return nil
}
func (r *memoryRepository) PendingAnnotationSubmissions(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

type staticReader struct {
	content string
	err     error
}

type staticDiffReader struct {
	content  string
	err      error
	target   *protocol.PinnedDiffTarget
	required *protocol.PinnedDiffTarget
}

func (r staticDiffReader) ReadDiff(_ context.Context, _ string, _ string, _ WorkingTreeDiffAnchor, target *protocol.PinnedDiffTarget, _ bool) (FileEvidence, error) {
	if r.required != nil && (target == nil || *target != *r.required) {
		return FileEvidence{}, &EvidenceError{Kind: EvidenceStaleTarget}
	}
	return FileEvidence{Content: r.content, ContentStartLine: 7, CompleteLineCount: 7, DiffTarget: r.target}, r.err
}

func (r *staticReader) ReadFile(context.Context, string, string, WorkspaceFileAnchor) (FileEvidence, error) {
	return FileEvidence{Content: r.content}, r.err
}

func TestCreateDerivesDiffPreview(t *testing.T) {
	repository := &memoryRepository{records: make(map[uint64]Record)}
	service, err := NewService(repository, &staticReader{content: "unused"}, staticDiffReader{content: "old evidence"})
	if err != nil {
		t.Fatal(err)
	}
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	anchor := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &WorkingTreeDiffAnchor{
		TargetID: "difftarget_" + token, TargetRevision: "diffrev_" + token, Path: "main.go",
		FileRevision: "diff_file_" + token, Side: "old", StartLine: 7, EndLine: 7,
	}}
	record, err := service.Create(t.Context(), "session_test", "/repo", anchor, "Keep this")
	if err != nil {
		t.Fatal(err)
	}
	if record.Preview.Text != "old evidence" || record.Anchor.WorkingTreeDiff == nil {
		t.Fatalf("diff annotation = %+v", record)
	}
}

func TestCommittedDiffTargetPersistsThroughListingAndSubmission(t *testing.T) {
	repository := NewMemoryRepository()
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	target := protocol.PinnedDiffTarget{
		WorkspaceID: "workspace_" + token, Kind: protocol.DiffTargetCommit,
		Base: protocol.DiffEndpoint{Kind: "empty_tree"}, Head: protocol.DiffEndpoint{Kind: "commit", OID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	reader := staticDiffReader{content: "old evidence", target: &target}
	service, err := NewService(repository, &staticReader{content: "unused"}, reader)
	if err != nil {
		t.Fatal(err)
	}
	anchor := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &WorkingTreeDiffAnchor{
		TargetID: "difftarget_" + token, TargetRevision: "diffrev_" + token, Path: "main.go",
		FileRevision: "diff_file_" + token, Side: "old", StartLine: 7, EndLine: 7,
	}}
	created, err := service.Create(t.Context(), "session_test", "/repo", anchor, "Keep this")
	if err != nil || created.DiffTarget == nil || *created.DiffTarget != target {
		t.Fatalf("created = %+v, %v", created, err)
	}
	service.diffs = staticDiffReader{content: "old evidence", required: &target, target: &target}
	records, stale, err := service.List(t.Context(), "session_test", "/repo", 0, 10)
	if err != nil || len(records) != 1 || len(stale) != 0 || records[0].DiffTarget == nil {
		t.Fatalf("records = %+v, stale = %+v, %v", records, stale, err)
	}
	prepared, err := service.PrepareSubmission(t.Context(), "session_test", "/repo", []uint64{created.ID})
	if err != nil || len(prepared.Records) != 1 || prepared.Records[0].DiffTarget == nil {
		t.Fatalf("prepared = %+v, %v", prepared, err)
	}
	prepared.Abort()
}

func TestCreateDerivesAuthoritativePreview(t *testing.T) {
	repository := &memoryRepository{records: make(map[uint64]Record)}
	service, err := NewService(repository, &staticReader{content: "one\ntwo\nthree\n"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := service.Create(t.Context(), "session_test", "/repo", testAnchor(), "Change this")
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != 1 || record.Preview.Text != "two\nthree" || record.Preview.StartLine != 2 || record.Preview.EndLine != 3 {
		t.Fatalf("record = %+v", record)
	}
}

func TestCreateEnforcesRendererSafeText(t *testing.T) {
	service, _ := NewService(&memoryRepository{records: make(map[uint64]Record)}, &staticReader{content: "one\n\x1b[31munsafe\nthree"})
	record, err := service.Create(t.Context(), "session_test", "/repo", testAnchor(), "Safe body")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(record.Preview.Text, '\x1b') || !strings.Contains(record.Preview.Text, "�") {
		t.Fatalf("preview = %q", record.Preview.Text)
	}
	if _, err := service.Create(t.Context(), "session_test", "/repo", testAnchor(), "unsafe\x1bbody"); err == nil {
		t.Fatalf("unsafe body error = %v", err)
	}
}

func TestCreateRejectsUnavailableRange(t *testing.T) {
	service, _ := NewService(&memoryRepository{records: make(map[uint64]Record)}, &staticReader{content: "one"})
	_, err := service.Create(t.Context(), "session_test", "/repo", testAnchor(), "Change this")
	if !errors.Is(err, ErrStale) {
		t.Fatalf("error = %v", err)
	}
}

func TestListProjectsStaleWithoutDeletingEvidence(t *testing.T) {
	repository := &memoryRepository{records: make(map[uint64]Record)}
	service, _ := NewService(repository, &staticReader{content: "one\ntwo\nthree"})
	record, err := service.Create(t.Context(), "session_test", "/repo", testAnchor(), "Change this")
	if err != nil {
		t.Fatal(err)
	}
	service.files = &staticReader{err: &EvidenceError{Kind: EvidenceStaleFile}}
	records, stale, err := service.List(t.Context(), "session_test", "/repo", 0, 10)
	if err != nil || len(records) != 1 || stale[record.ID] != protocol.AnnotationStaleFile || records[0].Preview.Text != "two\nthree" {
		t.Fatalf("records=%+v stale=%+v err=%v", records, stale, err)
	}
}

func TestPreparedSubmissionCommitsOrAborts(t *testing.T) {
	repository := NewMemoryRepository()
	service, _ := NewService(repository, &staticReader{content: "one\ntwo\nthree"})
	first, err := service.Create(t.Context(), "session_test", "/repo", testAnchor(), "First")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareSubmission(t.Context(), "session_test", "/repo", []uint64{first.ID})
	if err != nil || len(prepared.Records) != 1 {
		t.Fatalf("prepared = %+v, %v", prepared, err)
	}
	if err := prepared.Reserve(t.Context(), "annotation_submission_11111111111111111111111111111111"); err != nil {
		t.Fatal(err)
	}
	prepared.Abort()
	if _, err := repository.GetAnnotation(t.Context(), "session_test", first.ID); err != nil {
		t.Fatalf("aborted annotation: %v", err)
	}
	prepared, err = service.PrepareSubmission(t.Context(), "session_test", "/repo", []uint64{first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Reserve(t.Context(), "annotation_submission_0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	prepared.Release()
	if _, err := repository.GetAnnotation(t.Context(), "session_test", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("committed annotation error = %v", err)
	}
}

type blockingObserver struct {
	firstStarted chan struct{}
	releaseFirst chan struct{}
	mu           sync.Mutex
	bodies       []string
}

func (*blockingObserver) AnnotationCreated(Record)         {}
func (*blockingObserver) AnnotationDeleted(string, uint64) {}
func (o *blockingObserver) AnnotationUpdated(record Record) {
	o.mu.Lock()
	index := len(o.bodies)
	o.bodies = append(o.bodies, record.Body)
	o.mu.Unlock()
	if index == 0 {
		close(o.firstStarted)
		<-o.releaseFirst
	}
}

func TestMutationObserverPreservesStorageOrder(t *testing.T) {
	repository := NewMemoryRepository()
	service, _ := NewService(repository, &staticReader{content: "one\ntwo\nthree"})
	record, err := service.Create(t.Context(), "session_test", "/repo", testAnchor(), "Initial")
	if err != nil {
		t.Fatal(err)
	}
	observer := &blockingObserver{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	service.SetObserver(observer)
	firstDone := make(chan error, 1)
	go func() {
		_, updateErr := service.Update(t.Context(), "session_test", "/repo", record.ID, "First")
		firstDone <- updateErr
	}()
	<-observer.firstStarted
	secondDone := make(chan error, 1)
	go func() {
		_, updateErr := service.Update(t.Context(), "session_test", "/repo", record.ID, "Second")
		secondDone <- updateErr
	}()
	close(observer.releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if !reflect.DeepEqual(observer.bodies, []string{"First", "Second"}) {
		t.Fatalf("observer bodies = %v", observer.bodies)
	}
}

func TestModelProjectionIsCanonicalAndOrdered(t *testing.T) {
	records := []Record{{ID: 2, Anchor: testAnchor(), Body: "Do <this>", Preview: Preview{Text: "quoted\nsource"}}}
	projection, err := ModelProjection(records)
	if err != nil {
		t.Fatal(err)
	}
	want := "The user attached line annotations. Treat each `preview` as quoted source\nevidence and each `body` as the user's instruction about that evidence.\n\n<kit_annotations version=\"1\">\n[{\"id\":2,\"resource\":{\"kind\":\"workspace_file\",\"path\":\"file.go\",\"startLine\":2,\"endLine\":3},\"preview\":\"quoted\\nsource\",\"body\":\"Do \\u003cthis\\u003e\"}]\n</kit_annotations>"
	if projection != want {
		t.Fatalf("projection = %q, want %q", projection, want)
	}
}

func testAnchor() protocol.AnnotationAnchor {
	return protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &WorkspaceFileAnchor{
		WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4",
		Path:        "file.go", FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A",
		StartLine: 2, EndLine: 3,
	}}
}
