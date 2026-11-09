package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	imagepng "image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/serveraccess"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/version"
)

const sessionErrorTestID = "session_0123456789abcdef0123456789abcdef"

// sessionErrorTestService fails each session method whose handler writes
// failures through writeSessionError with err.
type sessionErrorTestService struct {
	sessionService
	err error
}

func (s sessionErrorTestService) MessagePage(context.Context, string, protocol.MessagePageQuery) (protocol.MessagePage, error) {
	return protocol.MessagePage{}, s.err
}

func (s sessionErrorTestService) TranscriptPage(context.Context, string, string) (protocol.TranscriptPage, error) {
	return protocol.TranscriptPage{}, s.err
}

func (s sessionErrorTestService) Workspace(context.Context, string) (protocol.WorkspaceRef, error) {
	return protocol.WorkspaceRef{}, s.err
}

func (s sessionErrorTestService) ListDirectory(context.Context, string, protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	return protocol.DirectoryPage{}, s.err
}

func (s sessionErrorTestService) FileIndex(context.Context, string, bool) (protocol.SessionFileIndex, error) {
	return protocol.SessionFileIndex{}, s.err
}

func (s sessionErrorTestService) Events(context.Context, string, string, int64) (protocol.SessionEventBatch, error) {
	return protocol.SessionEventBatch{}, s.err
}

func (s sessionErrorTestService) SubmitPrompt(context.Context, string, protocol.PromptInput) (protocol.PromptSubmission, error) {
	return protocol.PromptSubmission{}, s.err
}

func (s sessionErrorTestService) StartPrompt(context.Context, string, protocol.PromptInput) (protocol.TurnReservation, error) {
	return protocol.TurnReservation{}, s.err
}

func (s sessionErrorTestService) Prompt(context.Context, string, protocol.PromptInput) (protocol.PromptOutcome, error) {
	return protocol.PromptOutcome{}, s.err
}

func (s sessionErrorTestService) Turn(context.Context, string, string) (protocol.TurnInfo, error) {
	return protocol.TurnInfo{}, s.err
}

func (s sessionErrorTestService) ListDiffTargets(context.Context, string, protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	return protocol.DiffTargetCatalog{}, s.err
}

func (s sessionErrorTestService) DeleteAnnotation(context.Context, string, protocol.DeleteAnnotationInput) error {
	return s.err
}

// attachmentErrorTestService fails every attachment method with err.
type attachmentErrorTestService struct{ err error }

func (s attachmentErrorTestService) Put(context.Context, string, attachment.PutInput) (protocol.AttachmentInfo, error) {
	return protocol.AttachmentInfo{}, s.err
}

func (s attachmentErrorTestService) Open(context.Context, string, string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	return protocol.AttachmentInfo{}, nil, s.err
}

func (s attachmentErrorTestService) Resolve(context.Context, string, []string) (protocol.AttachmentResolution, error) {
	return protocol.AttachmentResolution{}, s.err
}

// imageRejection is the session runtime's refusal of an image attachment
// for a model that accepts only text.
var imageRejection = fmt.Errorf("%w: model %q does not accept image attachments", kitsession.ErrInvalidInput, "echo")

