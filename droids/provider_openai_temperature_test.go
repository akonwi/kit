package droids

import (
	"encoding/json"
	"testing"
)

func TestOpenAITemperatureCompatibility(t *testing.T) {
	for _, id := range []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-4o-mini"} {
		for _, effort := range []string{"", "none", "off", "low", "medium", "high", "xhigh", "max"} {
			for _, temperature := range []*float64{nil, temperaturePointer(0), temperaturePointer(0.7)} {
				t.Run(id+"/"+effort, func(t *testing.T) {
					model, ok := OpenAIModel(id)
					if !ok {
						t.Fatal("missing model")
					}
					req := Request{Reasoning: effort, Temperature: temperature}
					params, err := buildOpenAIResponseParams(model, req)
					invalid := id == "gpt-6-astra" && (effort == "none" || effort == "off") || id == "gpt-4o-mini" && effort != "" && effort != "none" && effort != "off"
					if invalid {
						if err == nil {
							t.Fatal("expected reasoning validation error")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					want := temperature != nil && (id == "gpt-4o-mini" || id != "gpt-6-astra" && (effort == "none" || effort == "off"))
					assertSerializedTemperature(t, params, temperature, want)
					if req.Temperature != temperature || req.Reasoning != effort {
						t.Fatal("request mutated")
					}
				})
			}
		}
	}
}

func TestCodexTemperatureBehaviorUnchanged(t *testing.T) {
	for _, model := range OpenAICodexModels() {
		for _, effort := range append([]string{""}, model.ReasoningLevels...) {
			t.Run(model.ID+"/"+effort, func(t *testing.T) {
				temperature := temperaturePointer(0.7)
				params, err := buildOpenAICodexResponseParams(model, Request{Reasoning: effort, Temperature: temperature})
				if err != nil {
					t.Fatal(err)
				}
				assertSerializedTemperature(t, params, temperature, true)
				if effort == "minimal" && string(params.Reasoning.Effort) != "low" {
					t.Fatal("minimal effort not normalized")
				}
			})
		}
	}
}

func TestCustomOpenAITemperatureBehaviorUnchanged(t *testing.T) {
	model := Model{ID: "custom", Reasoning: true, ReasoningLevels: []string{"high"}}
	temperature := temperaturePointer(0)
	params, err := buildOpenAIResponseParams(model, Request{Reasoning: "high", Temperature: temperature})
	if err != nil {
		t.Fatal(err)
	}
	assertSerializedTemperature(t, params, temperature, true)
}

func TestCodexTemperaturePolicyAfterNormalization(t *testing.T) {
	model, _ := OpenAICodexModel("gpt-5.4")
	model.TemperaturePolicy = TemperatureReasoningOff
	temperature := temperaturePointer(0.7)
	params, err := buildOpenAICodexResponseParams(model, Request{Reasoning: "minimal", Temperature: temperature})
	if err != nil {
		t.Fatal(err)
	}
	assertSerializedTemperature(t, params, temperature, false)
	if string(params.Reasoning.Effort) != "low" {
		t.Fatal("expected final low effort")
	}
}

func temperaturePointer(value float64) *float64 { return &value }

func assertSerializedTemperature(t *testing.T, params any, temperature *float64, want bool) {
	t.Helper()
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	value, present := body["temperature"]
	if present != want {
		t.Fatalf("temperature present = %v, want %v: %s", present, want, encoded)
	}
	if want && value != *temperature {
		t.Fatalf("temperature = %v, want %v", value, *temperature)
	}
}
