package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/sessiontool"
)

type fakeModelSessionManager struct {
	owner     kitsession.SessionRecord
	created   kitsession.CreateInput
	prompt    string
	promptErr error
	deleted   string
}

func (m *fakeModelSessionManager) Get(context.Context, string) (kitsession.SessionRecord, error) {
	return m.owner, nil
}
func (m *fakeModelSessionManager) Create(_ context.Context, input kitsession.CreateInput) (kitsession.SessionRecord, error) {
	m.created = input
	return kitsession.SessionRecord{ID: "session_new", CWD: input.CWD, Name: input.Name, ModelProvider: "test", ModelID: "default", ThinkingLevel: "medium", Persistent: true}, nil
}
func (m *fakeModelSessionManager) StartPrompt(_ context.Context, _ string, prompt string) (kitsession.RunReservation, error) {
	m.prompt = prompt
	return kitsession.RunReservation{RunID: "run_new"}, m.promptErr
}
func (m *fakeModelSessionManager) Delete(_ context.Context, sessionID string) error {
	m.deleted = sessionID
	return nil
}

func TestModelSessionServiceUsesConfiguredDefaultAndStartsPrompt(t *testing.T) {
	providers := defaultProviders{}
	manager := &fakeModelSessionManager{owner: kitsession.SessionRecord{ID: "session_owner", Persistent: true}}
	service := modelSessionService{
		manager: manager, providers: providers,
		availableProviders: func(context.Context) []string { return []string{"test"} },
		configuredDefault:  func() (string, error) { return "test/default", nil },
	}
	created, err := service.CreateModelSession(t.Context(), sessiontool.CreateInput{
		OwnerSessionID: "session_owner", CWD: "/repo", Name: "New work", Prompt: "begin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if manager.created.ParentSessionID != "session_owner" || manager.created.Model != "test/default" || manager.prompt != "begin" || created.ID != "session_new" || created.RunID != "run_new" {
		t.Fatalf("input = %+v, prompt = %q, result = %+v", manager.created, manager.prompt, created)
	}
}

func TestModelSessionServiceReportsPromptFailureAfterCreation(t *testing.T) {
	providers := defaultProviders{}
	manager := &fakeModelSessionManager{owner: kitsession.SessionRecord{ID: "session_owner", Persistent: true}, promptErr: errors.New("busy")}
	service := modelSessionService{manager: manager, providers: providers, availableProviders: func(context.Context) []string { return []string{"test"} }, configuredDefault: func() (string, error) { return "test/default", nil }}
	created, err := service.CreateModelSession(t.Context(), sessiontool.CreateInput{OwnerSessionID: "session_owner", CWD: "/repo", Name: "New work", Prompt: "begin"})
	if err == nil || created.ID != "" || manager.deleted != "session_new" {
		t.Fatalf("result = %+v, error = %v, deleted = %q", created, err, manager.deleted)
	}
}

func TestModelSessionServiceRejectsTemporaryOwner(t *testing.T) {
	manager := &fakeModelSessionManager{owner: kitsession.SessionRecord{ID: "session_owner", Persistent: false}}
	service := modelSessionService{manager: manager, providers: rejectingProviders{}}
	if _, err := service.CreateModelSession(t.Context(), sessiontool.CreateInput{OwnerSessionID: "session_owner", CWD: "/repo", Name: "New work"}); err == nil {
		t.Fatal("temporary owner creation succeeded")
	}
}

type defaultProviders struct{ rejectingProviders }

func (defaultProviders) Models() []droids.Model {
	return []droids.Model{{ID: "default", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}}
}
func (defaultProviders) Model(selector string) (droids.Model, bool) {
	models := (defaultProviders{}).Models()
	return models[0], selector == "test/default" || selector == "default"
}

type rejectingProviders struct{}

func (rejectingProviders) Models() []droids.Model { return nil }
func (rejectingProviders) Resolve(string) (droids.Model, error) {
	return droids.Model{}, errors.New("unexpected")
}
func (rejectingProviders) Model(string) (droids.Model, bool)   { return droids.Model{}, false }
func (rejectingProviders) RefreshModels(context.Context) error { return nil }
func (rejectingProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	return nil
}