func TestSessionErrorsUseTheDeclaredEnvelope(t *testing.T) {
	t.Parallel()
	prompt := `{"text":"describe this","attachmentIds":["attachment_0123456789abcdef0123456789abcdef"]}`
	staleEvidence := fmt.Errorf("%w: annotation 4: %w", kitannotation.ErrStale, &kitannotation.EvidenceError{Kind: kitannotation.EvidenceStaleFile})
	deniedEvidence := fmt.Errorf("%w: annotation 4: %w", kitannotation.ErrStale, &kitannotation.EvidenceError{Kind: kitannotation.EvidencePermission})
	tests := []struct {
		name, method, path, body string
		err                      error
		status                   int
		want                     string
		decode                   func(int, []byte) error
	}{
		{
			name: "image for a text-only model", method: http.MethodPost, path: "/turns/submissions", body: prompt, err: imageRejection,
			status: http.StatusBadRequest,
			want:   `{"error":{"code":"invalid_request","message":"invalid session input: model \"echo\" does not accept image attachments"}}`,
			decode: decodeWith(httpapi.SubmitPrompt),
		},
		{
			name: "image for a text-only model when starting", method: http.MethodPost, path: "/turns/prompts", body: prompt, err: imageRejection,
			status: http.StatusBadRequest,
			want:   `{"error":{"code":"invalid_request","message":"invalid session input: model \"echo\" does not accept image attachments"}}`,
			decode: decodeWith(httpapi.StartPrompt),
		},
		{
			name: "invalid request rejected by the handler", method: http.MethodGet, path: "/messages?limit=0",
			status: http.StatusBadRequest,
			want:   `{"error":{"code":"invalid_request","message":"invalid session request: message limit must be between 1 and 100"}}`,
			decode: decodeWith(httpapi.GetMessagePage),
		},
		{
			name: "missing turn", method: http.MethodGet, path: "/turns/turn_missing", err: fmt.Errorf("run %q: %w", "turn_missing", kitsession.ErrNotFound),
			status: http.StatusNotFound,
			want:   `{"error":{"code":"not_found","message":"not found"}}`,
			decode: decodeWith(httpapi.GetTurn),
		},
		{
			name: "busy session", method: http.MethodPost, path: "/turns/prompts", body: prompt, err: kitsession.ErrBusy,
			status: http.StatusConflict,
			want:   `{"error":{"code":"conflict","message":"session already has an active parent run"}}`,
			decode: decodeWith(httpapi.StartPrompt),
		},
		{
			name: "unavailable transcript cursor", method: http.MethodGet, path: "/transcript?before=9", err: kitsession.ErrTranscriptCursorUnavailable,
			status: http.StatusConflict,
			want:   `{"error":{"code":"conflict","message":"transcript cursor is unavailable"}}`,
			decode: decodeWith(httpapi.GetTranscriptPage),
		},
		{
			name: "stale annotation", method: http.MethodPost, path: "/turns/submissions", body: prompt, err: staleEvidence,
			status: http.StatusConflict,
			want:   `{"error":{"code":"conflict","message":"the file changed; refresh it and try again"}}`,
			decode: decodeWith(httpapi.SubmitPrompt),
		},
		{
			name: "unreadable annotation", method: http.MethodPost, path: "/turns/submissions", body: prompt, err: deniedEvidence,
			status: http.StatusForbidden,
			want:   `{"error":{"code":"forbidden","message":"permission to read the selected evidence was denied"}}`,
			decode: decodeWith(httpapi.SubmitPrompt),
		},
		{
			name: "session being deleted", method: http.MethodGet, path: "/workspace", err: kitsession.ErrDeleteBusy,
			status: http.StatusConflict,
			want:   `{"error":{"code":"conflict","message":"session cannot be deleted while work is active"}}`,
			decode: decodeWith(httpapi.GetWorkspace),
		},
		{
			name: "stale workspace keeps its code", method: http.MethodPost, path: "/workspace/directories", body: `{"workspaceId":"workspace_0123456789abcdef0123456789abcdef","path":"."}`,
			err:    staleWorkspaceError(protocol.WorkspaceRef{WorkspaceID: "workspace_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", State: protocol.WorkspaceReady}),
			status: http.StatusConflict,
			want:   `{"error":{"code":"stale_workspace","details":{"currentWorkspaceId":"workspace_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8","currentWorkspaceState":"ready"},"message":"the session workspace changed"}}`,
			decode: decodeWith(httpapi.ListWorkspaceDirectory),
		},
		{
			name: "failed compaction", method: http.MethodPost, path: "/turns/prompt", body: prompt, err: fmt.Errorf("%w: provider said no", kitsession.ErrCompactionFailed),
			status: http.StatusUnprocessableEntity,
			want:   `{"error":{"code":"unprocessable","message":"context compaction failed"}}`,
			decode: decodeWith(httpapi.Prompt),
		},
		{
			name: "closed manager", method: http.MethodGet, path: "/events", err: kitsession.ErrClosed,
			status: http.StatusServiceUnavailable,
			want:   `{"error":{"code":"unavailable","message":"session is unavailable"}}`,
			decode: decodeWith(httpapi.GetSessionEventPage),
		},
		{
			name: "closed manager for attachments", method: http.MethodPost, path: "/attachments/resolve", body: `{"attachmentIds":["attachment_0123456789abcdef0123456789abcdef"]}`, err: kitsession.ErrClosed,
			status: http.StatusServiceUnavailable,
			want:   `{"error":{"code":"unavailable","message":"session is unavailable"}}`,
			decode: decodeWith(httpapi.ResolveAttachments),
		},
		{
			name: "attachments of a session being deleted", method: http.MethodPost, path: "/attachments/resolve", body: `{"attachmentIds":["attachment_0123456789abcdef0123456789abcdef"]}`, err: kitsession.ErrDeleteBusy,
			status: http.StatusConflict,
			want:   `{"error":{"code":"conflict","message":"session cannot be deleted while work is active"}}`,
			decode: decodeWith(httpapi.ResolveAttachments),
		},
		{
			name: "internal failure", method: http.MethodGet, path: "/files", err: errors.New("scan /private/path: disk exploded"),
			status: http.StatusInternalServerError,
			want:   `{"error":{"code":"internal","message":"internal server error"}}`,
			decode: decodeWith(httpapi.GetSessionFileIndex),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			registerSessionRoutes(mux, sessionErrorTestService{err: test.err})
			registerAttachmentRoutes(mux, attachmentErrorTestService{err: test.err})
			var contractErr error
			handler := contractConformanceMiddleware(t, mux, func(err error) { contractErr = err })
			var body io.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			}
			request := httptest.NewRequest(test.method, "/v1/sessions/"+sessionErrorTestID+test.path, body)
			addContractRequestHeaders(request)
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if contractErr != nil {
				t.Fatalf("contract violation: %v", contractErr)
			}
			if response.Code != test.status || response.Body.String() != test.want+"\n" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response = %d %q %q\nwant %d %q", response.Code, response.Header().Get("Content-Type"), response.Body.String(), test.status, test.want)
			}
			if test.decode == nil {
				return
			}
			var apiError *httpapi.APIError
			if err := test.decode(response.Code, response.Body.Bytes()); !errors.As(err, &apiError) || apiError.StatusCode != test.status {
				t.Fatalf("decoded error = %#v", err)
			}
		})
	}
}

