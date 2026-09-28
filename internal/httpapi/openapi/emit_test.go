package openapi

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type trickyEnum string

func (trickyEnum) EnumValues() []string { return []string{"one", "two"} }

type trickySchema struct {
	Items []string        `json:"items"`
	Count uint64          `json:"count"`
	Raw   json.RawMessage `json:"raw"`
	Kind  trickyEnum      `json:"kind"`
}

func TestDocumentIsCurrent(t *testing.T) {
	document, err := Emit()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../../api/kit-session.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(document, committed) {
		t.Fatal("api/kit-session.openapi.json is stale; run go run ./internal/httpapi/openapi/cmd/emit")
	}
}

func TestSchemaPostProcessing(t *testing.T) {
	schema, err := SchemaForTesting(reflect.TypeOf(trickySchema{}))
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	items := properties["items"].(map[string]any)
	types := items["type"].([]any)
	if len(types) != 2 || types[0] != "array" || types[1] != "null" {
		t.Fatalf("slice type = %#v", types)
	}
	count := properties["count"].(map[string]any)
	if count["format"] != "uint64" || count["minimum"] != 0 {
		t.Fatalf("uint64 schema = %#v", count)
	}
	raw := properties["raw"].(map[string]any)
	if len(raw) != 0 {
		t.Fatalf("RawMessage schema = %#v", raw)
	}
	kind := properties["kind"].(map[string]any)
	if !reflect.DeepEqual(kind["enum"], []string{"one", "two"}) {
		t.Fatalf("enum = %#v", kind["enum"])
	}
}
