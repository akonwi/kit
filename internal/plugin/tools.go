package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// MaxTools bounds the per-session plugin tool catalog.
const MaxTools = 128

// MaxToolInputBytes bounds tool input before IPC encoding.
const MaxToolInputBytes = 64 * 1024

// MaxToolResultBytes bounds aggregate decoded text and image result content.
const MaxToolResultBytes = 4 * 1024 * 1024

// ErrToolUnavailable rejects revoked or replaced tool registrations.
var ErrToolUnavailable = errors.New("plugin tool is unavailable; do not replay against a replacement")
var toolLocalID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Tool is an immutable generation- and registration-owned tool declaration.
type Tool struct {
	ID, LocalID, ModelName, Label, Description, ExecutionMode, PromptSnippet string
	PromptGuidelines                                                         []string
	InputSchema                                                              json.RawMessage
	Owner                                                                    InstanceID
	Registration                                                             uint64
	validator                                                                *validator.Schema
}

// ToolContent is one validated text or base64 image result part.
type ToolContent struct{ Type, Text, Data, MIMEType string }

// ToolResult is a validated, non-streaming v1 execution result.
type ToolResult struct {
	Content   []ToolContent
	Details   json.RawMessage
	Terminate bool
}

func parseTool(params json.RawMessage) (Tool, error) {
	fields, err := contributionObject(params, "id", "label", "description", "inputSchema", "executionMode", "promptSnippet", "promptGuidelines")
	if err != nil {
		return Tool{}, err
	}
	var tool Tool
	if tool.LocalID, err = contributionString(fields, "id", 64, true); err != nil {
		return tool, err
	}
	if !toolLocalID.MatchString(tool.LocalID) {
		return tool, rpcError(-32602, "Invalid tool id")
	}
	if tool.Label, err = contributionString(fields, "label", 512, false); err != nil {
		return tool, err
	}
	if tool.Label == "" {
		tool.Label = tool.LocalID
	}
	if tool.Description, err = interactionString(fields, "description", 8*1024); err != nil {
		return tool, err
	}
	if tool.Description == "" {
		return tool, rpcError(-32602, "Tool description is required")
	}
	_, modePresent := fields["executionMode"]
	if tool.ExecutionMode, err = contributionString(fields, "executionMode", 16, modePresent); err != nil {
		return tool, err
	}
	if tool.ExecutionMode == "" {
		tool.ExecutionMode = "sequential"
	}
	if tool.ExecutionMode != "sequential" && tool.ExecutionMode != "parallel" {
		return tool, rpcError(-32602, "Invalid execution mode")
	}
	if tool.PromptSnippet, err = interactionString(fields, "promptSnippet", 8*1024); err != nil {
		return tool, err
	}
	if raw, ok := fields["promptGuidelines"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &tool.PromptGuidelines) != nil || len(tool.PromptGuidelines) > 32 {
			return tool, rpcError(-32602, "Invalid prompt guidelines")
		}
		for _, text := range tool.PromptGuidelines {
			if !interactionText(text, 1024) {
				return tool, rpcError(-32602, "Invalid prompt guideline")
			}
		}
	}
	tool.InputSchema = append(json.RawMessage(nil), fields["inputSchema"]...)
	tool.validator, err = compilePluginToolSchema(tool.InputSchema)
	if err != nil {
		return tool, rpcError(-32602, "Invalid or unsupported tool schema: "+diagnosticMessage(err))
	}
	return tool, nil
}

