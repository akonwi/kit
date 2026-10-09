package droids

import "testing"

func TestSpawnToolExecutionDefaultsAndOverrides(t *testing.T) {
	model, err := BindModel(AdaptProvider("test", []Model{{Provider: "test", ID: "model"}}, nil), Model{Provider: "test", ID: "model"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		policy    *ExecutionPolicy
		wantLimit int
		wantMode  ExecutionMode
	}{
		{name: "omitted", wantLimit: 8, wantMode: ModeParallel},
		{name: "zero value", policy: &ExecutionPolicy{}, wantLimit: 8, wantMode: ModeParallel},
		{name: "explicit limit", policy: &ExecutionPolicy{MaxParallelTools: 2}, wantLimit: 2, wantMode: ModeParallel},
		{name: "sequential preserved", policy: &ExecutionPolicy{ToolExecution: ModeSequential}, wantLimit: 8, wantMode: ModeSequential},
	} {
		t.Run(test.name, func(t *testing.T) {
			droid, err := Spawn(t.Context(), "conversation_execution_defaults", Config{Model: model, Execution: test.policy})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = droid.Close() })
			got := droid.sdk.config.Execution
			if got.MaxParallelTools != test.wantLimit || got.ToolExecution != test.wantMode {
				t.Fatalf("execution = %+v, want limit %d and mode %s", got, test.wantLimit, test.wantMode)
			}
		})
	}
}
