package kit

import (
	"context"
	"reflect"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

type fakeSettings struct {
	current protocol.ServerSettings
	updates []protocol.UpdateServerSettingsInput
}

func (f *fakeSettings) ServerSettings(context.Context) (protocol.ServerSettings, error) {
	return f.current, nil
}

func (f *fakeSettings) UpdateServerSettings(_ context.Context, input protocol.UpdateServerSettingsInput) (protocol.ServerSettings, error) {
	f.updates = append(f.updates, input)
	return f.current, nil
}

func TestClientReadsAndUpdatesServerSettings(t *testing.T) {
	fake := &fakeSettings{current: protocol.ServerSettings{
		ModelOverrides: []protocol.ModelOverride{}, PromptCacheRetention: protocol.PromptCacheShort,
		NewSessionModel: "openai/gpt-5.6-sol", NewSessionThinkingLevel: protocol.ThinkingMedium,
	}}
	client := newClient(&fakeClientBackend{}, nil)
	client.settings = fake
	t.Cleanup(func() { _ = client.Close() })

	if settings, err := client.ServerSettings(t.Context()); err != nil || !reflect.DeepEqual(settings, fake.current) {
		t.Fatalf("ServerSettings() = %+v, %v", settings, err)
	}
	if err := client.SetModelContextWindow(t.Context(), "openai/gpt-5.6-sol", 400_000); err != nil {
		t.Fatal(err)
	}
	want := []protocol.UpdateServerSettingsInput{{ModelOverrides: []protocol.ModelOverride{{Model: "openai/gpt-5.6-sol", ContextWindow: 400_000}}}}
	if !reflect.DeepEqual(fake.updates, want) {
		t.Fatalf("updates = %+v", fake.updates)
	}
	// An invalid update is refused before it reaches the server.
	if err := client.SetModelContextWindow(t.Context(), "no-provider", 1); err == nil || len(fake.updates) != 1 {
		t.Fatalf("SetModelContextWindow(invalid) = %v, updates %d", err, len(fake.updates))
	}
}
