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
	tool := NewTool(Tool[args]{Name: "forecast"})
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
	tool := NewTool(Tool[struct{}]{
		Name:    "now",
		Execute: func(context.Context, struct{}) (ToolResult, error) { return ToolText("ok"), nil },
	})
	s := tool.schema()
	if s.Parameters["type"] != "object" {
		t.Fatalf("empty-struct schema type = %v, want object", s.Parameters["type"])
	}
}

func TestExplicitParametersOverrideDerivation(t *testing.T) {
	explicit := map[string]any{"type": "object", "properties": map[string]any{}}
	tool := NewTool(Tool[struct {
		X int `json:"x"`
	}]{Name: "t", Parameters: explicit})
	if got := tool.schema().Parameters; got["properties"] == nil ||
		len(got["properties"].(map[string]any)) != 0 {
		t.Fatalf("explicit parameters were not used: %v", got)
	}
}

func TestToolSchemasPreserveRegistrationOrder(t *testing.T) {
	first := NewTool(Tool[struct{}]{Name: "first"})
	second := NewTool(Tool[struct{}]{Name: "second"})
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
	tool := NewTool(Tool[args]{
		Name: "strict",
		Execute: func(_ context.Context, input args) (ToolResult, error) {
			return ToolText(input.Value), nil
		},
	})
	for _, raw := range []string{
		`null`,
		`[]`,
		`{"value":"ok","unknown":true}`,
		`{"value":"ok"} {"value":"again"}`,
	} {
		if _, err := tool.execute(context.Background(), []byte(raw)); err == nil {
			t.Errorf("execute(%s) error = nil", raw)
		}
	}
	result, err := tool.execute(context.Background(), []byte(`{"value":"ok"}`))
	if err != nil {
		t.Fatalf("valid execution error = %v", err)
	}
	if got := result.Content[0].(TextContent).Text; got != "ok" {
		t.Fatalf("valid execution text = %q", got)
	}
}
