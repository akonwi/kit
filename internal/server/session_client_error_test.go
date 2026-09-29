package server

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
