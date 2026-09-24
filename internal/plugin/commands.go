package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxCommands bounds the live command catalog for one session.
	MaxCommands = 256
	// MaxCommandArgsBytes bounds literal command arguments before IPC encoding.
	MaxCommandArgsBytes = 64 * 1024
	// MaxContributionParamsBytes bounds encoded registration and toast payloads.
	MaxContributionParamsBytes = 64 * 1024
)

// ErrCommandUnavailable means the selected registration/generation cannot execute.
// Callers must refresh the catalog rather than automatically replay on replacement.
var ErrCommandUnavailable = errors.New("plugin command is unavailable")

var errCommandContextPending = errors.New("plugin command context reconciliation pending")

// Command is an immutable, renderer-neutral command registration. ID is canonical;
// LocalID is the plugin-facing invocation ID and intended picker label. Owner and
// Registration must accompany invocation so stale selections cannot target either
// a replacement process or a same-generation re-registration.
type Command struct {
	ID           string
	LocalID      string
	Owner        InstanceID
	Registration uint64
	Description  string
	ArgName      string
	Category     string
}

// Toast is a validated session-scoped plugin notification. Transient notifications
// are delivered only through the live observer, never retained for reconnect.
type Toast struct {
	Owner                    InstanceID
	Title, Subtitle, Variant string
	Persistent               bool
}

