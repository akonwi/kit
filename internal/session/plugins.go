package session

import (
	"context"
	"errors"
)

// PluginSession is the session-owned projection used to construct a plugin host.
// It deliberately contains no runtime, persistence, or renderer objects.
type PluginSession struct {
	ID, Name, CWD string
	SubagentNames []string
}

// PluginTurn is the bounded public projection delivered to session plugins.
type PluginTurn struct {
	ID         string
	Messages   []PluginTurnMessage
	OmitReason string
}

// PluginTurnMessage contains only protocol-public user or assistant text.
type PluginTurnMessage struct {
	Role    string
	Content []string
}

// PluginHost follows a loaded runtime, never a client attachment. Start and
// transition methods must only publish desired state/revoke work, without IO,
// waits, or synchronous callbacks. Close must also work before Start, and must
// not call back into the manager while runtime shutdown holds authority locks.
// PluginSubagentBaseHost receives the names reserved by the applied filesystem
// catalog before plugin registrations become effective.
type PluginSubagentBaseHost interface{ SetSubagentBase([]string) }

type PluginHost interface {
	Start()
	ChangeCWD(string)
	Rename(string)
	Reload()
	TurnStarted(string) bool
	TurnCompleted(PluginTurn)
	Close(context.Context) error
	Warnings() []string
}

// PluginHostFactory constructs an unstarted host outside manager authority locks.
// changed signals catalog/diagnostic updates without retaining a client connection.
// Delivery may be coalesced; clients recover through normal snapshot resynchronization.
type PluginHostFactory func(context.Context, PluginSession, func()) PluginHost

// WithPluginHostFactory enables session-owned external plugins. Concrete process
// and discovery composition belongs to the daemon, not the session package.
func WithPluginHostFactory(factory PluginHostFactory) ManagerOption {
	return func(options *managerOptions) error {
		if factory == nil {
			return errors.New("plugin host factory is required")
		}
		options.pluginFactory = factory
		return nil
	}
}

func runtimeWarnings(loaded *runtime) []string {
	warnings := append([]string(nil), loaded.configurationWarnings...)
	if warning := loaded.pluginToolState.warning.Load(); warning != nil {
		warnings = append(warnings, *warning)
	}
	if loaded.plugins != nil {
		pluginWarnings := loaded.plugins.Warnings()
		if len(pluginWarnings) > 0 && len(warnings) >= 8 {
			warnings = warnings[:7]
		}
		for _, warning := range pluginWarnings {
			if len(warnings) >= 8 {
				break
			}
			warnings = append(warnings, boundedReloadWarning(warning))
		}
	}
	return warnings
}
