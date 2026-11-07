package server

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	kitannotation "github.com/akonwi/kit/internal/annotation"
	kitscratchpad "github.com/akonwi/kit/internal/scratchpad"
	kitsession "github.com/akonwi/kit/internal/session"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
	"github.com/getkin/kin-openapi/openapi3"
)

const (
	contractEventSession = "session_0123456789abcdef0123456789abcdef"
	contractEventStream  = "stream_0123456789abcdef0123456789abcdef"
	contractEventTurn    = "run_0123456789abcdef0123456789abcdef"
)

// sessionEventContractFixtures returns one runtime event of every kind a
// session emits, plus the shapes whose zero or withheld fields the wire
// omits. Each is valid for the runtime and projects to a valid protocol event.
func sessionEventContractFixtures(t *testing.T) []kitsession.NewEvent {
	t.Helper()
	at := time.Date(2026, 3, 23, 12, 34, 56, 0, time.UTC)
	text := func(value string) []kitsession.TranscriptContent {
		return []kitsession.TranscriptContent{{Kind: kitsession.TranscriptContentText, Text: value}}
	}
	turn := func(event kitsession.NewEvent) kitsession.NewEvent {
		event.TurnID, event.RunID = contractEventTurn, contractEventTurn
		return event
	}
	annotation := &kitannotation.Record{
		ID: 7, SessionID: contractEventSession,
		Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
			WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4",
			Path:        "internal/tui/app.go", FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A",
			StartLine: 10, EndLine: 12,
		}},
		Body: "Keep this bounded.", Preview: kitannotation.Preview{StartLine: 10, EndLine: 12, Text: "one\ntwo\nthree"},
	}
	call := &kitsession.ClientPluginCall{
		ID: "clientcall_0123456789abcdef0123456789abcdef", InstanceID: "clientplugin_0123456789abcdef0123456789abcdef",
		PluginID: "browser", ToolID: "snapshot", ToolCallID: "call_1", TurnID: contractEventTurn, Input: json.RawMessage(`{"tab":"t1"}`),
	}
	resolved := *call
	resolved.Outcome = kitsession.ClientPluginCallCompleted
	return []kitsession.NewEvent{
		turn(kitsession.NewEvent{Kind: kitsession.EventRunStarted, Status: kitsession.RunStatusRunning}),
		turn(kitsession.NewEvent{Kind: kitsession.EventUserMessage, Text: "Explain the change."}),
		turn(kitsession.NewEvent{Kind: kitsession.EventPluginMessage, PluginID: "autoresearch", Text: "Continue."}),
		turn(kitsession.NewEvent{Kind: kitsession.EventAssistantStarted, MessageID: "message_1"}),
		turn(kitsession.NewEvent{Kind: kitsession.EventAssistantTextDelta, MessageID: "message_1", ContentIndex: 1, Delta: "Hello"}),
		turn(kitsession.NewEvent{Kind: kitsession.EventThinkingDelta, MessageID: "message_1", Delta: "Reasoning"}),
		turn(kitsession.NewEvent{Kind: kitsession.EventAssistantCompleted, MessageID: "message_1", Text: "Hello", Thinking: "Reasoning"}),
		turn(kitsession.NewEvent{Kind: kitsession.EventToolPlanned, MessageID: "message_1", ContentIndex: 2, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"go.mod"}`}),
		turn(kitsession.NewEvent{Kind: kitsession.EventToolStarted, ToolCallID: "call_1", ToolName: "read", ArgumentsTruncated: true}),
		turn(kitsession.NewEvent{Kind: kitsession.EventToolUpdated, ToolCallID: "call_1", ToolName: "read", Content: text("partial"), IsError: true}),
		turn(kitsession.NewEvent{Kind: kitsession.EventToolCompleted, ToolCallID: "call_1", ToolName: "read", Content: text("done"), Details: json.RawMessage(`{"lines":2}`)}),
		turn(kitsession.NewEvent{Kind: kitsession.EventToolCompleted, ToolCallID: "call_2", ToolName: "bash", ContentTruncated: true, DetailsOmitted: true}),
		turn(kitsession.NewEvent{Kind: kitsession.EventCompactionStarted, CompactionID: "compact_1"}),
		{Kind: kitsession.EventCompactionCompleted, CompactionID: "compact_2"},
		turn(kitsession.NewEvent{Kind: kitsession.EventCompactionFailed, CompactionID: "compact_3", CompactionFailure: kitsession.CompactionFailureNotAdaptable}),
		turn(kitsession.NewEvent{Kind: kitsession.EventProviderRetryScheduled, ProviderRetry: &kitsession.ProviderRetry{Count: 1, RetryAt: at}}),
		turn(kitsession.NewEvent{Kind: kitsession.EventProviderRetryStarted, ProviderRetry: &kitsession.ProviderRetry{Count: 1}}),
		turn(kitsession.NewEvent{Kind: kitsession.EventContextUpdated, ContextTokens: 1200, ContextWindow: 200000}),
		turn(kitsession.NewEvent{Kind: kitsession.EventContextUpdated, ContextWindow: 200000}),
		turn(kitsession.NewEvent{Kind: kitsession.EventUsageUpdated, Usage: &kitsession.SessionUsage{Input: 3, Output: 2, TotalTokens: 5}}),
		turn(kitsession.NewEvent{Kind: kitsession.EventRunFinished, Status: kitsession.RunStatusCompleted}),
		turn(kitsession.NewEvent{Kind: kitsession.EventRunFinished, Status: kitsession.RunStatusFailed, ErrorKind: kitsession.ProviderErrorRateLimit, ErrorMessage: "rate limited"}),
		{Kind: kitsession.EventSessionRenamed, SessionName: "Renamed"},
		{Kind: kitsession.EventSessionCWDChanged, CWD: t.TempDir()},
		{Kind: kitsession.EventSessionConfigurationChanged, Configuration: &kitsession.SessionConfiguration{ModelProvider: "openai", ModelID: "gpt-5", ThinkingLevel: "high", ConfigurationRevision: 3}},
		{Kind: kitsession.EventFollowUpsChanged, FollowUps: &kitsession.FollowUpQueue{Count: 1, Previews: []string{"next"}, AdmissionFailure: "attachment is unavailable"}},
		{Kind: kitsession.EventFollowUpsChanged, FollowUps: &kitsession.FollowUpQueue{}},
		{Kind: kitsession.EventSubagentChanged, SubagentConversationID: "conversation_1", SubagentTaskID: "task_1"},
		{Kind: kitsession.EventPeerQueryChanged, PeerRequestID: "peer_0123456789abcdef0123456789abcdef"},
		turn(kitsession.NewEvent{Kind: kitsession.EventInteractionRequested, Interaction: &kitsession.InteractionRequest{
			ID: "interaction_0123456789abcdef0123456789abcdef", SessionID: contractEventSession, RunID: contractEventTurn, ToolCallID: "call_1",
			Kind: kitsession.InteractionConfirm, Title: "Proceed?", CreatedAt: at,
		}}),
		{Kind: kitsession.EventInteractionRequested, Interaction: &kitsession.InteractionRequest{
			ID: "interaction_fedcba9876543210fedcba9876543210", SessionID: contractEventSession, Plugin: &kitsession.PluginInteractionOwner{PluginID: "browser", Instance: "plugininstance_1"},
			Kind: kitsession.InteractionConfirm, Title: "Allow?", CreatedAt: at,
		}},
		turn(kitsession.NewEvent{Kind: kitsession.EventInteractionResolved, InteractionID: "interaction_0123456789abcdef0123456789abcdef", InteractionResolution: "answered"}),
		{Kind: kitsession.EventInteractionResolved, InteractionID: "interaction_fedcba9876543210fedcba9876543210", InteractionResolution: "shutdown"},
		{Kind: kitsession.EventAnnotationCreated, AnnotationID: annotation.ID, Annotation: annotation},
		{Kind: kitsession.EventAnnotationUpdated, AnnotationID: annotation.ID, Annotation: annotation},
		{Kind: kitsession.EventAnnotationDeleted, AnnotationID: annotation.ID},
		{Kind: kitsession.EventAnnotationSubmitted, AnnotationIDs: []uint64{annotation.ID, 8}, AcceptedMessageID: "message_0123456789abcdef0123456789abcdef"},
		{Kind: kitsession.EventScratchpadChanged, Scratchpad: &kitscratchpad.Record{OwnerSessionID: contractEventSession, Content: "notes", Revision: 2, UpdatedAt: at}},
		{Kind: kitsession.EventBoundaryCreated, BoundaryID: "cwd:1", BoundaryKind: "cwd", BoundarySource: "user", Content: text("[cwd from user]"), Details: json.RawMessage(`{"version":1}`), CreatedAt: at},
		{Kind: kitsession.EventBoundaryCreated, BoundaryID: "note:1", BoundaryKind: "note", ContentTruncated: true, DetailsOmitted: true, CreatedAt: at},
		{Kind: kitsession.EventURLOpenRequested, URL: "https://example.com/docs"},
		{Kind: kitsession.EventClientPluginCallRequested, ClientPluginCall: call},
		{Kind: kitsession.EventClientPluginCallResolved, ClientPluginCall: &resolved},
	}
}

// sessionEventContractPage projects the fixtures into one event page as the
// server sends them, before live-only fields are withheld.
func sessionEventContractPage(t *testing.T) protocol.SessionEventBatch {
	t.Helper()
	fixtures := sessionEventContractFixtures(t)
	page := kitsession.EventPage{StreamID: contractEventStream, FirstSequence: 1, LastSequence: int64(len(fixtures))}
	for index, fixture := range fixtures {
		fixture.SessionID = contractEventSession
		event := kitsession.Event{NewEvent: fixture, StreamID: contractEventStream, Sequence: int64(index + 1)}
		page.Events = append(page.Events, event)
	}
	batch := runtimeSessionService{workspaces: kitworkspace.NewService()}.projectSessionEventPage(page)
	if len(batch.Events) != len(fixtures) {
		t.Fatalf("projected %d of %d events", len(batch.Events), len(fixtures))
	}
	for index, event := range batch.Events {
		if event.Payload == nil {
			t.Fatalf("event %d (%s) has no protocol payload", index, page.Events[index].Kind)
		}
	}
	if err := batch.Validate(); err != nil {
		t.Fatalf("projected page: %v", err)
	}
	return batch
}

// runtimeEventKinds lists every EventKind constant the session package
// declares, so a new runtime kind cannot skip this contract test.
func runtimeEventKinds(t *testing.T) []kitsession.EventKind {
	t.Helper()
	files, err := filepath.Glob("../session/*.go")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []kitsession.EventKind
	fileSet := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				if identifier, ok := value.Type.(*ast.Ident); !ok || identifier.Name != "EventKind" {
					continue
				}
				for _, raw := range value.Values {
					literal := raw.(*ast.BasicLit)
					kinds = append(kinds, kitsession.EventKind(strings.Trim(literal.Value, `"`)))
				}
			}
		}
	}
	if len(kinds) == 0 {
		t.Fatal("found no runtime event kinds")
	}
	return kinds
}

