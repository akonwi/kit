package openapi

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
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

func TestNestedSubagentLiveEventIsDiscriminatedUnion(t *testing.T) {
	encoded, err := Emit()
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	components := document["components"].(map[string]any)["schemas"].(map[string]any)
	schema := components["SubagentLiveEvent"].(map[string]any)
	discriminator, _ := schema["discriminator"].(map[string]any)
	variants, _ := schema["oneOf"].([]any)
	if discriminator["propertyName"] != "kind" || len(variants) != 9 {
		t.Fatalf("SubagentLiveEvent schema = %#v, want closed nine-variant kind union", schema)
	}
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
	if items["type"] != "array" || items["nullable"] != nil {
		t.Fatalf("slice schema = %#v", items)
	}
	if required, _ := schema["required"].([]string); !contains(required, "items") {
		t.Fatalf("required = %#v", required)
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

func TestStreamOperationPublishesPayloadAndStreamExtension(t *testing.T) {
	encoded, err := Emit()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			OperationID string          `json:"operationId"`
			Tags        []string        `json:"tags"`
			Stream      json.RawMessage `json:"x-kit-stream"`
			Responses   map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	stream := document.Paths["/v1/sessions/{sessionID}/vcs/events"]["get"]
	if stream.OperationID != "streamSessionVCS" || !reflect.DeepEqual(stream.Tags, []string{"vcs"}) {
		t.Fatalf("operation = %q %v", stream.OperationID, stream.Tags)
	}
	var extension map[string]any
	if err := json.Unmarshal(stream.Stream, &extension); err != nil || !reflect.DeepEqual(extension, map[string]any{
		"records": []any{"vcs.status"}, "resumable": false, "maxRecordBytes": float64(65536),
	}) {
		t.Fatalf("x-kit-stream = %s", stream.Stream)
	}
	success := stream.Responses["200"].Content
	if len(success) != 1 || !reflect.DeepEqual(success["text/event-stream"].Schema, map[string]any{"$ref": "#/components/schemas/SessionVCSStatus"}) {
		t.Fatalf("200 content = %#v", success)
	}
	statuses := make([]string, 0, len(stream.Responses))
	for status, response := range stream.Responses {
		statuses = append(statuses, status)
		if status != "200" && response.Content["application/json"].Schema == nil {
			t.Fatalf("%s is not a JSON error body", status)
		}
	}
	sort.Strings(statuses)
	if want := []string{"200", "400", "401", "403", "404", "409", "421", "426", "429", "500", "503"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %v, want %v", statuses, want)
	}
	read := document.Paths["/v1/sessions/{sessionID}/vcs"]["get"]
	if read.OperationID != "getSessionVCS" || read.Stream != nil ||
		!reflect.DeepEqual(read.Responses["200"].Content["application/json"].Schema, map[string]any{"$ref": "#/components/schemas/SessionVCSStatus"}) {
		t.Fatalf("read operation = %+v", read)
	}
	for _, name := range []string{"SessionVCSStatus", "VCSStatus", "VCSHead", "GitHubPullRequest", "CapacityExceededError", "UnavailableError", "ConflictError"} {
		if document.Components.Schemas[name] == nil {
			t.Fatalf("component %s is missing", name)
		}
	}
}

type pointerLeaf struct {
	Note *string `json:"note"`
}

type optionalPointers struct {
	Note    *string      `json:"note,omitempty"`
	Leaf    *pointerLeaf `json:"-"`
	Zero    *int         `json:"zero,omitzero"`
	Items   []string     `json:"items"`
	skipped *string
}

type embeddedOptional struct {
	*optionalPointers
	Name string `json:"name"`
}

type nullPointer struct {
	Count *int `json:"count"`
}

type nestedNullPointer struct {
	Leaves []pointerLeaf `json:"leaves,omitempty"`
}

type embeddedNullPointer struct {
	pointerLeaf
}

func TestOptionalPointerFieldsAreOmitted(t *testing.T) {
	for _, value := range []any{optionalPointers{}, embeddedOptional{}} {
		schema, err := SchemaForTesting(reflect.TypeOf(value))
		if err != nil {
			t.Fatalf("%T: %v", value, err)
		}
		properties := schema["properties"].(map[string]any)
		if _, ok := properties["note"]; !ok {
			t.Fatalf("%T properties = %#v", value, properties)
		}
	}
	schema, _ := SchemaForTesting(reflect.TypeOf(optionalPointers{}))
	if !reflect.DeepEqual(schema["required"], []string{"items"}) {
		t.Fatalf("required = %#v, want only items", schema["required"])
	}
}

func TestNullablePointerFieldsAreRejected(t *testing.T) {
	const rule = "pointer field must be omitempty or omitzero; optional contract fields are omitted, never null"
	for _, test := range []struct {
		typ  reflect.Type
		want string
	}{
		{reflect.TypeOf(nullPointer{}), "nullPointer.Count: " + rule},
		{reflect.TypeOf(nestedNullPointer{}), "pointerLeaf.Note: " + rule},
		{reflect.TypeOf(embeddedNullPointer{}), "pointerLeaf.Note: " + rule},
	} {
		if _, err := SchemaForTesting(test.typ); err == nil || err.Error() != test.want {
			t.Fatalf("%s: err = %v, want %q", test.typ, err, test.want)
		}
	}
}

func TestDocumentEmitsTranscriptContentUnion(t *testing.T) {
	t.Parallel()
	document, err := Emit()
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatal(err)
	}
	schema := decoded.Components.Schemas["TranscriptContent"]
	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 6 {
		t.Fatalf("oneOf = %#v; want six variants", schema["oneOf"])
	}
	discriminator, _ := schema["discriminator"].(map[string]any)
	if discriminator["propertyName"] != "kind" {
		t.Fatalf("discriminator = %#v", discriminator)
	}
}

func TestSchemaForTestingEmitsTranscriptContentUnion(t *testing.T) {
	t.Parallel()
	schema, err := SchemaForTesting(reflect.TypeFor[protocol.TranscriptContent]())
	if err != nil {
		t.Fatalf("SchemaForTesting() error = %v", err)
	}
	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 6 {
		t.Fatalf("oneOf = %#v; want six variants", schema["oneOf"])
	}
	discriminator, _ := schema["discriminator"].(map[string]any)
	if discriminator["propertyName"] != "kind" {
		t.Fatalf("discriminator = %#v", discriminator)
	}
	mapping, _ := discriminator["mapping"].(map[string]any)
	if len(mapping) != 6 || mapping["toolCall"] == nil {
		t.Fatalf("mapping = %#v", mapping)
	}
}
