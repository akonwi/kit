package settingsbridge

import (
	"context"
	"reflect"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

type recorder struct {
	inputs []protocol.UpdateServerSettingsInput
}

func (r *recorder) UpdateServerSettings(_ context.Context, input protocol.UpdateServerSettingsInput) (protocol.ServerSettings, error) {
	r.inputs = append(r.inputs, input)
	return protocol.ServerSettings{PromptCacheRetention: protocol.PromptCacheShort}, nil
}

func TestEachSetterCarriesOnlyItsField(t *testing.T) {
	t.Parallel()
	client := &recorder{}
	ctx := context.Background()
	for _, set := range []func() error{
		func() error { _, err := setDefaultModel(ctx, client, "anthropic/claude-sonnet-4-6"); return err },
		func() error { _, err := setCompactionModel(ctx, client, ""); return err },
		func() error { _, err := setPromptCacheRetention(ctx, client, "long"); return err },
		func() error { _, err := setReadClaudeConfigs(ctx, client, false); return err },
	} {
		if err := set(); err != nil {
			t.Fatal(err)
		}
	}
	model, empty, long, off := "anthropic/claude-sonnet-4-6", "", protocol.PromptCacheLong, false
	want := []protocol.UpdateServerSettingsInput{
		{DefaultModel: &model},
		{CompactionModel: &empty},
		{PromptCacheRetention: &long},
		{ReadClaudeConfigs: &off},
	}
	if !reflect.DeepEqual(client.inputs, want) {
		t.Fatalf("updates = %#v, want %#v", client.inputs, want)
	}
}