var localContributionID = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*$`)

func localID(value string) bool {
	return len(value) > 0 && len(value) <= 128 && localContributionID.MatchString(value)
}

func contributionText(value string, limit int, required bool) bool {
	if len(value) > limit || !utf8.ValidString(value) || (required && strings.TrimSpace(value) == "") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}

func contributionObject(raw json.RawMessage, allowed ...string) (map[string]json.RawMessage, error) {
	if len(raw) > MaxContributionParamsBytes {
		return nil, rpcError(-32005, "Contribution parameters exceed protocol limits")
	}
	if !utf8.Valid(raw) {
		return nil, rpcError(-32602, "Params must be UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, rpcError(-32602, "Params must be an object")
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, rpcError(-32602, "Invalid parameter name")
		}
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return nil, rpcError(-32602, "Unknown parameter")
		}
		if _, exists := fields[key]; exists {
			return nil, rpcError(-32602, "Duplicate parameter")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, rpcError(-32602, "Invalid parameter value")
		}
		fields[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, rpcError(-32602, "Invalid parameter object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, rpcError(-32602, "Trailing parameter data")
	}
	return fields, nil
}

func contributionString(fields map[string]json.RawMessage, name string, limit int, required bool) (string, error) {
	raw, present := fields[name]
	if !present || bytes.Equal(raw, []byte("null")) {
		if !required {
			return "", nil
		}
		return "", rpcError(-32602, "Missing required parameter: "+name)
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || !contributionText(value, limit, required) {
		return "", rpcError(-32602, "Invalid parameter: "+name)
	}
	return value, nil
}

func parseCommand(raw json.RawMessage) (Command, error) {
	fields, err := contributionObject(raw, "id", "description", "argName", "category")
	if err != nil {
		return Command{}, err
	}
	var command Command
	if command.LocalID, err = contributionString(fields, "id", 128, true); err != nil {
		return Command{}, err
	}
	if !localID(command.LocalID) {
		return Command{}, rpcError(-32602, "Invalid local command id")
	}
	if command.Description, err = contributionString(fields, "description", 1024, true); err != nil {
		return Command{}, err
	}
	if command.ArgName, err = contributionString(fields, "argName", 128, false); err != nil {
		return Command{}, err
	}
	if command.Category, err = contributionString(fields, "category", 128, false); err != nil {
		return Command{}, err
	}
	return command, nil
}

func parseLocalID(raw json.RawMessage) (string, error) {
	fields, err := contributionObject(raw, "id")
	if err != nil {
		return "", err
	}
	id, err := contributionString(fields, "id", 128, true)
	if err != nil {
		return "", err
	}
	if !localID(id) {
		return "", rpcError(-32602, "Invalid local contribution id")
	}
	return id, nil
}

// activeEntryLocked is the common mutation fence. A nil instance is allowed
// during launch because validated init and registration can precede StartInstance
// returning; the RPC reader still enforces ready-state admission in that window.
func (h *Host) activeEntryLocked(owner InstanceID) *hostEntry {
	if h.closed || h.ctx.Err() != nil {
		return nil
	}
	for _, entry := range h.entries {
		if entry.owner != owner || entry.epoch != h.view.epoch || (entry.installation.Source == Project && entry.projectEpoch != h.view.projectEpoch) || entry.failure != nil {
			continue
		}
		if entry.instance != nil && entry.instance.Status().State != InstanceReady {
			return nil
		}
		return entry
	}
	return nil
}

func (h *Host) handleRequest(ctx context.Context, owner InstanceID, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "kit/tool-calls/register-interceptor", "kit/tool-calls/unregister-interceptor":
		return h.handleInterceptorRequest(ctx, owner, method, params)
	case "kit/tools/register", "kit/tools/unregister":
		return h.handleToolRequest(ctx, owner, method, params)
	case "kit/subagents/register", "kit/subagents/unregister":
		return h.handleSubagentRequest(ctx, owner, method, params)
	case "kit/footer/set", "kit/footer/clear", "kit/footer/hide", "kit/footer/show":
		return h.handleFooter(ctx, owner, method, params)
	case "kit/ui/confirm", "kit/ui/input", "kit/ui/select":
		return h.handleInteraction(ctx, owner, method, params)
	case "kit/commands/register":
		command, err := parseCommand(params)
		if err != nil {
			return nil, err
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if h.activeEntryLocked(owner) == nil {
			return nil, rpcError(-32002, "Plugin generation is unavailable")
		}
		h.pruneCommandsLocked()
		command.Owner = owner
		command.ID = owner.PluginID + "." + command.LocalID
		if _, exists := h.commands[command.ID]; exists {
			return nil, rpcError(-32003, "Command is already registered")
		}
		if len(h.commands) >= MaxCommands {
			return nil, rpcError(-32005, "Session command limit exceeded")
		}
		h.nextCommand++
		command.Registration = h.nextCommand
		h.commands[command.ID] = command
		h.publishChange()
		return json.Marshal(struct {
			ID string `json:"id"`
		}{command.ID})
	case "kit/commands/unregister":
		id, err := parseLocalID(params)
		if err != nil {
			return nil, err
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if h.activeEntryLocked(owner) == nil {
			return nil, rpcError(-32002, "Plugin generation is unavailable")
		}
		canonical := owner.PluginID + "." + id
		if command, ok := h.commands[canonical]; ok && command.Owner == owner {
			delete(h.commands, canonical)
			h.publishChange()
		}
		return json.RawMessage("null"), nil
	default:
		return h.unsupportedRequest(ctx, owner, method)
	}
}

func (h *Host) pruneCommandsLocked() {
	owners := make(map[InstanceID]bool)
	for _, command := range h.commands {
		if _, known := owners[command.Owner]; !known {
			owners[command.Owner] = h.activeEntryLocked(command.Owner) != nil
		}
	}
	for id, command := range h.commands {
		if !owners[command.Owner] {
			delete(h.commands, id)
		}
	}
}

// Commands returns a stable canonical-ID ordering. All commands of one owner are
// included or excluded together, including when a process crashes during a read.
func (h *Host) Commands() []Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	var result []Command
	owners := make(map[InstanceID]bool)
	for _, command := range h.commands {
		active, known := owners[command.Owner]
		if !known {
			entry := h.activeEntryLocked(command.Owner)
			active = entry != nil && entry.instance != nil
			owners[command.Owner] = active
		}
		if active {
			result = append(result, command)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// ExecuteCommand invokes exactly the selected owner and registration. Pending
// context reconciliation is an admission wait, not a dispatched call or replay.
// Results must be null; malformed results fail the instance and revoke its catalog.
func (h *Host) ExecuteCommand(ctx context.Context, selection Command, args string) error {
	if len(args) > MaxCommandArgsBytes || !utf8.ValidString(args) || strings.ContainsRune(args, 0) {
		return rpcError(-32602, "Invalid command arguments")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h.mu.Lock()
		command, exists := h.commands[selection.ID]
		entry := h.activeEntryLocked(selection.Owner)
		if !exists || command.Owner != selection.Owner || command.Registration != selection.Registration || entry == nil {
			h.mu.Unlock()
			return ErrCommandUnavailable
		}
		changed := h.stateChanged
		instance := entry.instance
		if instance == nil {
			h.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-h.ctx.Done():
				return ErrCommandUnavailable
			case <-changed:
				continue
			}
		}
		synchronized := entry.context.Project.Cwd == h.view.cwd && sameSessionContext(entry.context.Session, h.view.session)
		h.mu.Unlock()
		if !synchronized {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-instance.Revoked():
				return ErrCommandUnavailable
			case <-changed:
				continue
			}
		}
		params, _ := json.Marshal(struct {
			ID   string `json:"id"`
			Args string `json:"args"`
		}{command.LocalID, args})
		_, err := instance.callAdmitted(ctx, "kit/commands/execute", params, func(raw json.RawMessage) error {
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return errors.New("plugin command result must be null")
			}
			return nil
		}, func(enqueue func() error) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			current, exists := h.commands[selection.ID]
			entry := h.activeEntryLocked(selection.Owner)
			if !exists || current.Owner != selection.Owner || current.Registration != selection.Registration || entry == nil {
				return ErrCommandUnavailable
			}
			if entry.context.Project.Cwd != h.view.cwd || !sameSessionContext(entry.context.Session, h.view.session) {
				return errCommandContextPending
			}
			return enqueue()
		})
		if errors.Is(err, errCommandContextPending) {
			continue
		}
		if err != nil {
			return fmt.Errorf("plugin command %s failed (effects may have partially completed): %w", selection.ID, err)
		}
		return nil
	}
}

func parseToast(raw json.RawMessage) (Toast, error) {
	fields, err := contributionObject(raw, "title", "subtitle", "variant", "persistent")
	if err != nil {
		return Toast{}, err
	}
	var toast Toast
	if toast.Title, err = contributionString(fields, "title", 1024, true); err != nil {
		return Toast{}, err
	}
	// Subtitles can contain plain multiline text, but not terminal controls.
	if value, ok := fields["subtitle"]; ok && !bytes.Equal(value, []byte("null")) {
		if json.Unmarshal(value, &toast.Subtitle) != nil || len(toast.Subtitle) > 4096 || !utf8.ValidString(toast.Subtitle) {
			return Toast{}, rpcError(-32602, "Invalid subtitle")
		}
		for _, r := range toast.Subtitle {
			if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.In(r, unicode.Cf) {
				return Toast{}, rpcError(-32602, "Invalid subtitle")
			}
		}
	}
	if toast.Variant, err = contributionString(fields, "variant", 16, true); err != nil {
		return Toast{}, err
	}
	if toast.Variant != "info" && toast.Variant != "warning" && toast.Variant != "error" {
		return Toast{}, rpcError(-32602, "Invalid toast variant")
	}
	if value, ok := fields["persistent"]; ok {
		if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &toast.Persistent) != nil {
			return Toast{}, rpcError(-32602, "Invalid persistent flag")
		}
	}
	return toast, nil
}

func (h *Host) handleNotification(ctx context.Context, owner InstanceID, method string, params json.RawMessage) error {
	if method != "kit/ui/toast" {
		return nil
	}
	toast, err := parseToast(params)
	if err != nil {
		return nil
	} // Invalid notifications have no response.
	h.mu.Lock()
	if ctx.Err() != nil || h.activeEntryLocked(owner) == nil {
		h.mu.Unlock()
		return nil
	}
	toast.Owner = owner
	if toast.Persistent {
		h.recordDiagnosticLocked(fmt.Sprintf("%s: %s %s", owner.PluginID, toast.Title, toast.Subtitle), false)
	}
	observer := h.config.Toast
	if observer == nil && !toast.Persistent {
		h.recordDiagnosticLocked(fmt.Sprintf("%s: toast delivery is not connected to a client yet", owner.PluginID), false)
	}
	h.mu.Unlock()
	if observer != nil {
		return observer(ctx, toast)
	}
	return nil
}
