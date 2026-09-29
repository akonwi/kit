package openapi

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/akonwi/kit/internal/httpapi"
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

func TestErrorVariantNames(t *testing.T) {
	for _, test := range []struct {
		tag        string
		code       httpapi.ErrorCode
		hasDetails bool
		want       string
	}{
		{"scratchpad", httpapi.ErrorInstanceMismatch, false, "InstanceMismatchError"},
		{"workspace", httpapi.ErrorNotFound, false, "NotFoundError"},
		{"workspace", httpapi.ErrorNotFound, true, "WorkspaceNotFoundError"},
		{"scratchpad", "scratchpad_revision_conflict", true, "ScratchpadRevisionConflictError"},
		{"scratchpad", "scratchpad_unavailable", false, "ScratchpadUnavailableError"},
		{"diff", "stale_workspace", true, "DiffStaleWorkspaceError"},
		{"annotation", "stale_workspace", false, "AnnotationStaleWorkspaceError"},
	} {
		if got := errorVariantName(test.tag, test.code, test.hasDetails); got != test.want {
			t.Errorf("errorVariantName(%q, %q, %v) = %q, want %q", test.tag, test.code, test.hasDetails, got, test.want)
		}
	}
}

func TestErrorStatusesAreUnionsOfSharedVariants(t *testing.T) {
	encoded, err := Emit()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						Properties struct {
							Error struct {
								OneOf         []map[string]string `json:"oneOf"`
								Discriminator struct {
									PropertyName string            `json:"propertyName"`
									Mapping      map[string]string `json:"mapping"`
								} `json:"discriminator"`
							} `json:"error"`
						} `json:"properties"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	scratchpad := document.Paths["/v1/sessions/{sessionID}/scratchpad"]
	errorUnion := func(method, status string) (map[string]string, []map[string]string, string) {
		union := scratchpad[method].Responses[status].Content["application/json"].Schema.Properties.Error
		return union.Discriminator.Mapping, union.OneOf, union.Discriminator.PropertyName
	}
	ref := func(name string) string { return "#/components/schemas/" + name }
	for _, method := range []string{"get", "put"} {
		mapping, oneOf, property := errorUnion(method, "404")
		if property != "code" || !reflect.DeepEqual(mapping, map[string]string{"not_found": ref("NotFoundError")}) ||
			!reflect.DeepEqual(oneOf, []map[string]string{{"$ref": ref("NotFoundError")}}) {
			t.Fatalf("%s 404 union = %v %v %q", method, mapping, oneOf, property)
		}
	}
	mapping, _, _ := errorUnion("put", "400")
	if want := map[string]string{"invalid_request": ref("InvalidRequestError"), "scratchpad_invalid_content": ref("ScratchpadInvalidContentError")}; !reflect.DeepEqual(mapping, want) {
		t.Fatalf("put 400 mapping = %v, want %v", mapping, want)
	}
	mapping, _, _ = errorUnion("get", "409")
	if want := map[string]string{"instance_mismatch": ref("InstanceMismatchError"), "scratchpad_migration_required": ref("ScratchpadMigrationRequiredError"), "scratchpad_unsupported": ref("ScratchpadUnsupportedError")}; !reflect.DeepEqual(mapping, want) {
		t.Fatalf("get 409 mapping = %v, want %v", mapping, want)
	}
}

func TestConflictingErrorVariantIsRejected(t *testing.T) {
	components := map[string]any{}
	withoutDetails := httpapi.ErrorResponse{Status: 409, Codes: []httpapi.ErrorCode{"stale_workspace"}}
	if _, err := errorEnvelopeSchema("diff", withoutDetails, components); err != nil {
		t.Fatal(err)
	}
	withDetails := httpapi.ErrorResponse{Status: 409, Codes: []httpapi.ErrorCode{"stale_workspace"},
		Details: map[httpapi.ErrorCode]reflect.Type{"stale_workspace": reflect.TypeOf(map[string]string{})}}
	_, err := errorEnvelopeSchema("diff", withDetails, components)
	if err == nil || err.Error() != "error variant DiffStaleWorkspaceError is declared with conflicting schemas" {
		t.Fatalf("conflicting declaration error = %v", err)
	}
}
