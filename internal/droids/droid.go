package droids

import "context"

// Droid is one live autonomous conversation opened through Open.
type Droid struct {
	sdk                *sdkRuntime
	providers          Providers
	model              Model
	maxTokens          int
	compactionReserve  int
	toolsByName        map[string]AnyTool
	orderedToolSchemas []ToolSchema
}

// Close shuts the droid down without a deadline. It does not close a Store
// supplied by the caller.
func (d *Droid) Close() error {
	if d == nil {
		return nil
	}
	return d.Shutdown(context.Background())
}

func (d *Droid) providerToolSchemas() []ToolSchema {
	out := make([]ToolSchema, len(d.orderedToolSchemas))
	copy(out, d.orderedToolSchemas)
	return out
}
