package droids

import (
	"strings"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

// OpenAI prompt-cache controls are an explicit, reviewed endpoint policy.
// Catalog cache prices and experimental metadata do not enable them.

func (p *openAIProvider) cacheRetention() PromptCacheRetention {
	if p.promptCacheRetention == nil {
		return PromptCacheShort
	}
	return p.promptCacheRetention()
}

func openAIPublicResponses(model Model) bool {
	return model.API == ModelAPIOpenAIResponses && strings.TrimRight(model.BaseURL, "/") == defaultOpenAIBaseURL
}

// openAIPromptCacheOptionsModel reports GPT-5.6 and later Responses models,
// which use prompt_cache_options. The rule is the prompt-caching guide's
// generation boundary, not a catalog cache_write price.
func openAIPromptCacheOptionsModel(id string) bool {
	return id == "gpt-5.6" || strings.HasPrefix(id, "gpt-5.6-") || id == "gpt-6" || strings.HasPrefix(id, "gpt-6-")
}

// openAIExtendedRetentionModel reports earlier models whose documented
// prompt_cache_retention values were reviewed. GPT-5.5 and GPT-5.5 Pro accept
// only 24h. The other IDs accept in_memory and 24h. Snapshots and siblings
// are not included by prefix.
func openAIExtendedRetentionModel(id string) (inMemory bool, ok bool) {
	switch id {
	case "gpt-5.5", "gpt-5.5-pro":
		return false, true
	case "gpt-5.4", "gpt-5.2", "gpt-5.1-codex-max", "gpt-5.1", "gpt-5.1-codex",
		"gpt-5.1-codex-mini", "gpt-5.1-chat-latest", "gpt-5", "gpt-5-codex", "gpt-4.1":
		return true, true
	default:
		return false, false
	}
}

// applyOpenAIPromptCache sets public Responses cache controls. Codex, custom
// base URLs, and models outside the reviewed lists are left unchanged so a
// gateway is not sent fields it may reject. Prompt-cache keys, prewarm, and
// comparison IDs are omitted: Kit has no per-customer cache split, and a
// per-request key would hurt routing on earlier models.
//
// GPT-5.6 and later keep implicit mode, which breakpoints the latest eligible
// message, and add one explicit breakpoint at the end of the stable system
// prompt. Top-level instructions cannot carry that breakpoint, so the prompt
// is sent as the first developer input item instead of being duplicated in
// instructions. The ttl is the only documented value, 30m; the short/long
// setting does not change it. Earlier reviewed models map long to 24h and
// short to in_memory when that value is supported.
func applyOpenAIPromptCache(model Model, retention PromptCacheRetention, params *responses.ResponseNewParams) {
	if !openAIPublicResponses(model) || params == nil {
		return
	}
	if openAIPromptCacheOptionsModel(model.ID) {
		params.PromptCacheOptions = responses.ResponseNewParamsPromptCacheOptions{
			Mode: "implicit",
			Ttl:  "30m",
		}
		if params.Instructions.Valid() && params.Instructions.Value != "" {
			text := params.Instructions.Value
			params.Instructions = param.Opt[string]{}
			params.Input.OfInputItemList = append(
				responses.ResponseInputParam{openAIDeveloperCacheBoundary(text)},
				params.Input.OfInputItemList...,
			)
		}
		return
	}
	inMemory, ok := openAIExtendedRetentionModel(model.ID)
	if !ok {
		return
	}
	if retention == PromptCacheLong {
		params.PromptCacheRetention = responses.ResponseNewParamsPromptCacheRetention24h
		return
	}
	if inMemory {
		params.PromptCacheRetention = responses.ResponseNewParamsPromptCacheRetentionInMemory
	}
}

func openAIDeveloperCacheBoundary(text string) responses.ResponseInputItemUnionParam {
	block := responses.ResponseInputContentParamOfInputText(text)
	block.OfInputText.PromptCacheBreakpoint = responses.NewResponseInputTextPromptCacheBreakpointParam()
	return responses.ResponseInputItemParamOfMessage(
		responses.ResponseInputMessageContentListParam{block},
		responses.EasyInputMessageRoleDeveloper,
	)
}

// openAIUsage projects Responses usage into Kit's categories. cached_tokens
// and cache_write_tokens are a breakdown of input_tokens, not additional
// tokens. Input is the uncached remainder so calculateCost prices each
// category once. CacheWrite1h stays zero: OpenAI does not use Anthropic's
// one-hour write price. A breakdown larger than input_tokens is left negative
// for validateUsageTokens to reject.
func openAIUsage(u responses.ResponseUsage) Usage {
	cacheRead := int(u.InputTokensDetails.CachedTokens)
	cacheWrite := int(u.InputTokensDetails.CacheWriteTokens)
	return Usage{
		Input:       int(u.InputTokens) - cacheRead - cacheWrite,
		Output:      int(u.OutputTokens),
		CacheRead:   cacheRead,
		CacheWrite:  cacheWrite,
		Reasoning:   int(u.OutputTokensDetails.ReasoningTokens),
		TotalTokens: int(u.TotalTokens),
	}
}