func publishedSessionEventSchemas(t *testing.T) (*openapi3.T, map[string]string) {
	t.Helper()
	document, err := openapi3.NewLoader().LoadFromFile("../../api/kit-session.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	union := document.Components.Schemas["SessionEvent"].Value
	if union.Discriminator == nil || union.Discriminator.PropertyName != "kind" || len(union.OneOf) != len(union.Discriminator.Mapping) {
		t.Fatalf("SessionEvent is not a kind-discriminated union: %+v", union)
	}
	mapping := map[string]string{}
	for kind, ref := range union.Discriminator.Mapping {
		mapping[kind] = strings.TrimPrefix(ref.Ref, "#/components/schemas/")
	}
	return document, mapping
}

// TestEverySessionEventKindIsPublished keeps the runtime, the protocol union,
// and the published discriminator mapping describing the same kinds.
func TestEverySessionEventKindIsPublished(t *testing.T) {
	t.Parallel()
	_, mapping := publishedSessionEventSchemas(t)
	var declared []string
	for _, variant := range (protocol.SessionEvent{}).UnionVariants() {
		declared = append(declared, variant.Kind)
	}
	var published []string
	for kind := range mapping {
		published = append(published, kind)
	}
	sort.Strings(declared)
	sort.Strings(published)
	if !reflect.DeepEqual(declared, published) {
		t.Fatalf("published kinds = %v\ndeclared kinds = %v", published, declared)
	}

	fixtures := sessionEventContractFixtures(t)
	covered := map[kitsession.EventKind]bool{}
	for _, fixture := range fixtures {
		covered[fixture.Kind] = true
	}
	for _, kind := range runtimeEventKinds(t) {
		if !covered[kind] {
			t.Errorf("runtime event kind %q has no contract fixture", kind)
		}
	}
	emitted := map[string]bool{}
	for _, event := range sessionEventContractPage(t).Events {
		emitted[string(event.Kind())] = true
	}
	for _, kind := range published {
		if !emitted[kind] {
			t.Errorf("published kind %q is not emitted by any runtime fixture", kind)
		}
	}
}

// TestEmittedSessionEventsConformToPublishedSchema checks every emitted event
// shape against its published variant, on the live stream, the event page,
// and the stream's caught-up records, and that each decodes back to the same
// bytes.
func TestEmittedSessionEventsConformToPublishedSchema(t *testing.T) {
	t.Parallel()
	document, mapping := publishedSessionEventSchemas(t)
	batch := sessionEventContractPage(t)

	// The live shape: what a stream bound to the call's instance carries for
	// events that arrive after it opened.
	union := document.Components.Schemas["SessionEvent"].Value
	for index, event := range batch.Events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
		var value map[string]any
		if err := json.Unmarshal(encoded, &value); err != nil {
			t.Fatal(err)
		}
		if err := union.VisitJSON(value); err != nil {
			t.Errorf("event %d does not match SessionEvent: %v\n%s", index, err, encoded)
		}
		variant := document.Components.Schemas[mapping[string(event.Kind())]].Value
		if err := variant.VisitJSON(value); err != nil {
			t.Errorf("event %d does not match %s: %v\n%s", index, mapping[string(event.Kind())], err, encoded)
		}
		var decoded protocol.SessionEvent
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Errorf("event %d does not decode: %v\n%s", index, err, encoded)
			continue
		}
		if again, err := json.Marshal(decoded); err != nil || !bytes.Equal(again, encoded) {
			t.Errorf("event %d round trip = %s, %v; want %s", index, again, err, encoded)
		}
	}

	// The event page, served through the contract conformance middleware.
	mux := http.NewServeMux()
	registerSessionRoutes(mux, eventStreamTestService{batch: batch})
	var contractErr error
	handler := contractConformanceMiddleware(t, mux, func(err error) { contractErr = err })
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+contractEventSession+"/events?stream="+contractEventStream, nil)
	addContractRequestHeaders(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if contractErr != nil || response.Code != http.StatusOK {
		t.Fatalf("event page = %d contract=%v body=%s", response.Code, contractErr, response.Body.String())
	}
	var page protocol.SessionEventBatch
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Events) != len(batch.Events) {
		t.Fatalf("event page decoded %d events: %v", len(page.Events), err)
	}

	// The stream: its first record's events are all caught up, and the
	// service then ends it with a resynchronization record.
	stream := document.Paths.Find("/v1/sessions/{sessionID}/events/stream").Get.Responses.Status(http.StatusOK).Value.Content.Get("text/event-stream").Schema.Value
	streamMux := http.NewServeMux()
	registerSessionRoutes(streamMux, resyncingEventStreamService{eventStreamTestService{batch: batch}})
	streamResponse := httptest.NewRecorder()
	streamMux.ServeHTTP(streamResponse, httptest.NewRequest(http.MethodGet, "/v1/sessions/"+contractEventSession+"/events/stream?stream="+contractEventStream, nil))
	var streamed []map[string]any
	for _, record := range strings.Split(strings.TrimSpace(streamResponse.Body.String()), "\n\n") {
		for _, line := range strings.Split(record, "\n") {
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var value map[string]any
				if err := json.Unmarshal([]byte(data), &value); err != nil {
					t.Fatal(err)
				}
				if err := stream.VisitJSON(value); err != nil {
					t.Fatalf("stream record does not match SessionEventBatch: %v", err)
				}
				streamed = append(streamed, value)
			}
		}
	}
	if len(streamed) != 2 {
		t.Fatalf("stream = %q, want an events record and a resync record", streamResponse.Body.String())
	}
	if events, _ := streamed[0]["events"].([]any); len(events) != len(batch.Events) {
		t.Fatalf("stream record carries %d events, want %d", len(events), len(batch.Events))
	}
}

// resyncingEventStreamService ends an event stream after its first page.
type resyncingEventStreamService struct{ eventStreamTestService }

func (resyncingEventStreamService) WaitEvents(context.Context, string, string, int64) (protocol.SessionEventBatch, error) {
	return protocol.SessionEventBatch{StreamID: "stream_fedcba9876543210fedcba9876543210", ResyncRequired: true, Events: []protocol.SessionEvent{}}, nil
}