func (h *Host) handleToolRequest(ctx context.Context, owner InstanceID, method string, params json.RawMessage) (json.RawMessage, error) {
	var tool Tool
	var err error
	if method == "kit/tools/register" {
		tool, err = parseTool(params)
	} else {
		var fields map[string]json.RawMessage
		fields, err = contributionObject(params, "id")
		if err == nil {
			tool.LocalID, err = contributionString(fields, "id", 64, true)
			if err == nil && !toolLocalID.MatchString(tool.LocalID) {
				err = rpcError(-32602, "Invalid tool id")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	tool.Owner = owner
	tool.ID = owner.PluginID + "." + tool.LocalID
	tool.ModelName = owner.PluginID + "__" + tool.LocalID
	if len(tool.ModelName) > 64 {
		return nil, rpcError(-32602, "Model-facing tool name exceeds 64 characters")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if h.activeEntryLocked(owner) == nil {
		return nil, rpcError(-32002, "Plugin generation is unavailable")
	}
	h.pruneToolsLocked()
	if method == "kit/tools/unregister" {
		if current, ok := h.tools[tool.ID]; ok && current.Owner == owner {
			delete(h.tools, tool.ID)
			h.publishChange()
		}
		return json.RawMessage("null"), nil
	}
	if _, exists := h.tools[tool.ID]; exists {
		return nil, rpcError(-32003, "Tool is already registered")
	}
	if len(h.tools) >= MaxTools {
		return nil, rpcError(-32005, "Session tool limit exceeded")
	}
	h.nextTool++
	tool.Registration = h.nextTool
	h.tools[tool.ID] = tool
	h.publishChange()
	return json.Marshal(struct {
		ID        string `json:"id"`
		ModelName string `json:"modelName"`
	}{tool.ID, tool.ModelName})
}
func (h *Host) pruneToolsLocked() {
	for id, tool := range h.tools {
		if h.activeEntryLocked(tool.Owner) == nil {
			delete(h.tools, id)
		}
	}
}

// Tools returns a detached snapshot without waiting for process IO.
func (h *Host) Tools() []Tool {
	h.mu.Lock()
	defer h.mu.Unlock()
	var result []Tool
	for _, tool := range h.tools {
		if h.activeEntryLocked(tool.Owner) == nil {
			continue
		}
		tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
		tool.PromptGuidelines = append([]string(nil), tool.PromptGuidelines...)
		tool.validator = nil
		result = append(result, tool)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// ExecuteTool validates input and dispatches only to the captured registration.
func (h *Host) ExecuteTool(ctx context.Context, selection Tool, callID string, input json.RawMessage) (ToolResult, error) {
	if len(input) > MaxToolInputBytes || !contributionText(callID, 512, true) {
		return ToolResult{}, rpcError(-32602, "Invalid tool call or input size")
	}
	value, err := decodeToolJSON(input)
	if err != nil {
		return ToolResult{}, rpcError(-32602, "Invalid tool input")
	}
	if _, ok := value.(map[string]any); !ok {
		return ToolResult{}, rpcError(-32602, "Tool input must be an object")
	}
	h.mu.Lock()
	tool, ok := h.tools[selection.ID]
	h.mu.Unlock()
	if !ok || tool.Owner != selection.Owner || tool.Registration != selection.Registration {
		return ToolResult{}, ErrToolUnavailable
	}
	if err := tool.validator.Validate(value); err != nil {
		return ToolResult{}, rpcError(-32602, "Tool input does not match schema")
	}
	params, err := json.Marshal(struct {
		ID     string          `json:"id"`
		CallID string          `json:"toolCallId"`
		Input  json.RawMessage `json:"input"`
	}{tool.LocalID, callID, input})
	if err != nil {
		return ToolResult{}, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return ToolResult{}, err
		}
		h.mu.Lock()
		current, exists := h.tools[tool.ID]
		entry := h.activeEntryLocked(tool.Owner)
		changed := h.stateChanged
		if !exists || current.Registration != tool.Registration || current.Owner != tool.Owner || entry == nil {
			h.mu.Unlock()
			return ToolResult{}, ErrToolUnavailable
		}
		instance := entry.instance
		synchronized := instance != nil && entry.context.Project.Cwd == h.view.cwd && sameSessionContext(entry.context.Session, h.view.session)
		h.mu.Unlock()
		if !synchronized {
			var revoked <-chan struct{}
			if instance != nil {
				revoked = instance.Revoked()
			}
			select {
			case <-revoked:
				return ToolResult{}, ErrToolUnavailable
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			case <-h.ctx.Done():
				return ToolResult{}, ErrToolUnavailable
			case <-changed:
				continue
			}
		}
		var result ToolResult
		_, err := instance.callAdmitted(ctx, "kit/tools/execute", params, func(raw json.RawMessage) error { var err error; result, err = parseToolResult(raw); return err }, func(enqueue func() error) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			current, ok := h.tools[tool.ID]
			entry := h.activeEntryLocked(tool.Owner)
			if !ok || current.Owner != tool.Owner || current.Registration != tool.Registration || entry == nil {
				return ErrToolUnavailable
			}
			if entry.context.Project.Cwd != h.view.cwd || !sameSessionContext(entry.context.Session, h.view.session) {
				return errCommandContextPending
			}
			return enqueue()
		})
		if errors.Is(err, errCommandContextPending) {
			continue
		}
		if err != nil {
			return ToolResult{}, fmt.Errorf("plugin tool %s failed (effects may have partially completed): %w", tool.ID, err)
		}
		return result, nil
	}
}

func parseToolResult(raw json.RawMessage) (ToolResult, error) {
	if len(raw) > MaxFrameBytes {
		return ToolResult{}, errors.New("tool result exceeds frame limit")
	}
	value, err := decodeToolJSON(raw)
	if err != nil {
		return ToolResult{}, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ToolResult{}, errors.New("tool result must be an object")
	}
	for key := range object {
		if key != "content" && key != "details" && key != "terminate" {
			return ToolResult{}, errors.New("unknown tool result field")
		}
	}
	parts, ok := object["content"].([]any)
	if !ok || len(parts) > 64 {
		return ToolResult{}, errors.New("tool content must be a bounded array")
	}
	var result ToolResult
	if value, present := object["terminate"]; present {
		flag, ok := value.(bool)
		if !ok {
			return result, errors.New("terminate must be boolean")
		}
		result.Terminate = flag
	}
	if value, present := object["details"]; present {
		result.Details, err = json.Marshal(value)
		if err != nil || len(result.Details) > 64*1024 {
			return result, errors.New("tool details exceed limit")
		}
	}
	contentBytes := 0
	for _, value := range parts {
		part, ok := value.(map[string]any)
		if !ok {
			return result, errors.New("invalid tool content")
		}
		kind, _ := part["type"].(string)
		content := ToolContent{Type: kind}
		switch kind {
		case "text":
			text, ok := part["text"].(string)
			if !ok || len(part) != 2 {
				return result, errors.New("invalid text content")
			}
			content.Text = text
			contentBytes += len(text)
		case "image":
			data, ok := part["data"].(string)
			mime, mimeOK := part["mimeType"].(string)
			if !ok || !mimeOK || len(part) != 3 {
				return result, errors.New("invalid image content")
			}
			switch mime {
			case "image/png", "image/jpeg", "image/gif", "image/webp":
			default:
				return result, errors.New("unsupported tool image MIME type")
			}
			decoded, err := base64.StdEncoding.Strict().DecodeString(data)
			if err != nil || len(decoded) == 0 {
				return result, errors.New("invalid base64 image")
			}
			contentBytes += len(decoded)
			content.Data = data
			content.MIMEType = mime
		default:
			return result, errors.New("unsupported tool content type")
		}
		if contentBytes > MaxToolResultBytes {
			return result, errors.New("tool content exceeds aggregate limit")
		}
		result.Content = append(result.Content, content)
	}
	return result, nil
}
