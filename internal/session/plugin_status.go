package session

import (
	"sync"

	"github.com/akonwi/kit/droids"
)

// PluginStatus is the session status published to plugins: the values the
// session details view shows.
type PluginStatus struct {
	SessionID     string
	Model         string
	ThinkingLevel string
	// ParentID is empty for a session without a parent.
	ParentID, ParentName string
	// ContextWindow is zero before the session knows the model's window.
	ContextTokens, ContextWindow int
	Usage                        SessionUsage
}

// PluginCompaction reports a compaction of the session's conversation.
type PluginCompaction struct {
	ID string
	// TurnID is empty for a compaction outside a turn.
	TurnID    string
	Completed bool
	// FailureReason is empty unless a completed compaction failed.
	FailureReason CompactionFailureReason
}

// PluginStatusHost receives the session's status and compaction events. Both
// methods must return without waiting: the session calls them while holding
// its event log's lock.
type PluginStatusHost interface {
	SetStatus(PluginStatus)
	CompactionChanged(PluginCompaction)
}

// pluginStatusFeed keeps a session's plugin status current from its events.
type pluginStatusFeed struct {
	host PluginStatusHost
	mu   sync.Mutex
	// status is guarded by mu.
	status PluginStatus
}

func newPluginStatusFeed(host PluginStatusHost, record SessionRecord, snapshot droids.Snapshot) *pluginStatusFeed {
	feed := &pluginStatusFeed{host: host, status: PluginStatus{
		SessionID:     record.ID,
		Model:         record.ModelProvider + "/" + record.ModelID,
		ThinkingLevel: record.ThinkingLevel,
		ParentID:      record.ParentSessionID,
		ParentName:    record.ParentSessionName,
		ContextTokens: snapshot.Context.Usage.EstimatedInput,
		ContextWindow: snapshot.Context.Usage.ContextWindow,
		Usage:         projectSessionUsage(snapshot.Usage),
	}}
	if feed.status.ContextWindow <= 0 {
		feed.status.ContextTokens, feed.status.ContextWindow = 0, 0
	}
	host.SetStatus(feed.status)
	return feed
}

// replaceRuntime updates the status for a replacement runtime running
// record's model: the model, thinking level, and the replacement's context,
// whose window may differ from the previous model's. Usage carries over.
func (feed *pluginStatusFeed) replaceRuntime(record SessionRecord, snapshot droids.Snapshot) {
	feed.mu.Lock()
	defer feed.mu.Unlock()
	feed.status.Model = record.ModelProvider + "/" + record.ModelID
	feed.status.ThinkingLevel = record.ThinkingLevel
	feed.setContextLocked(snapshot)
	feed.host.SetStatus(feed.status)
}

// resetContext takes the context from snapshot, for transitions such as a
// client's compaction whose context events the session does not publish.
func (feed *pluginStatusFeed) resetContext(snapshot droids.Snapshot) {
	feed.mu.Lock()
	defer feed.mu.Unlock()
	feed.setContextLocked(snapshot)
	feed.host.SetStatus(feed.status)
}

func (feed *pluginStatusFeed) setContextLocked(snapshot droids.Snapshot) {
	feed.status.ContextTokens = snapshot.Context.Usage.EstimatedInput
	feed.status.ContextWindow = snapshot.Context.Usage.ContextWindow
	if feed.status.ContextWindow <= 0 {
		feed.status.ContextTokens, feed.status.ContextWindow = 0, 0
	}
}

// observe updates the status from newly appended events. The event log calls
// it while holding its lock, so it takes only the feed's lock and the host
// never waits.
func (feed *pluginStatusFeed) observe(events []NewEvent) {
	feed.mu.Lock()
	defer feed.mu.Unlock()
	changed := false
	for _, event := range events {
		switch event.Kind {
		case EventUsageUpdated:
			if event.Usage != nil && *event.Usage != feed.status.Usage {
				feed.status.Usage = *event.Usage
				changed = true
			}
		case EventContextUpdated:
			if event.ContextWindow > 0 && (event.ContextTokens != feed.status.ContextTokens || event.ContextWindow != feed.status.ContextWindow) {
				feed.status.ContextTokens, feed.status.ContextWindow = event.ContextTokens, event.ContextWindow
				changed = true
			}
		case EventSessionConfigurationChanged:
			if configuration := event.Configuration; configuration != nil {
				model := configuration.ModelProvider + "/" + configuration.ModelID
				if model != feed.status.Model || configuration.ThinkingLevel != feed.status.ThinkingLevel {
					feed.status.Model, feed.status.ThinkingLevel = model, configuration.ThinkingLevel
					changed = true
				}
			}
		case EventCompactionStarted:
			feed.host.CompactionChanged(PluginCompaction{ID: event.CompactionID, TurnID: event.TurnID})
		case EventCompactionCompleted:
			feed.host.CompactionChanged(PluginCompaction{ID: event.CompactionID, TurnID: event.TurnID, Completed: true})
		case EventCompactionFailed:
			feed.host.CompactionChanged(PluginCompaction{ID: event.CompactionID, TurnID: event.TurnID, Completed: true, FailureReason: event.CompactionFailure})
		}
	}
	if changed {
		feed.host.SetStatus(feed.status)
	}
}
