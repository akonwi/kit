package mcpconfig

import (
	"encoding/json"
	"fmt"
	"sort"
)

// rawServer is one server entry decoded field by field so a single malformed
// field degrades to a diagnostic instead of discarding the whole entry.
type rawServer struct {
	description *string
	disabled    *bool
	command     *string
	args        *[]string
	env         map[string]string
	cwd         *string
	url         *string
	headers     map[string]string
	auth        *string
	bearerToken *string
	bearerEnv   *string
	// envCleared and headersCleared record an explicit JSON null, which clears
	// values inherited from lower-precedence files instead of merging with them.
	envCleared     bool
	headersCleared bool
}

// mergedServer accumulates entries across files. Scalar fields take the value
// from the highest-precedence file that set them; env and headers merge by key.
type mergedServer struct {
	raw    rawServer
	source File
	// rank is the precedence index of the highest-precedence file that defined
	// this server, used for ceiling eviction.
	rank        int
	commandRank int
	urlRank     int
}

func (m *mergedServer) merge(raw rawServer, file File, rank int) {
	m.source = file
	if raw.description != nil {
		m.raw.description = raw.description
	}
	if raw.disabled != nil {
		m.raw.disabled = raw.disabled
	}
	if raw.command != nil {
		m.raw.command = raw.command
		m.commandRank = rank + 1
	}
	if raw.args != nil {
		m.raw.args = raw.args
	}
	if raw.cwd != nil {
		m.raw.cwd = raw.cwd
	}
	if raw.url != nil {
		m.raw.url = raw.url
		m.urlRank = rank + 1
	}
	if raw.auth != nil {
		m.raw.auth = raw.auth
	}
	if raw.bearerToken != nil {
		m.raw.bearerToken = raw.bearerToken
	}
	if raw.bearerEnv != nil {
		m.raw.bearerEnv = raw.bearerEnv
	}
	m.raw.env = mergeStrings(m.raw.env, raw.env, raw.envCleared)
	m.raw.headers = mergeStrings(m.raw.headers, raw.headers, raw.headersCleared)
}

func mergeStrings(base, override map[string]string, cleared bool) map[string]string {
	if cleared {
		base = nil
	}
	if len(override) == 0 {
		return base
	}
	if base == nil {
		base = map[string]string{}
	}
	for key, value := range override {
		base[key] = value
	}
	return base
}

// decodeServer converts one JSON entry into rawServer, reporting each field
// that could not be used. It returns false when the entry is not an object.
func decodeServer(name string, raw json.RawMessage, file File, result *Result) (rawServer, bool) {
	var server rawServer
	if isNull(raw) {
		result.addDiagnostic(diagnose(file, name, "", "must be an object, not null"))
		return server, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		result.addDiagnostic(diagnose(file, name, "", fmt.Sprintf("must be an object: %v", err)))
		return server, false
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := fields[key]
		if isNull(value) {
			// An explicit null is a tombstone: it clears any value inherited from a
			// lower-precedence file rather than leaving that value in place.
			server.clear(key)
			continue
		}
		switch key {
		case "description":
			server.description = decodeString(value, file, name, key, result)
		case "disabled":
			server.disabled = decodeBool(value, file, name, key, result)
		case "command":
			server.command = decodeString(value, file, name, key, result)
		case "args":
			server.args = decodeStringSlice(value, file, name, key, result)
		case "env":
			server.env = decodeStringMap(value, file, name, key, result)
		case "cwd":
			server.cwd = decodeString(value, file, name, key, result)
		case "url", "baseUrl":
			if decoded := decodeString(value, file, name, key, result); decoded != nil {
				server.url = decoded
			}
		case "headers":
			server.headers = decodeStringMap(value, file, name, key, result)
		case "auth":
			server.auth = decodeString(value, file, name, key, result)
		case "bearerToken":
			server.bearerToken = decodeString(value, file, name, key, result)
		case "bearerTokenEnv":
			server.bearerEnv = decodeString(value, file, name, key, result)
		}
	}
	return server, true
}

func diagnose(file File, server, field, message string) Diagnostic {
	return Diagnostic{Source: file.Source, Path: file.Path, Server: server, Field: field, Message: message}
}

// clear applies an explicit JSON null for one field. Clearing a scalar sets its
// zero value so the merged entry stops inheriting lower-precedence values; this
// is how a project file drops a user-level credential or transport.
func (r *rawServer) clear(key string) {
	var (
		empty   string
		falsey  bool
		noArgs  = []string{}
		noValue = &empty
	)
	switch key {
	case "description":
		r.description = noValue
	case "disabled":
		r.disabled = &falsey
	case "command":
		r.command = noValue
	case "args":
		r.args = &noArgs
	case "env":
		r.env = nil
		r.envCleared = true
	case "cwd":
		r.cwd = noValue
	case "url", "baseUrl":
		r.url = noValue
	case "headers":
		r.headers = nil
		r.headersCleared = true
	case "auth":
		r.auth = noValue
	case "bearerToken":
		r.bearerToken = noValue
	case "bearerTokenEnv":
		r.bearerEnv = noValue
	}
}

func decodeString(raw json.RawMessage, file File, server, field string, result *Result) *string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		result.addDiagnostic(diagnose(file, server, field, "ignored: must be a string"))
		return nil
	}
	return &value
}

func decodeBool(raw json.RawMessage, file File, server, field string, result *Result) *bool {
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		result.addDiagnostic(diagnose(file, server, field, "ignored: must be true or false"))
		return nil
	}
	return &value
}

func decodeStringSlice(raw json.RawMessage, file File, server, field string, result *Result) *[]string {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		result.addDiagnostic(diagnose(file, server, field, "ignored: must be an array of strings"))
		return nil
	}
	if values == nil {
		values = []string{}
	}
	return &values
}

func decodeStringMap(raw json.RawMessage, file File, server, field string, result *Result) map[string]string {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		result.addDiagnostic(diagnose(file, server, field, "ignored: must be an object of string values"))
		return nil
	}
	values := map[string]string{}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var value string
		if err := json.Unmarshal(entries[key], &value); err != nil {
			result.addDiagnostic(diagnose(file, server, field+"."+key, "ignored: must be a string"))
			continue
		}
		values[key] = value
	}
	if len(values) == 0 {
		return nil
	}
	return values
}
