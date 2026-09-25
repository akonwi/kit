package sessiontool

import (
	"context"
	"errors"
	"testing"
)

type fakeService struct {
	input   CreateInput
	session Session
	err     error
}

func (s *fakeService) CreateModelSession(_ context.Context, input CreateInput) (Session, error) {
	s.input = input
	return s.session, s.err
}

func TestCreateSessionToolProjectsCreatedSession(t *testing.T) {
	service := &fakeService{session: Session{ID: "session_new", CWD: "/repo", Model: "test/model", ThinkingLevel: "medium", RunID: "run_new"}}
	tool := &ToolService{Service: service}
	result := tool.execute(t.Context(), "session_owner", arguments{CWD: " /repo ", Name: " New work ", Prompt: " investigate "})
	if result.IsError || service.input.OwnerSessionID != "session_owner" || service.input.CWD != "/repo" || service.input.Name != "New work" || service.input.Prompt != " investigate " {
		t.Fatalf("result = %+v; input = %+v", result, service.input)
	}
	if len(result.Details) == 0 {
		t.Fatalf("result has no structured details: %+v", result)
	}
}

func TestCreateSessionToolPreservesPromptWhitespace(t *testing.T) {
	service := &fakeService{session: Session{ID: "session_new", CWD: "/repo", Model: "test/model"}}
	tool := &ToolService{Service: service}
	result := tool.execute(t.Context(), "session_owner", arguments{CWD: "/repo", Name: "Work", Prompt: "  indented  "})
	if result.IsError || service.input.Prompt != "  indented  " || len(result.Details) == 0 {
		t.Fatalf("result = %+v; input = %+v", result, service.input)
	}
}

func TestCreateSessionToolReturnsServiceError(t *testing.T) {
	tool := &ToolService{Service: &fakeService{err: errors.New("denied")}}
	result := tool.execute(t.Context(), "session_owner", arguments{CWD: "/repo"})
	if !result.IsError {
		t.Fatalf("result = %+v", result)
	}
}