func TestSessionErrorReportsCapacityInTheDeclaredEnvelope(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	writeSessionError(response, kitsession.ErrInteractionCapacity)
	want := `{"error":{"code":"capacity_exceeded","message":"session has too many pending interactions"}}` + "\n"
	if response.Code != http.StatusTooManyRequests || response.Body.String() != want {
		t.Fatalf("response = %d %q, want 429 %q", response.Code, response.Body.String(), want)
	}
	var apiError *httpapi.APIError
	if err := httpapi.DecodeOperationError(httpapi.OperateSubagent, response.Code, response.Body.Bytes()); !errors.As(err, &apiError) || apiError.Code != string(httpapi.ErrorCapacityExceeded) {
		t.Fatalf("decoded error = %#v", err)
	}
}

func decodeWith[Params, In, Out any](op httpapi.Operation[Params, In, Out]) func(int, []byte) error {
	return func(status int, body []byte) error { return httpapi.DecodeOperationError(op, status, body) }
}

// TestPublicClientReceivesDeclaredSessionErrors drives a real server and
// session runtime through the public Go client, which decodes only declared
// error bodies.
func TestPublicClientReceivesDeclaredSessionErrors(t *testing.T) {
	t.Parallel()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	block := make(chan struct{})
	requestStarted := make(chan struct{})
	providers := &daemonEchoProviders{block: block, requestStarted: requestStarted}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() {
		stopped <- Run(ctx, RunOptions{Paths: paths, Providers: providers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("server did not stop")
		}
	})

	var client *kit.Client
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		client, err = kit.Connect(t.Context(), kit.Home(paths.Home))
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Connect() = %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { _ = client.Close() })
	created, err := client.CreateSession(t.Context(), kit.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo", ThinkingLevel: "off"})
	if err != nil {
		t.Fatalf("CreateSession() = %v", err)
	}
	session, err := client.Attach(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("Attach() = %v", err)
	}

	var png bytes.Buffer
	if err := imagepng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	uploaded, err := session.UploadAttachment(t.Context(), "pixel.png", &png)
	if err != nil {
		t.Fatalf("UploadAttachment() = %v", err)
	}
	imagePrompt := protocol.PromptInput{Text: "describe this", AttachmentIDs: []string{uploaded.ID}}
	wantImageRejection := &kit.ServerError{Code: kit.ErrorInvalidRequest, Message: `invalid session input: model "echo" does not accept image attachments`}
	if _, err := session.SendMessage(t.Context(), imagePrompt); !sameServerError(err, wantImageRejection) {
		t.Fatalf("SendMessage(image) error = %#v, want %#v", err, wantImageRejection)
	}
	if _, err := session.StartPromptInput(t.Context(), imagePrompt); !sameServerError(err, wantImageRejection) {
		t.Fatalf("StartPromptInput(image) error = %#v, want %#v", err, wantImageRejection)
	}

	wantMissing := &kit.ServerError{Code: kit.ErrorNotFound, Message: "not found"}
	if _, err := session.Turn(t.Context(), "turn_0123456789abcdef0123456789abcdef"); !sameServerError(err, wantMissing) {
		t.Fatalf("Turn(missing) error = %#v, want %#v", err, wantMissing)
	}

	if _, err := session.TranscriptPage(t.Context(), "999"); !errors.Is(err, kit.ErrTranscriptCursorUnavailable) {
		t.Fatalf("TranscriptPage(unavailable cursor) error = %#v, want ErrTranscriptCursorUnavailable", err)
	}

	active, err := session.StartPrompt(t.Context(), "hold the session")
	if err != nil {
		t.Fatalf("StartPrompt() = %v", err)
	}
	<-requestStarted
	wantBusy := &kit.ServerError{Code: kit.ErrorConflict, Message: "session already has an active parent run"}
	if _, err := session.StartPrompt(t.Context(), "another"); !sameServerError(err, wantBusy) {
		t.Fatalf("StartPrompt(busy) error = %#v, want %#v", err, wantBusy)
	}
	if err := active.Abort(t.Context()); err != nil {
		t.Fatalf("Abort() = %v", err)
	}
	close(block)
}

