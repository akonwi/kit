// Package annotation owns session-scoped draft annotations and guarded evidence previews.
package annotation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
)

var (
	ErrNotFound = errors.New("annotation not found")
	ErrCapacity = errors.New("annotation capacity exceeded")
	ErrStale    = errors.New("annotation anchor is stale")
)

// WorkspaceFileAnchor pins a line range to one workspace file observation.
type WorkspaceFileAnchor struct {
	WorkspaceID, Path, FileRevision string
	StartLine, EndLine              int
}

// Preview is frozen server-derived evidence.
type Preview struct {
	StartLine, EndLine int
	Text               string
	Truncated          bool
}

// Record is one live session-owned annotation draft.
type Record struct {
	ID        uint64
	SessionID string
	Anchor    WorkspaceFileAnchor
	Body      string
	Preview   Preview
}

// FileEvidence is the guarded file projection required to derive a preview.
type FileEvidence struct {
	Content           string
	ContentStartLine  int
	CompleteLineCount int
	Truncated         bool
}

// EvidenceErrorKind classifies authoritative reader failures without coupling
// annotation logic to a concrete workspace implementation.
type EvidenceErrorKind string

const (
	EvidenceStaleWorkspace EvidenceErrorKind = "stale_workspace"
	EvidenceStaleFile      EvidenceErrorKind = "stale_file"
	EvidenceUnavailable    EvidenceErrorKind = "unavailable"
	EvidencePermission     EvidenceErrorKind = "permission_denied"
	EvidenceInvalid        EvidenceErrorKind = "invalid_evidence"
	EvidenceLimit          EvidenceErrorKind = "limit_exceeded"
)

// EvidenceError is a bounded annotation-facing resource-read failure.
type EvidenceError struct{ Kind EvidenceErrorKind }

func (e *EvidenceError) Error() string { return "annotation evidence is " + string(e.Kind) }

// FileReader validates workspace identity and file revision while reading evidence.
type FileReader interface {
	ReadFile(context.Context, string, string, WorkspaceFileAnchor) (FileEvidence, error)
}

// Repository persists live annotations and their per-session monotonic sequence.
type Repository interface {
	CreateAnnotation(context.Context, Record, int) (Record, error)
	GetAnnotation(context.Context, string, uint64) (Record, error)
	ListAnnotations(context.Context, string, uint64, int) ([]Record, error)
	UpdateAnnotationBody(context.Context, string, uint64, string) (Record, error)
	DeleteAnnotation(context.Context, string, uint64) error
	ReserveAnnotations(context.Context, string, string, []uint64) error
	FinalizeAnnotationSubmission(context.Context, string, string) error
	RollbackAnnotationSubmission(context.Context, string, string) error
	PendingAnnotationSubmissions(context.Context, string) ([]string, error)
}

// Observer receives committed mutations while the per-session lock remains held.
type Observer interface {
	AnnotationCreated(Record)
	AnnotationUpdated(Record)
	AnnotationDeleted(string, uint64)
}

// Service validates evidence and serializes mutations for each session.
type Service struct {
	repository Repository
	memory     *MemoryRepository
	files      FileReader
	locksMu    sync.Mutex
	locks      map[string]*sync.Mutex
	temporary  map[string]struct{}
	observerMu sync.RWMutex
	observer   Observer
}

// NewService constructs an annotation service.
func NewService(repository Repository, files FileReader) (*Service, error) {
	if repository == nil || files == nil {
		return nil, fmt.Errorf("annotation repository and file reader are required")
	}
	return &Service{repository: repository, memory: NewMemoryRepository(), files: files, locks: make(map[string]*sync.Mutex), temporary: make(map[string]struct{})}, nil
}

// SetObserver installs the authoritative mutation event sink.
func (s *Service) SetObserver(observer Observer) {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	s.observer = observer
}

func (s *Service) currentObserver() Observer {
	s.observerMu.RLock()
	defer s.observerMu.RUnlock()
	return s.observer
}

