// Package pluginprotocol owns the wire records of Kit plugin protocol
// version 2: the manifest, each method's params and result, each
// notification, and the error codes and reasons (ADR 0065).
//
// Plugins are trusted processes that Kit launches from a manifest and speaks
// to with JSON-RPC 2.0, one message per line, over the plugin's stdin and
// stdout. The normative specification is docs/plugin-protocol/v2.md. The
// JSON Schemas in docs/plugin-protocol/v2 are generated from this package by
// cmd/emit and must not be edited by hand.
//
// Objects Kit sends are open: Kit may add optional members within a version,
// and plugins ignore members they do not know. Objects plugins send are
// closed: Kit rejects members it does not know.
package pluginprotocol
