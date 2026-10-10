package kit

import (
	"fmt"
	"net/http"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

func TestProjectDiffErrorPreservesDeclaredDiffAndGenericErrors(t *testing.T) {
	for name, test := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"deadline limit": {
			status: http.StatusRequestEntityTooLarge,
			body:   `{"error":{"code":"limit_exceeded","message":"the diff took longer than the server allows","details":{"limit":"deadline"}}}`,
			want:   &protocol.DiffError{Code: protocol.DiffErrorLimit, Message: "the diff took longer than the server allows", Details: map[string]string{"limit": "deadline"}},
		},
		"unsupported repository": {
			status: http.StatusUnprocessableEntity,
			body:   `{"error":{"code":"unsupported_repository","message":"repository is unsupported","details":{"reason":"sparse_index"}}}`,
			want:   &protocol.DiffError{Code: protocol.DiffErrorUnsupportedRepository, Message: "repository is unsupported", Details: map[string]string{"reason": "sparse_index"}},
		},
		"capacity": {
			status: http.StatusTooManyRequests,
			body:   `{"error":{"code":"capacity_exceeded","message":"diff observation exceeds cache capacity","details":{"scope":"session"}}}`,
			want:   &protocol.DiffError{Code: protocol.DiffErrorCapacity, Message: "diff observation exceeds cache capacity", Details: map[string]string{"scope": "session"}},
		},
		"stale target": {
			status: http.StatusConflict,
			body:   `{"error":{"code":"stale_target","message":"repository authority changed"}}`,
			want:   &protocol.DiffError{Code: protocol.DiffErrorStaleTarget, Message: "repository authority changed"},
		},
		"diff unavailable": {
			status: http.StatusServiceUnavailable,
			body:   `{"error":{"code":"unavailable","message":"diff service is unavailable"}}`,
			want:   &protocol.DiffError{Code: protocol.DiffErrorUnavailable, Message: "diff service is unavailable"},
		},
		"generic internal": {
			status: http.StatusInternalServerError,
			body:   `{"error":{"code":"internal","message":"internal server error"}}`,
			want:   &ServerError{Code: ErrorInternal, Message: "internal server error"},
		},
		"generic invalid request": {
			status: http.StatusBadRequest,
			body:   `{"error":{"code":"invalid_request","message":"request body is invalid"}}`,
			want:   &ServerError{Code: ErrorInvalidRequest, Message: "request body is invalid"},
		},
		"common unauthorized": {
			status: http.StatusUnauthorized,
			body:   `{"error":{"code":"unauthorized","message":"missing access token"}}`,
			want:   &ServerError{Code: ErrorUnauthorized, Message: "missing access token"},
		},
	} {
		decoded := httpapi.DecodeOperationError(httpapi.ObserveWorkingTree, test.status, []byte(test.body))
		got := projectDiffError(decoded)
		if describeDiffProjection(got) != describeDiffProjection(test.want) {
			t.Errorf("%s:\n got  %s\n want %s", name, describeDiffProjection(got), describeDiffProjection(test.want))
		}
	}
}

func TestProjectDiffErrorRejectsMalformedDiffDetails(t *testing.T) {
	decoded := httpapi.DecodeOperationError(httpapi.ObserveWorkingTree, http.StatusRequestEntityTooLarge, []byte(`{"error":{"code":"limit_exceeded","message":"limit exceeded","details":{"limit":"unknown_limit"}}}`))
	if got := describeDiffProjection(projectDiffError(decoded)); got != "*errors.errorString daemon returned malformed diff error" {
		t.Fatalf("projection = %s", got)
	}
}

// describeDiffProjection renders the observable identity of a projected
// error: its type and, for typed errors, its code, message, and details.
func describeDiffProjection(err error) string {
	switch typed := err.(type) {
	case *protocol.DiffError:
		return fmt.Sprintf("%T code=%s message=%q details=%v", typed, typed.Code, typed.Message, typed.Details)
	case *ServerError:
		return fmt.Sprintf("%T code=%s message=%q details=%v", typed, typed.Code, typed.Message, typed.Details)
	default:
		return fmt.Sprintf("%T %v", err, err)
	}
}
