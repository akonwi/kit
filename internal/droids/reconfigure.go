package droids

import (
	"context"
	"fmt"
)

// RequestConfiguration is the provider-facing configuration sampled before a
// model request. Reconfiguring it does not interrupt a request already in flight.
type RequestConfiguration struct {
	SystemPrompt  string
	Reasoning     string
	ContextWindow int
	Tools         []AnyTool
}

type runtimeRequestConfiguration struct {
	systemPrompt  string
	reasoning     string
	maxTokens     int
	contextWindow int
	toolsByName   map[string]AnyTool
	toolSchemas   []ToolSchema
}

func buildRuntimeRequestConfiguration(model Model, config RequestConfiguration) (*runtimeRequestConfiguration, error) {
	if config.ContextWindow < 0 {
		return nil, fmt.Errorf("droids: ContextWindow must not be negative")
	}
	maxTokens, err := resolveRequestMaxTokens(model, 0, config.Reasoning)
	if err != nil {
		return nil, err
	}
	result := &runtimeRequestConfiguration{
		systemPrompt:  config.SystemPrompt,
		reasoning:     config.Reasoning,
		maxTokens:     maxTokens,
		contextWindow: config.ContextWindow,
		toolsByName:   make(map[string]AnyTool, len(config.Tools)),
		toolSchemas:   make([]ToolSchema, 0, len(config.Tools)),
	}
	for _, tool := range config.Tools {
		if tool == nil {
			return nil, fmt.Errorf("droids: nil tool")
		}
		schema := tool.schema()
		if schema.Name == "" {
			return nil, fmt.Errorf("droids: tool name is required")
		}
		if _, duplicate := result.toolsByName[schema.Name]; duplicate {
			return nil, fmt.Errorf("droids: duplicate tool name %q", schema.Name)
		}
		result.toolsByName[schema.Name] = tool
		result.toolSchemas = append(result.toolSchemas, schema)
	}
	return result, nil
}

// Reconfigure atomically replaces provider-facing request configuration. A
// request already in flight retains its captured configuration; the next one
// observes the replacement.
func (d *Droid) Reconfigure(config RequestConfiguration) error {
	return d.ReconfigureContext(context.Background(), config)
}

// ReconfigureContext durably records pending reasoning changes and atomically
// replaces request configuration without mutating any in-flight request.
func (d *Droid) ReconfigureContext(ctx context.Context, config RequestConfiguration) error {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: Reconfigure requires a droid opened with droids.Spawn")
	}
	next, err := buildRuntimeRequestConfiguration(d.model, config)
	if err != nil {
		return err
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed || rt.shutdownStarted {
		return ErrClosed
	}
	combined, err := mergeAdditionalTools(next, rt.additionalTools, rt.additionalPrompt)
	if err != nil {
		return err
	}
	if err := rt.reconcileReasoningLocked(ctx, config.Reasoning, false); err != nil {
		return err
	}
	rt.baseRequestConfig = next
	rt.requestConfig.Store(combined)
	return nil
}

// SetAdditionalTools atomically replaces independently owned tool contributions.
// Base reconfiguration preserves them; in-flight requests retain their captured
// schema and callbacks. Revocation must also be enforced by each callback owner.
func (d *Droid) SetAdditionalTools(tools []AnyTool, prompt string) error {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: additional tools require a spawned droid")
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed || rt.shutdownStarted {
		return ErrClosed
	}
	next, err := mergeAdditionalTools(rt.baseRequestConfig, tools, prompt)
	if err != nil {
		return err
	}
	rt.additionalTools = append([]AnyTool(nil), tools...)
	rt.additionalPrompt = prompt
	rt.requestConfig.Store(next)
	return nil
}

func mergeAdditionalTools(base *runtimeRequestConfiguration, tools []AnyTool, prompt string) (*runtimeRequestConfiguration, error) {
	next := *base
	next.systemPrompt += prompt
	next.toolSchemas = append([]ToolSchema(nil), base.toolSchemas...)
	next.toolsByName = make(map[string]AnyTool, len(base.toolsByName)+len(tools))
	for name, tool := range base.toolsByName {
		next.toolsByName[name] = tool
	}
	for _, tool := range tools {
		if tool == nil {
			return nil, fmt.Errorf("droids: nil additional tool")
		}
		schema := tool.schema()
		if schema.Name == "" {
			return nil, fmt.Errorf("droids: tool name is required")
		}
		if _, exists := next.toolsByName[schema.Name]; exists {
			return nil, fmt.Errorf("droids: duplicate tool name %q", schema.Name)
		}
		next.toolsByName[schema.Name] = tool
		next.toolSchemas = append(next.toolSchemas, schema)
	}
	return &next, nil
}

func (rt *sdkRuntime) currentRequestConfiguration() *runtimeRequestConfiguration {
	return rt.requestConfig.Load()
}
