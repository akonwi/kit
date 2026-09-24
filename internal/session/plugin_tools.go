package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/subagent"
)

// PluginTool is the session-facing declaration of one executable registration.
type PluginTool struct {
	ID, Instance, ModelName, Description, ExecutionMode, PromptSnippet string
	PromptGuidelines                                                   []string
	InputSchema                                                        json.RawMessage
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
	ExecuteTool(context.Context, PluginTool, string, json.RawMessage) (PluginToolResult, error)
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
	tools, toolPrompt, err := projectPluginTools(toolHost, toolsCatalog)
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
		r.pluginContributions.effective = cloneSubagentLoadResult(r.pluginContributions.base)
		r.pluginContributions.plugins = nil
		r.pluginContributions.fingerprint = [32]byte{}
	} else {
		r.pluginToolState.warning.Store(nil)
		r.pluginContributions.effective = effective
		r.pluginContributions.plugins = append([]PluginSubagent(nil), subagentCatalog...)
		r.pluginContributions.fingerprint = fingerprint
	}
}

func projectPluginTools(host PluginToolHost, catalog []PluginTool) ([]droids.AnyTool, string, error) {
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
			result, err := host.ExecuteTool(ctx, tool, string(call.ToolCallID), input)
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
					projected.Content = append(projected.Content, droids.NewImageData(content.MIMEType, data))
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
