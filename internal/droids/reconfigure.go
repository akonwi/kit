package droids

import "fmt"

// RequestConfiguration is the provider-facing configuration sampled before a
// model request. Reconfiguring it does not interrupt a request already in flight.
type RequestConfiguration struct {
	SystemPrompt string
	Reasoning    string
	Tools        []AnyTool
}

type runtimeRequestConfiguration struct {
	systemPrompt string
	reasoning    string
	maxTokens    int
	toolsByName  map[string]AnyTool
	toolSchemas  []ToolSchema
}

func buildRuntimeRequestConfiguration(model Model, config RequestConfiguration) (*runtimeRequestConfiguration, error) {
	maxTokens, err := resolveRequestMaxTokens(model, 0, config.Reasoning)
	if err != nil {
		return nil, err
	}
	result := &runtimeRequestConfiguration{
		systemPrompt: config.SystemPrompt,
		reasoning:    config.Reasoning,
		maxTokens:    maxTokens,
		toolsByName:  make(map[string]AnyTool, len(config.Tools)),
		toolSchemas:  make([]ToolSchema, 0, len(config.Tools)),
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
	rt.requestConfig.Store(next)
	return nil
}

func (rt *sdkRuntime) currentRequestConfiguration() *runtimeRequestConfiguration {
	return rt.requestConfig.Load()
}
