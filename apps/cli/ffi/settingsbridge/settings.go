// Package settingsbridge changes one server setting at a time for Ard, which
// cannot build the optional fields of a settings update.
package settingsbridge

import (
	"context"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// updater applies one settings update, as *kit.Client does.
type updater interface {
	UpdateServerSettings(context.Context, protocol.UpdateServerSettingsInput) (protocol.ServerSettings, error)
}

// SetDefaultModel sets the model for new sessions that name none; empty lets
// the server choose.
func SetDefaultModel(ctx context.Context, client *kit.Client, model string) (protocol.ServerSettings, error) {
	return setDefaultModel(ctx, client, model)
}

func setDefaultModel(ctx context.Context, client updater, model string) (protocol.ServerSettings, error) {
	return client.UpdateServerSettings(ctx, protocol.UpdateServerSettingsInput{DefaultModel: &model})
}

// SetCompactionModel sets the model that summarizes context; empty uses each
// session's own model.
func SetCompactionModel(ctx context.Context, client *kit.Client, model string) (protocol.ServerSettings, error) {
	return setCompactionModel(ctx, client, model)
}

func setCompactionModel(ctx context.Context, client updater, model string) (protocol.ServerSettings, error) {
	return client.UpdateServerSettings(ctx, protocol.UpdateServerSettingsInput{CompactionModel: &model})
}

// SetPromptCacheRetention sets how long providers keep prompt caches:
// "short" or "long".
func SetPromptCacheRetention(ctx context.Context, client *kit.Client, retention string) (protocol.ServerSettings, error) {
	return setPromptCacheRetention(ctx, client, retention)
}

func setPromptCacheRetention(ctx context.Context, client updater, retention string) (protocol.ServerSettings, error) {
	value := protocol.PromptCacheRetention(retention)
	return client.UpdateServerSettings(ctx, protocol.UpdateServerSettingsInput{PromptCacheRetention: &value})
}

// SetReadClaudeConfigs sets whether sessions discover Claude Code
// configuration, such as a project's .claude/commands.
func SetReadClaudeConfigs(ctx context.Context, client *kit.Client, enabled bool) (protocol.ServerSettings, error) {
	return setReadClaudeConfigs(ctx, client, enabled)
}

func setReadClaudeConfigs(ctx context.Context, client updater, enabled bool) (protocol.ServerSettings, error) {
	return client.UpdateServerSettings(ctx, protocol.UpdateServerSettingsInput{ReadClaudeConfigs: &enabled})
}
