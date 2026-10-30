package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/modelimage"
	"github.com/akonwi/kit/internal/saveattachment"
	"github.com/akonwi/kit/internal/subagent"
)

// pluginImageStore keeps the images plugin tools return as session
// attachments, so clients can show them with the call and the model can copy
// them with save_attachment. Without a store, or for an image the store
// refuses, the image stays inline for the model only.
type pluginImageStore struct {
	sessionID string
	store     attachment.Store
}

// content returns the image block followed, when a store is configured, by
// the model-visible line naming its attachment or saying it was not kept.
func (images pluginImageStore) content(ctx context.Context, toolName, mediaType string, data []byte) []droids.ResultContent {
	inline := droids.NewImageData(mediaType, data)
	if images.store == nil || images.sessionID == "" {
		return []droids.ResultContent{inline}
	}
	unavailable := []droids.ResultContent{inline, droids.TextContent{Text: saveattachment.Unavailable}}
	extension := strings.TrimPrefix(mediaType, "image/")
	if extension == "jpeg" {
		extension = "jpg"
	}
	input, err := attachment.InspectImage(toolName+"."+extension, bytes.NewReader(data), modelimage.Limits())
	if err != nil || input.MediaType != mediaType {
		return unavailable
	}
	input.SessionID = images.sessionID
	record, err := images.store.Put(ctx, input)
	if err != nil {
		return unavailable
	}
	file := droids.NewFileData(record.Filename, record.MediaType, data)
	file.AttachmentID = record.ID
	return []droids.ResultContent{file, droids.TextContent{Text: saveattachment.Reference(record)}}
}

// PluginTool is the session-facing declaration of one executable registration.
type PluginTool struct {
	ID, Instance, ModelName, Description, ExecutionMode, PromptSnippet string
	PromptGuidelines                                                   []string
	InputSchema                                                        json.RawMessage
	// Client marks a tool hosted by an attached client rather than a plugin
	// process (ADR 0051). Only client-hosted tools reach subagents.
	Client bool
}

// PluginToolCall identifies one model tool call and the agent that made it.
// TurnID is set for the session's primary agent and SubagentConversationID
// for a subagent.
type PluginToolCall struct {
	ID                     string
	TurnID                 string
	SubagentConversationID string
}

// PluginToolContent is one host-validated text or base64 image result part.
type PluginToolContent struct{ Type, Text, Data, MIMEType string }

// PluginToolResult carries host-validated results across the composition port.
type PluginToolResult struct {
	Content   []PluginToolContent
	Details   json.RawMessage
	Terminate bool
}

// PluginToolHost exposes immutable catalogs and generation-fenced execution.
type PluginToolHost interface {
	Tools() []PluginTool
	ExecuteTool(context.Context, PluginTool, PluginToolCall, json.RawMessage) (PluginToolResult, error)
}

type pluginToolState struct{ warning atomic.Pointer[string] }

