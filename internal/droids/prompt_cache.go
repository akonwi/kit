package droids

// PromptCacheRetention selects how long a provider retains the prompt cache
// entries a request writes. Providers map it to their own lifetimes and
// pricing; a provider without a choice uses its default.
type PromptCacheRetention string

const (
	// PromptCacheShort is the provider's default lifetime. Anthropic retains
	// entries for five minutes, refreshed on each read.
	PromptCacheShort PromptCacheRetention = "short"
	// PromptCacheLong extends the lifetime for conversations whose turns are
	// often minutes apart. Anthropic retains entries for one hour, refreshed on
	// each read, and charges twice the input rate to write them.
	PromptCacheLong PromptCacheRetention = "long"
)