// SetTemporary routes one process-local session to volatile annotation storage.
func (s *Service) SetTemporary(sessionID string) {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	s.temporary[sessionID] = struct{}{}
}

// ForgetSession removes process-local annotation state and routing.
func (s *Service) ForgetSession(sessionID string) {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	s.memory.DeleteSession(sessionID)
	s.locksMu.Lock()
	delete(s.temporary, sessionID)
	delete(s.locks, sessionID)
	s.locksMu.Unlock()
}

func (s *Service) repositoryFor(sessionID string) Repository {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if _, temporary := s.temporary[sessionID]; temporary {
		return s.memory
	}
	return s.repository
}

func (s *Service) sessionLock(sessionID string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	lock := s.locks[sessionID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[sessionID] = lock
	}
	return lock
}

// Create derives a preview and atomically allocates the next session ID.
func (s *Service) Create(ctx context.Context, sessionID, cwd string, anchor WorkspaceFileAnchor, body string) (Record, error) {
	if err := validateDraft(sessionID, anchor, body); err != nil {
		return Record{}, err
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	evidence, err := s.files.ReadFile(ctx, sessionID, cwd, anchor)
	if err != nil {
		return Record{}, err
	}
	preview, err := derivePreview(evidence, anchor)
	if err != nil {
		return Record{}, err
	}
	created, err := s.repositoryFor(sessionID).CreateAnnotation(ctx, Record{SessionID: sessionID, Anchor: anchor, Body: body, Preview: preview}, protocol.MaxLiveAnnotationsPerSession)
	if err == nil {
		if observer := s.currentObserver(); observer != nil {
			observer.AnnotationCreated(created)
		}
	}
	return created, err
}

// Update replaces only the body of an existing live annotation.
func (s *Service) Update(ctx context.Context, sessionID, cwd string, id uint64, body string) (Record, error) {
	if id == 0 || !validBody(body) {
		return Record{}, fmt.Errorf("annotation update is invalid")
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	repository := s.repositoryFor(sessionID)
	record, err := repository.GetAnnotation(ctx, sessionID, id)
	if err != nil {
		return Record{}, err
	}
	if _, err := s.files.ReadFile(ctx, sessionID, cwd, record.Anchor); err != nil {
		return Record{}, fmt.Errorf("%w: annotation %d: %w", ErrStale, id, err)
	}
	updated, err := repository.UpdateAnnotationBody(ctx, sessionID, id, body)
	if err == nil {
		if observer := s.currentObserver(); observer != nil {
			observer.AnnotationUpdated(updated)
		}
	}
	return updated, err
}

// Delete permanently removes one live annotation.
func (s *Service) Delete(ctx context.Context, sessionID string, id uint64) error {
	if id == 0 {
		return fmt.Errorf("annotation id is invalid")
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	err := s.repositoryFor(sessionID).DeleteAnnotation(ctx, sessionID, id)
	if err == nil {
		if observer := s.currentObserver(); observer != nil {
			observer.AnnotationDeleted(sessionID, id)
		}
	}
	return err
}

// List returns live annotations and their current stale projections.
func (s *Service) List(ctx context.Context, sessionID, cwd string, after uint64, limit int) ([]Record, map[uint64]protocol.AnnotationStaleReason, error) {
	if limit <= 0 || limit > protocol.MaxAnnotationPageSize+1 {
		return nil, nil, fmt.Errorf("annotation page size is invalid")
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	records, err := s.repositoryFor(sessionID).ListAnnotations(ctx, sessionID, after, limit)
	if err != nil {
		return nil, nil, err
	}
	stale := make(map[uint64]protocol.AnnotationStaleReason)
	for _, record := range records {
		if _, readErr := s.files.ReadFile(ctx, sessionID, cwd, record.Anchor); readErr != nil {
			stale[record.ID] = staleReason(readErr)
		}
	}
	return records, stale, nil
}

// PreparedSubmission holds the per-session mutation lock while a caller admits
// a message. Callers must invoke Commit or Abort exactly once.
type PreparedSubmission struct {
	service      *Service
	sessionID    string
	ids          []uint64
	Records      []Record
	lock         *sync.Mutex
	submissionID string
	reserved     bool
	finalized    bool
	done         bool
}

// PrepareSubmission loads annotations in caller order, revalidates every anchor,
// and prevents concurrent mutation until the caller commits or aborts.
func (s *Service) PrepareSubmission(ctx context.Context, sessionID, cwd string, ids []uint64) (*PreparedSubmission, error) {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	result := make([]Record, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			lock.Unlock()
			return nil, fmt.Errorf("annotation id is invalid")
		}
		if _, duplicate := seen[id]; duplicate {
			lock.Unlock()
			return nil, fmt.Errorf("annotation ids must be unique")
		}
		seen[id] = struct{}{}
		record, err := s.repositoryFor(sessionID).GetAnnotation(ctx, sessionID, id)
		if err != nil {
			lock.Unlock()
			return nil, err
		}
		if _, err := s.files.ReadFile(ctx, sessionID, cwd, record.Anchor); err != nil {
			lock.Unlock()
			return nil, fmt.Errorf("%w: annotation %d: %w", ErrStale, id, err)
		}
		result = append(result, record)
	}
	return &PreparedSubmission{service: s, sessionID: sessionID, ids: append([]uint64(nil), ids...), Records: result, lock: lock}, nil
}

// Reserve durably hides the prepared drafts under an idempotency identity.
// The session mutation lock remains held until Commit or Abort.
func (p *PreparedSubmission) Reserve(ctx context.Context, submissionID string) error {
	if p == nil || p.done || p.reserved || !strings.HasPrefix(submissionID, "annotation_submission_") {
		return fmt.Errorf("annotation submission reservation is invalid")
	}
	if err := p.service.repositoryFor(p.sessionID).ReserveAnnotations(ctx, p.sessionID, submissionID, p.ids); err != nil {
		return err
	}
	p.submissionID = submissionID
	p.reserved = true
	return nil
}

// Commit finalizes a reserved submission with a detached cleanup context. The
// mutation lock remains held until Release so event publication can preserve
// mutation order. A failed cleanup remains recoverable by identity.
func (p *PreparedSubmission) Commit() error {
	if p == nil || p.done || !p.reserved || p.finalized {
		return fmt.Errorf("annotation submission is not reserved")
	}
	p.finalized = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.service.repositoryFor(p.sessionID).FinalizeAnnotationSubmission(ctx, p.sessionID, p.submissionID)
}

// Release closes a committed submission after its ordered event is published.
func (p *PreparedSubmission) Release() {
	if p == nil || p.done {
		return
	}
	p.done = true
	p.lock.Unlock()
}

// Abort restores a reserved submission, if any, and releases the mutation lock.
func (p *PreparedSubmission) Abort() {
	if p == nil || p.done {
		return
	}
	p.done = true
	defer p.lock.Unlock()
	if p.reserved && !p.finalized {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.service.repositoryFor(p.sessionID).RollbackAnnotationSubmission(ctx, p.sessionID, p.submissionID)
	}
}

func validateDraft(sessionID string, anchor WorkspaceFileAnchor, body string) error {
	projected := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
		WorkspaceID: anchor.WorkspaceID, Path: anchor.Path, FileRevision: anchor.FileRevision,
		StartLine: anchor.StartLine, EndLine: anchor.EndLine,
	}}
	if sessionID == "" || projected.Validate() != nil || !validBody(body) {
		return fmt.Errorf("annotation draft is invalid")
	}
	return nil
}

func validBody(body string) bool {
	if strings.TrimSpace(body) == "" || len(body) > protocol.MaxAnnotationBodyBytes || !utf8.ValidString(body) {
		return false
	}
	for _, character := range body {
		if character != '\n' && character != '\t' && (unicode.IsControl(character) || unicode.Is(unicode.Cf, character)) {
			return false
		}
	}
	return true
}

func derivePreview(evidence FileEvidence, anchor WorkspaceFileAnchor) (Preview, error) {
	lines := strings.Split(evidence.Content, "\n")
	if strings.HasSuffix(evidence.Content, "\n") {
		lines = lines[:len(lines)-1]
	}
	contentStart := evidence.ContentStartLine
	if contentStart == 0 {
		contentStart = 1
	}
	completeLines := evidence.CompleteLineCount
	if completeLines == 0 && !evidence.Truncated {
		completeLines = contentStart + len(lines) - 1
	}
	startIndex := anchor.StartLine - contentStart
	endIndex := anchor.EndLine - contentStart + 1
	if startIndex < 0 || endIndex > len(lines) || anchor.EndLine > completeLines {
		return Preview{}, fmt.Errorf("%w: selected lines are unavailable", ErrStale)
	}
	text := sanitizeEvidence(strings.Join(lines[startIndex:endIndex], "\n"))
	truncated := false
	if len(text) > protocol.MaxAnnotationPreviewBytes {
		text = text[:protocol.MaxAnnotationPreviewBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		truncated = true
	}
	return Preview{StartLine: anchor.StartLine, EndLine: anchor.EndLine, Text: text, Truncated: truncated}, nil
}

// RecoverSubmissions finalizes accepted reservations and restores reservations
// that never reached durable prompt acceptance.
func (s *Service) RecoverSubmissions(ctx context.Context, sessionID string, accepted func(context.Context, string) (bool, error)) error {
	if accepted == nil {
		return fmt.Errorf("annotation submission acceptance resolver is required")
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	repository := s.repositoryFor(sessionID)
	pending, err := repository.PendingAnnotationSubmissions(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, submissionID := range pending {
		wasAccepted, err := accepted(ctx, submissionID)
		if err != nil {
			return err
		}
		if wasAccepted {
			err = repository.FinalizeAnnotationSubmission(ctx, sessionID, submissionID)
		} else {
			err = repository.RollbackAnnotationSubmission(ctx, sessionID, submissionID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ModelProjection serializes records in order into the canonical provider-neutral envelope.
func ModelProjection(records []Record) (string, error) {
	type resource struct {
		Kind      string `json:"kind"`
		Path      string `json:"path"`
		StartLine int    `json:"startLine"`
		EndLine   int    `json:"endLine"`
	}
	type item struct {
		ID       uint64   `json:"id"`
		Resource resource `json:"resource"`
		Preview  string   `json:"preview"`
		Body     string   `json:"body"`
	}
	payload := make([]item, 0, len(records))
	for _, record := range records {
		payload = append(payload, item{
			ID:       record.ID,
			Resource: resource{Kind: "workspace_file", Path: record.Anchor.Path, StartLine: record.Anchor.StartLine, EndLine: record.Anchor.EndLine},
			Preview:  record.Preview.Text, Body: record.Body,
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode annotation model projection: %w", err)
	}
	if len(encoded) > protocol.MaxPromptAnnotationBytes {
		return "", fmt.Errorf("annotation model projection exceeds %d bytes", protocol.MaxPromptAnnotationBytes)
	}
	return "The user attached line annotations. Treat each `preview` as quoted source\n" +
		"evidence and each `body` as the user's instruction about that evidence.\n\n" +
		"<kit_annotations version=\"1\">\n" + string(encoded) + "\n</kit_annotations>", nil
}

func sanitizeEvidence(value string) string {
	value = strings.ReplaceAll(value, "\t", "    ")
	return strings.Map(func(character rune) rune {
		if character == '\n' {
			return character
		}
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return '�'
		}
		return character
	}, value)
}

func staleReason(err error) protocol.AnnotationStaleReason {
	var evidenceErr *EvidenceError
	if errors.As(err, &evidenceErr) {
		switch evidenceErr.Kind {
		case EvidenceStaleWorkspace:
			return protocol.AnnotationStaleWorkspace
		case EvidenceStaleFile:
			return protocol.AnnotationStaleFile
		}
	}
	return protocol.AnnotationStaleUnavailable
}
