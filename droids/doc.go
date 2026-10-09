// Package droids provides an application-neutral runtime for autonomous model
// agents.
//
// Applications configure a Droid with a model, tools, policies, and a Store,
// then use Open to create or restore a conversation. The Droid owns execution,
// retries, compaction, persistence coordination, and settlement after a prompt
// is admitted. Applications supervise it through snapshots, events, and
// lifecycle operations.
//
// MemoryStore provides ephemeral storage. Durable SQLite storage is available
// from the sqlitestore subpackage, and droidstest provides reusable Store
// conformance support.
package droids