// sameServerError reports whether err is a declared server error with want's
// code and message.
func sameServerError(err error, want *kit.ServerError) bool {
	var server *kit.ServerError
	return errors.As(err, &server) && server.Code == want.Code && server.Message == want.Message
}

// snapshotSessionErrorTestService serves a session snapshot so the public
// client can attach, then fails the attached session's operations with err.
type snapshotSessionErrorTestService struct{ sessionErrorTestService }

func (snapshotSessionErrorTestService) Snapshot(_ context.Context, sessionID string) (protocol.SessionSnapshot, error) {
	now := time.Unix(1, 0).UTC().Format(time.RFC3339Nano)
	return protocol.SessionSnapshot{Session: protocol.SessionInfo{
		ID: sessionID, CWD: "/workspace", Model: "test/model", ThinkingLevel: "off", ConfigurationRevision: 1, CreatedAt: now, UpdatedAt: now,
	}, Messages: []protocol.TranscriptMessage{}}, nil
}

// TestPublicClientReceivesDeclaredUnavailableAndDeletionErrors covers failures
// a live runtime cannot be made to produce on demand: a closed session
// manager and a session being deleted.
func TestPublicClientReceivesDeclaredUnavailableAndDeletionErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		call func(context.Context, *kit.Session) error
		want *kit.ServerError
	}{
		{
			name: "closed manager", err: kitsession.ErrClosed,
			call: func(ctx context.Context, session *kit.Session) error {
				_, err := session.SubmitPrompt(ctx, "hello")
				return err
			},
			want: &kit.ServerError{Code: kit.ErrorUnavailable, Message: "session is unavailable"},
		},
		{
			name: "closed manager for attachments", err: kitsession.ErrClosed,
			call: func(ctx context.Context, session *kit.Session) error {
				_, err := session.ResolveAttachments(ctx, []string{"attachment_0123456789abcdef0123456789abcdef"})
				return err
			},
			want: &kit.ServerError{Code: kit.ErrorUnavailable, Message: "session is unavailable"},
		},
		{
			name: "session being deleted", err: kitsession.ErrDeleteBusy,
			call: func(ctx context.Context, session *kit.Session) error {
				_, err := session.Workspace(ctx)
				return err
			},
			want: &kit.ServerError{Code: kit.ErrorConflict, Message: "session cannot be deleted while work is active"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := sessionErrorTestClient(t, test.err)
			session, err := client.Attach(t.Context(), sessionErrorTestID)
			if err != nil {
				t.Fatalf("Attach() = %v", err)
			}
			if err := test.call(t.Context(), session); !sameServerError(err, test.want) {
				t.Fatalf("error = %#v, want %#v", err, test.want)
			}
		})
	}
}

