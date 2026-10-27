package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	testClientCallID     = "clientcall_0123456789abcdef0123456789abcdef"
	testClientInstanceID = "clientplugin_0123456789abcdef0123456789abcdef"
)

func TestAttachClientPluginInputValidatesDeclarations(t *testing.T) {
	tool := ClientPluginToolDeclaration{ID: "snapshot", Description: "Outline a tab.\nUse refs to act.", InputSchema: json.RawMessage(`{"type":"object"}`), ExecutionMode: "parallel", PromptGuidelines: []string{"Prefer refs"}}
	valid := AttachClientPluginInput{PluginID: "browser", Tools: []ClientPluginToolDeclaration{tool}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (AttachClientPluginInput{PluginID: "browser", Tools: []ClientPluginToolDeclaration{}}).Validate(); err != nil {
		t.Fatalf("empty tool list: %v", err)
	}
	mutate := func(change func(*ClientPluginToolDeclaration)) AttachClientPluginInput {
		copy := tool
		change(&copy)
		return AttachClientPluginInput{PluginID: "browser", Tools: []ClientPluginToolDeclaration{copy}}
	}
	many := make([]ClientPluginToolDeclaration, MaxClientPluginTools+1)
	for index := range many {
		many[index] = tool
		many[index].ID = "t" + strings.Repeat("a", index)
	}
	for name, input := range map[string]AttachClientPluginInput{
		"plugin id":         {PluginID: "Browser", Tools: []ClientPluginToolDeclaration{tool}},
		"missing tools":     {PluginID: "browser"},
		"too many":          {PluginID: "browser", Tools: many},
		"duplicate":         {PluginID: "browser", Tools: []ClientPluginToolDeclaration{tool, tool}},
		"tool id":           mutate(func(tool *ClientPluginToolDeclaration) { tool.ID = "Snap" }),
		"no description":    mutate(func(tool *ClientPluginToolDeclaration) { tool.Description = "" }),
		"mode":              mutate(func(tool *ClientPluginToolDeclaration) { tool.ExecutionMode = "eager" }),
		"schema not object": mutate(func(tool *ClientPluginToolDeclaration) { tool.InputSchema = json.RawMessage(`[]`) }),
		"long name":         mutate(func(tool *ClientPluginToolDeclaration) { tool.ID = strings.Repeat("a", 60) }),
	} {
		if err := input.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, input)
		}
	}
}

func TestCompleteClientPluginCallInputIsAResultOrAnError(t *testing.T) {
	for name, input := range map[string]CompleteClientPluginCallInput{
		"empty result": {},
		"text":         {Content: []ClientPluginContent{{Type: "text", Text: "outline"}}, Details: json.RawMessage(`{"origin":"https://example.com"}`)},
		"image":        {Content: []ClientPluginContent{{Type: "image", Data: "iVBORw0KGgo=", MIMEType: "image/png"}}},
		"error":        {Error: "no tab t1"},
	} {
		if err := input.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v", name, err)
		}
	}
	for name, input := range map[string]CompleteClientPluginCallInput{
		"error and content": {Error: "no", Content: []ClientPluginContent{{Type: "text", Text: "x"}}},
		"unknown type":      {Content: []ClientPluginContent{{Type: "audio"}}},
		"text with data":    {Content: []ClientPluginContent{{Type: "text", Text: "x", Data: "eA=="}}},
		"image type":        {Content: []ClientPluginContent{{Type: "image", Data: "eA==", MIMEType: "image/svg+xml"}}},
		"image data":        {Content: []ClientPluginContent{{Type: "image", Data: "not base64", MIMEType: "image/png"}}},
		"details":           {Details: json.RawMessage(`{`)},
	} {
		if err := input.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, input)
		}
	}
}

func TestClientPluginCallEventsAreSessionScoped(t *testing.T) {
	session := func(payload SessionEventPayload) SessionEvent {
		return SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Payload: payload}
	}
	call := &ClientPluginCall{PluginID: "browser", ToolID: "act", ToolCallID: "call_1", SubagentConversationID: "subagent_1", Input: json.RawMessage(`{"ref":"e3"}`)}
	for name, event := range map[string]SessionEvent{
		"live request":     session(ClientPluginCallRequestedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: testClientInstanceID, ClientPluginCall: call}),
		"withheld request": session(ClientPluginCallRequestedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: testClientInstanceID}),
		"resolution":       session(ClientPluginCallResolvedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: testClientInstanceID, ClientPluginCallResolution: ClientPluginCallWithdrawn}),
	} {
		if err := event.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v", name, err)
		}
	}
	twoAgents := *call
	twoAgents.TurnID = "run_1"
	array := *call
	array.Input = json.RawMessage(`[1]`)
	for name, event := range map[string]SessionEvent{
		"turn scoped":     eventForTest(1, ClientPluginCallResolvedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: testClientInstanceID, ClientPluginCallResolution: ClientPluginCallCompleted}),
		"call id":         session(ClientPluginCallRequestedEvent{ClientPluginCallID: "call_1", ClientPluginInstanceID: testClientInstanceID}),
		"instance id":     session(ClientPluginCallRequestedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: "clientplugin_x"}),
		"two agents":      session(ClientPluginCallRequestedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: testClientInstanceID, ClientPluginCall: &twoAgents}),
		"array input":     session(ClientPluginCallRequestedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: testClientInstanceID, ClientPluginCall: &array}),
		"unknown outcome": session(ClientPluginCallResolvedEvent{ClientPluginCallID: testClientCallID, ClientPluginInstanceID: testClientInstanceID, ClientPluginCallResolution: "lost"}),
	} {
		if err := event.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %+v", name, event)
		}
	}
	var decoded SessionEvent
	err := json.Unmarshal([]byte(`{"streamId":"stream_test","sequence":1,"sessionId":"session_test","kind":"session.name.changed","sessionName":"Renamed","clientPluginCallId":"`+testClientCallID+`"}`), &decoded)
	if err == nil {
		err = decoded.Validate()
	}
	if err == nil {
		t.Fatal("rename accepted a client plugin call id")
	}
}
