package droids

import (
	"context"
	"testing"
)

func TestDeriveSchemaFromArgs(t *testing.T) {
	type args struct {
		City string `json:"city" jsonschema:"description=city name"`
		Days int    `json:"days"`
	}
	tool := MustTool(Tool[args]{Name: "forecast", Execute: testNoopTool[args]})
	s := tool.schema()

	if s.Parameters["type"] != "object" {
		t.Fatalf("type = %v, want object", s.Parameters["type"])
	}
	props, ok := s.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %T", s.Parameters["properties"])
	}
	city, ok := props["city"].(map[string]any)
	if !ok || city["type"] != "string" {
		t.Fatalf("city prop = %v", props["city"])
	}
	if city["description"] != "city name" {
		t.Fatalf("city description = %v", city["description"])
	}
	if props["days"].(map[string]any)["type"] != "integer" {
		t.Fatalf("days prop = %v", props["days"])
	}
}

func TestDeriveSchemaEmptyStruct(t *testing.T) {
	tool := MustTool(Tool[struct{}]{
		Name: "now",
		Execute: func(_ context.Context, _ ToolContext, _ struct{}, _ ToolUpdate) (ToolResult, error) {
			return ToolText("ok"), nil
		},
	})
	s := tool.schema()
	if s.Parameters["type"] != "object" {
		t.Fatalf("empty-struct schema type = %v, want object", s.Parameters["type"])
	}
}

func TestExplicitParametersOverrideDerivation(t *testing.T) {
	explicit := map[string]any{"type": "object", "properties": map[string]any{}}
	tool := MustTool(Tool[struct {
		X int `json:"x"`
	}]{Name: "t", Parameters: explicit, Execute: func(context.Context, ToolContext, struct {
		X int `json:"x"`
	}, ToolUpdate) (ToolResult, error) {
		return ToolText("ok"), nil
	}})
	if got := tool.schema().Parameters; got["properties"] == nil ||
		len(got["properties"].(map[string]any)) != 0 {
		t.Fatalf("explicit parameters were not used: %v", got)
	}
}

func TestToolSchemaCanonicalizesNumericKeywordsForProviderEncoding(t *testing.T) {
	tool := MustTool(Tool[struct{}]{
		Name: "batch",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{"type": "array", "minItems": 1},
			},
		},
		Execute: testNoopTool[struct{}],
	})
	properties := tool.schema().Parameters["properties"].(map[string]any)
	minimum := properties["items"].(map[string]any)["minItems"]
	if _, ok := minimum.(float64); !ok || minimum != float64(1) {
		t.Fatalf("minItems = %#v (%T), want provider-safe number", minimum, minimum)
	}
}

func TestToolSchemasPreserveRegistrationOrder(t *testing.T) {
	first := MustTool(Tool[struct{}]{Name: "first", Execute: testNoopTool[struct{}]})
	second := MustTool(Tool[struct{}]{Name: "second", Execute: testNoopTool[struct{}]})
	droid := Droid{orderedToolSchemas: []ToolSchema{first.schema(), second.schema()}}

	schemas := droid.providerToolSchemas()
	if len(schemas) != 2 || schemas[0].Name != "first" || schemas[1].Name != "second" {
		t.Fatalf("tool schemas = %+v, want first then second", schemas)
	}
}

func TestToolExecutionStrictlyDecodesOneObject(t *testing.T) {
	type args struct {
		Value string `json:"value"`
	}
	tool := MustTool(Tool[args]{
		Name: "strict",
		Execute: func(_ context.Context, _ ToolContext, input args, _ ToolUpdate) (ToolResult, error) {
			return ToolText(input.Value), nil
		},
	})
	for _, raw := range []string{
		`null`,
		`[]`,
		`{"value":"ok","unknown":true}`,
		`{"value":"ok"} {"value":"again"}`,
	} {
		if _, err := tool.execute(context.Background(), ToolContext{}, []byte(raw), nil); err == nil {
			t.Errorf("execute(%s) error = nil", raw)
		}
	}
	result, err := tool.execute(context.Background(), ToolContext{}, []byte(`{"value":"ok"}`), nil)
	if err != nil {
		t.Fatalf("valid execution error = %v", err)
	}
	if got := result.Content[0].(TextContent).Text; got != "ok" {
		t.Fatalf("valid execution text = %q", got)
	}
}

func testNoopTool[Args any](context.Context, ToolContext, Args, ToolUpdate) (ToolResult, error) {
	return ToolText("ok"), nil
}