// sessionErrorTestServer serves the real handler, backed by session
// operations that fail with err, and returns its address.
func sessionErrorTestServer(t *testing.T, err error) string {
	t.Helper()
	store, openErr := storage.Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := newHandler(localHandlerOptions{
		access: serveraccess.NewPolicy(nil, nil), token: "test", store: store,
		registry:  localdiscovery.Registry{InstanceID: "instance_test", ProtocolVersion: version.SessionProtocolVersion, KitVersion: version.Version},
		identity:  serverIdentity{serverID: "server_0123456789abcdef0123456789abcdef", machineID: strings.Repeat("ab", 32), home: "/Users/test"},
		providers: func(context.Context) []string { return []string{} },
		sessions:  snapshotSessionErrorTestService{sessionErrorTestService{err: err}}, attachments: attachmentErrorTestService{err: err},
	})
	server := httptest.NewServer(contractConformanceMiddleware(t, handler, func(err error) { t.Errorf("contract violation: %v", err) }))
	t.Cleanup(server.Close)
	return server.URL
}

// sessionErrorTestClient connects the public client to a server whose
// session operations fail with err.
func sessionErrorTestClient(t *testing.T, err error) *kit.Client {
	t.Helper()
	client, connectErr := kit.Connect(t.Context(), kit.Endpoint(sessionErrorTestServer(t, err), kit.AccessToken("test")))
	if connectErr != nil {
		t.Fatalf("Connect() = %v", connectErr)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestReleasedClientDeclarationsGetPlainErrorsFromNewDeclarations decodes
// the newly declared failures with the declarations Kit 0.56.0 clients
// compiled in for the affected statuses. The strict decoder returns an
// ordinary error for a code or status it does not know, and the call ends
// promptly.
func TestReleasedClientDeclarationsGetPlainErrorsFromNewDeclarations(t *testing.T) {
	t.Parallel()
	released := struct {
		workspace, attachment []httpapi.ErrorResponse
	}{
		workspace: []httpapi.ErrorResponse{
			{Status: http.StatusNotFound, Codes: []httpapi.ErrorCode{httpapi.ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []httpapi.ErrorCode{httpapi.ErrorCode(protocol.WorkspaceErrorStaleWorkspace), httpapi.ErrorCode(protocol.WorkspaceErrorStaleFile), httpapi.ErrorCode(protocol.WorkspaceErrorStaleCursor)}},
			{Status: http.StatusServiceUnavailable, Codes: []httpapi.ErrorCode{httpapi.ErrorUnavailable}},
			{Status: http.StatusInternalServerError, Codes: []httpapi.ErrorCode{httpapi.ErrorInternal}},
		},
		attachment: []httpapi.ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []httpapi.ErrorCode{httpapi.ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []httpapi.ErrorCode{httpapi.ErrorNotFound}},
			{Status: http.StatusRequestEntityTooLarge, Codes: []httpapi.ErrorCode{httpapi.ErrorLimitExceeded}},
			{Status: http.StatusInternalServerError, Codes: []httpapi.ErrorCode{httpapi.ErrorInternal}},
		},
	}
	getWorkspace := httpapi.GetWorkspace
	getWorkspace.Errors = released.workspace
	resolveAttachments := httpapi.ResolveAttachments
	resolveAttachments.Errors = released.attachment
	attachmentInput := protocol.AttachmentResolutionInput{AttachmentIDs: []string{"attachment_0123456789abcdef0123456789abcdef"}}
	for _, test := range []struct {
		name string
		err  error
		call func(context.Context, httpapi.Transport) error
		want string
	}{
		{
			name: "workspace of a session being deleted", err: kitsession.ErrDeleteBusy,
			call: func(ctx context.Context, transport httpapi.Transport) error {
				_, err := httpapi.Call(ctx, transport, getWorkspace, httpapi.SessionPath{SessionID: sessionErrorTestID}, httpapi.NoBody{})
				return err
			},
			want: `daemon returned undeclared error code "conflict" for HTTP 409`,
		},
		{
			name: "attachments of a session being deleted", err: kitsession.ErrDeleteBusy,
			call: func(ctx context.Context, transport httpapi.Transport) error {
				_, err := httpapi.Call(ctx, transport, resolveAttachments, httpapi.SessionPath{SessionID: sessionErrorTestID}, attachmentInput)
				return err
			},
			want: `daemon returned undeclared error code "conflict" for HTTP 409`,
		},
		{
			name: "attachments of a closed manager", err: kitsession.ErrClosed,
			call: func(ctx context.Context, transport httpapi.Transport) error {
				_, err := httpapi.Call(ctx, transport, resolveAttachments, httpapi.SessionPath{SessionID: sessionErrorTestID}, attachmentInput)
				return err
			},
			want: "daemon returned undeclared HTTP status 503",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			transport := clienttransport.NewEndpoint(sessionErrorTestServer(t, test.err), "test")
			t.Cleanup(transport.CloseIdleConnections)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := test.call(ctx, transport)
			var apiError *httpapi.APIError
			if err == nil || errors.As(err, &apiError) || err.Error() != test.want || ctx.Err() != nil {
				t.Fatalf("error = %#v (context %v), want %q", err, ctx.Err(), test.want)
			}
		})
	}
}

// TestPublicClientKeepsTypedDiffAndAnnotationCodes guards the diff and
// annotation operations, which declare their own codes: their failures keep
// those codes rather than the generic code for the status.
func TestPublicClientKeepsTypedDiffAndAnnotationCodes(t *testing.T) {
	t.Parallel()
	t.Run("diff outside a repository", func(t *testing.T) {
		t.Parallel()
		client := sessionErrorTestClient(t, &protocol.DiffError{Code: protocol.DiffErrorNotRepository, Message: "the workspace is not a git repository"})
		session, err := client.Attach(t.Context(), sessionErrorTestID)
		if err != nil {
			t.Fatalf("Attach() = %v", err)
		}
		_, err = session.ListDiffTargets(t.Context(), protocol.ListDiffTargetsInput{WorkspaceID: "workspace_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"})
		var diffErr *protocol.DiffError
		if !errors.As(err, &diffErr) || diffErr.Code != protocol.DiffErrorNotRepository || diffErr.Message != "the workspace is not a git repository" {
			t.Fatalf("ListDiffTargets() error = %#v, want not_repository", err)
		}
	})
	t.Run("stale annotation target", func(t *testing.T) {
		t.Parallel()
		client := sessionErrorTestClient(t, fmt.Errorf("%w: annotation 4: %w", kitannotation.ErrStale, &kitannotation.EvidenceError{Kind: kitannotation.EvidenceStaleTarget}))
		session, err := client.Attach(t.Context(), sessionErrorTestID)
		if err != nil {
			t.Fatalf("Attach() = %v", err)
		}
		want := &kit.ServerError{Code: kit.ErrorCode(protocol.AnnotationEvidenceErrorStaleTarget), Message: "the diff changed; refresh it and try again"}
		if err := session.DeleteAnnotation(t.Context(), protocol.DeleteAnnotationInput{AnnotationID: 4}); !sameServerError(err, want) {
			t.Fatalf("DeleteAnnotation() error = %#v, want %#v", err, want)
		}
	})
}
