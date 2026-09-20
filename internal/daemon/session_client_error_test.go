package daemon

import (
	"errors"
	"net/http"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestDecodeAnnotationEvidenceErrorPreservesUserMessage(t *testing.T) {
	err := decodeAPIError(http.StatusConflict, []byte(`{"error":{"code":"stale_target","message":"the diff changed; refresh it and try again"}}`))
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("decoded error = %T %v", err, err)
	}
	if got := apiError.UserMessage(); got != "the diff changed; refresh it and try again" {
		t.Fatalf("user message = %q", got)
	}
}

func TestDecodeScratchpadConflictPreservesAuthoritativeRecord(t *testing.T) {
	err := decodeAPIError(http.StatusConflict, []byte(`{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{"scratchpad":{"ownerSessionId":"session_0123456789abcdef0123456789abcdef","content":"shared","revision":"2","updatedAt":"2026-03-23T12:34:56Z"}}}}`))
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code != string(protocol.ScratchpadRevisionConflict) ||
		apiError.CurrentScratchpad == nil || apiError.CurrentScratchpad.Content != "shared" || apiError.CurrentScratchpad.Revision != 2 {
		t.Fatalf("decoded error = %#v", err)
	}
	var scratchpadError *protocol.ScratchpadError
	if !errors.As(err, &scratchpadError) || scratchpadError.Code != protocol.ScratchpadRevisionConflict || scratchpadError.Current != apiError.CurrentScratchpad {
		t.Fatalf("typed scratchpad error = %#v", scratchpadError)
	}
}

func TestDecodeScratchpadErrorRejectsMalformedEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing conflict record", http.StatusConflict, `{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{}}}`},
		{"wrong status", http.StatusBadRequest, `{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{"scratchpad":{"ownerSessionId":"session_0123456789abcdef0123456789abcdef","content":"shared","revision":"2","updatedAt":"2026-03-23T12:34:56Z"}}}}`},
		{"unknown details", http.StatusServiceUnavailable, `{"error":{"code":"scratchpad_unavailable","message":"scratchpad unavailable","details":{"unexpected":"value"}}}`},
		{"unknown record field", http.StatusConflict, `{"error":{"code":"scratchpad_revision_conflict","message":"scratchpad revision conflict","details":{"scratchpad":{"ownerSessionId":"session_0123456789abcdef0123456789abcdef","content":"shared","revision":"2","updatedAt":"2026-03-23T12:34:56Z","unexpected":true}}}}`},
		{"null details", http.StatusServiceUnavailable, `{"error":{"code":"scratchpad_unavailable","message":"scratchpad unavailable","details":null}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := decodeAPIError(test.status, []byte(test.body))
			var apiError *APIError
			if errors.As(err, &apiError) {
				t.Fatalf("malformed error decoded as APIError: %#v", apiError)
			}
		})
	}
}

func TestDecodeStrictJSONObjectRejectsTrailingJSON(t *testing.T) {
	var details protocol.ScratchpadErrorDetails
	if err := decodeStrictJSONObject([]byte(`{} {}`), &details); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestDecodeInvalidAnnotationEvidenceError(t *testing.T) {
	err := decodeAPIError(http.StatusUnprocessableEntity, []byte(`{"error":{"code":"invalid_evidence","message":"that range cannot be annotated"}}`))
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code != "invalid_evidence" {
		t.Fatalf("decoded error = %T %v", err, err)
	}
}