// refreshPluginContributions runs on the host's serialized notification worker,
// outside runtime authority. It atomically composes executable tools and
// subagent prompt metadata so neither contribution class overwrites the other.
func (r *runtime) refreshPluginContributions() {
	toolHost, hasTools := r.plugins.(PluginToolHost)
	subagentHost, hasSubagents := r.plugins.(PluginSubagentHost)
	if !hasTools && !hasSubagents {
		return
	}
	r.pluginContributions.mu.Lock()
	defer r.pluginContributions.mu.Unlock()
	var toolsCatalog []PluginTool
	if hasTools {
		toolsCatalog = toolHost.Tools()
	}
	var subagentCatalog []PluginSubagent
	if hasSubagents {
		subagentCatalog = subagentHost.Subagents()
	}
	fingerprint := pluginContributionFingerprint(toolsCatalog, subagentCatalog)
	if r.pluginContributions.fingerprint == fingerprint {
		return
	}
	tools, toolPrompt, err := projectPluginTools(toolHost, toolsCatalog, primaryPluginToolCall, r.pluginImages)
	var clientTools []droids.AnyTool
	var clientPrompt string
	if err == nil {
		var clientCatalog []PluginTool
		for _, tool := range toolsCatalog {
			if tool.Client {
				clientCatalog = append(clientCatalog, tool)
			}
		}
		clientTools, clientPrompt, err = projectPluginTools(toolHost, clientCatalog, subagentPluginToolCall, r.pluginImages)
	}
	var pluginDefinitions []subagent.Definition
	var subagentPrompt string
	if err == nil {
		pluginDefinitions, subagentPrompt, err = projectPluginSubagents(subagentCatalog)
	}
	var effective subagent.LoadResult
	if err == nil {
		effective, err = mergeSubagentCatalog(r.pluginContributions.base, pluginDefinitions)
	}
	if err == nil {
		droid := r.pluginContributions.droid.Load()
		if droid == nil {
			err = fmt.Errorf("plugin contribution droid is unavailable")
		} else {
			err = droid.SetAdditionalTools(tools, toolPrompt+subagentPrompt)
		}
	}
	if err != nil {
		// Never leave revoked or incoherent contributions advertised.
		if droid := r.pluginContributions.droid.Load(); droid != nil {
			_ = droid.SetAdditionalTools(nil, "")
		}
		warning := pluginContributionWarning(err)
		r.pluginToolState.warning.Store(&warning)
		r.clientTools.publish(nil, "")
		r.pluginContributions.effective = cloneSubagentLoadResult(r.pluginContributions.base)
		r.pluginContributions.plugins = nil
		r.pluginContributions.fingerprint = [32]byte{}
	} else {
		r.pluginToolState.warning.Store(nil)
		r.clientTools.publish(clientTools, clientPrompt)
		r.pluginContributions.effective = effective
		r.pluginContributions.plugins = append([]PluginSubagent(nil), subagentCatalog...)
		r.pluginContributions.fingerprint = fingerprint
	}
}

// primaryPluginToolCall identifies a call by the session's primary agent.
func primaryPluginToolCall(call droids.ToolContext) PluginToolCall {
	return PluginToolCall{ID: string(call.ToolCallID), TurnID: string(call.TurnID)}
}

// subagentPluginToolCall identifies a call by a subagent, whose droid
// conversation is the subagent conversation.
func subagentPluginToolCall(call droids.ToolContext) PluginToolCall {
	return PluginToolCall{ID: string(call.ToolCallID), SubagentConversationID: string(call.ConversationID)}
}

func projectPluginTools(host PluginToolHost, catalog []PluginTool, caller func(droids.ToolContext) PluginToolCall, images pluginImageStore) ([]droids.AnyTool, string, error) {
	var tools []droids.AnyTool
	var prompt strings.Builder
	for _, tool := range catalog {
		var parameters map[string]any
		decoder := json.NewDecoder(bytes.NewReader(tool.InputSchema))
		decoder.UseNumber()
		if err := decoder.Decode(&parameters); err != nil {
			return nil, "", err
		}
		bound, err := droids.NewTool(droids.Tool[json.RawMessage]{RegistrationID: tool.Instance + ":" + tool.ID, Name: tool.ModelName, Description: tool.Description, Parameters: parameters, Mode: droids.ExecutionMode(tool.ExecutionMode), Execute: func(ctx context.Context, call droids.ToolContext, input json.RawMessage, _ droids.ToolUpdate) (droids.ToolResult, error) {
			result, err := host.ExecuteTool(ctx, tool, caller(call), input)
			if err != nil {
				return droids.ToolResult{}, err
			}
			projected := droids.ToolResult{Details: result.Details, Terminate: result.Terminate}
			for _, content := range result.Content {
				switch content.Type {
				case "text":
					projected.Content = append(projected.Content, droids.TextContent{Text: content.Text})
				case "image":
					data, err := base64.StdEncoding.Strict().DecodeString(content.Data)
					if err != nil {
						return droids.ToolResult{}, err
					}
					projected.Content = append(projected.Content, images.content(ctx, tool.ModelName, content.MIMEType, data)...)
				default:
					return droids.ToolResult{}, fmt.Errorf("invalid plugin tool content")
				}
			}
			return projected, nil
		}})
		if err != nil {
			return nil, "", err
		}
		tools = append(tools, bound)
		if tool.PromptSnippet != "" || len(tool.PromptGuidelines) > 0 {
			fmt.Fprintf(&prompt, "\n\nTool %s:\n", tool.ModelName)
			if tool.PromptSnippet != "" {
				prompt.WriteString(tool.PromptSnippet)
				prompt.WriteByte('\n')
			}
			for _, guideline := range tool.PromptGuidelines {
				prompt.WriteString("- " + guideline + "\n")
			}
		}
	}
	return tools, prompt.String(), nil
}
