package protocol

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// PluginCommand is an executable session contribution, separate from prompt templates.
// Instance is opaque and must be retained with a selection until invocation.
type PluginCommand struct {
	ID          string `json:"id"`
	LocalID     string `json:"localId"`
	PluginID    string `json:"pluginId"`
	Instance    string `json:"instance"`
	Description string `json:"description"`
	ArgName     string `json:"argName,omitempty"`
	Category    string `json:"category,omitempty"`
}

var pluginCommandLocalID = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`)
var pluginCommandPluginID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var pluginCommandOwner = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)

// Validate checks bounded, safe command metadata and canonical namespacing.
func (command PluginCommand) Validate() error {
	if !pluginCommandPluginID.MatchString(command.PluginID) || len(command.LocalID) > 128 || !pluginCommandLocalID.MatchString(command.LocalID) || command.ID != command.PluginID+"."+command.LocalID || !pluginCommandOwner.MatchString(command.Instance) || strings.TrimSpace(command.Description) == "" || !validRendererText(command.Description, 1024) || (command.ArgName != "" && !validRendererText(command.ArgName, 128)) || (command.Category != "" && !validRendererText(command.Category, 128)) {
		return errors.New("invalid plugin command metadata")
	}
	return nil
}

// PluginCommandInput invokes precisely one catalog selection with literal arguments.
type PluginCommandInput struct {
	ID       string `json:"id"`
	Instance string `json:"instance"`
	Args     string `json:"args"`
}

// Validate rejects malformed selections before routing or dispatch.
func (input PluginCommandInput) Validate() error {
	plugin, local, ok := strings.Cut(input.ID, ".")
	if !ok || !pluginCommandPluginID.MatchString(plugin) || len(local) > 128 || !pluginCommandLocalID.MatchString(local) || !pluginCommandOwner.MatchString(input.Instance) || len(input.Args) > 64*1024 || !utf8.ValidString(input.Args) || strings.ContainsRune(input.Args, 0) {
		return errors.New("invalid plugin command selection or arguments")
	}
	return nil
}

const (
	// PluginCommandUnavailable identifies a missing or stale catalog selection.
	PluginCommandUnavailable = "plugin_command_unavailable"
	// PluginCommandFailed identifies a callback failure with potentially partial effects.
	PluginCommandFailed = "plugin_command_failed"
)

// PluginCommandError is the bounded public failure of a selected command.
type PluginCommandError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (failure PluginCommandError) Error() string { return failure.Message }

// Validate enforces canonical error codes and renderer-safe explanations.
func (failure PluginCommandError) Validate() error {
	if (failure.Code != PluginCommandUnavailable && failure.Code != PluginCommandFailed) || !validRendererText(failure.Message, 1024) {
		return errors.New("invalid plugin command error")
	}
	return nil
}
