package droids

// PromptCacheRetention selects how long a provider retains the prompt cache
// entries a request writes. Providers map it to their own lifetimes and
// pricing; a provider without a choice uses its default.
type PromptCacheRetention string

const (
	// PromptCacheShort is the provider's default lifetime. Anthropic retains
	// entries for five minutes, refreshed on each read. Reviewed earlier OpenAI
	// models send in_memory when that value is supported.
	PromptCacheShort PromptCacheRetention = "short"
	// PromptCacheLong extends the lifetime for conversations whose turns are
	// often minutes apart. Anthropic retains entries for one hour, refreshed on
	// each read, and charges twice the input rate to write them. Reviewed
	// earlier OpenAI models send 24h, which has no separate write price.
	PromptCacheLong PromptCacheRetention = "long"
)
