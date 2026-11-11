package cli

import (
	"slices"
	"strconv"

	"github.com/spf13/cobra"
)

// viewKinds lists the kinds `kit --view` accepts.
var viewKinds = []string{ViewDiff, ViewFile, ViewHostFile, ViewSubagent, ViewScratchpad}

// envExecutable starts a view with an explicit Kit home, since Rex starts
// commands with the login shell's environment rather than the caller's.
const envExecutable = "/usr/bin/env"

// bindViewFlags adds the flags that select one workspace view.
func bindViewFlags(command *cobra.Command, view *View) {
	flags := command.Flags()
	flags.StringVar(&view.Kind, "view", "", "show only one view of the session: diff, file, host-file, subagent, or scratchpad")
	flags.StringVar(&view.Path, "path", "", "with --view file or host-file, the file to show")
	flags.IntVar(&view.Line, "line", 0, "with --view file or host-file, the line to show")
	flags.StringVar(&view.Conversation, "conversation", "", "with --view subagent, the conversation to show")
	flags.Uint64Var(&view.Annotation, "annotation", 0, "with --view diff or file, the annotation to reveal")
}

// validateView checks the flags of a single-view client.
func validateView(view View, options interactiveOptions) error {
	if view.Kind == "" {
		for flag, set := range map[string]bool{
			"path": view.Path != "", "line": view.Line != 0, "conversation": view.Conversation != "", "annotation": view.Annotation != 0,
		} {
			if set {
				return usagef("--%s requires --view", flag)
			}
		}
		return nil
	}
	if !slices.Contains(viewKinds, view.Kind) {
		return usagef("--view must be diff, file, host-file, subagent, or scratchpad")
	}
	if options.SessionID == "" {
		return usagef("--view requires --session")
	}
	files := view.Kind == ViewFile || view.Kind == ViewHostFile
	switch {
	case files && view.Path == "":
		return usagef("--view %s requires --path", view.Kind)
	case !files && view.Path != "":
		return usagef("--path is only valid with --view file or host-file")
	case !files && view.Line != 0:
		return usagef("--line is only valid with --view file or host-file")
	case view.Line < 0:
		return usagef("--line must not be negative")
	case view.Kind == ViewSubagent && view.Conversation == "":
		return usagef("--view subagent requires --conversation")
	case view.Kind != ViewSubagent && view.Conversation != "":
		return usagef("--conversation is only valid with --view subagent")
	case view.Annotation != 0 && view.Kind != ViewDiff && view.Kind != ViewFile:
		return usagef("--annotation is only valid with --view diff or file")
	}
	return nil
}

// viewCommand is the command line that shows view of sessionID on target's
// server, run by executable with the Kit home home: plain `kit` for that
// home's own server, and `kit attach` for another. A view never starts a
// server.
func viewCommand(executable, home string, target attachTarget, sessionID string, view View) []string {
	command := []string{envExecutable, "KIT_HOME=" + home, executable}
	if !target.own() {
		command = append(command, "attach", attachArgument(target))
	}
	command = append(command, "--session", sessionID, "--view", view.Kind)
	if view.Path != "" {
		command = append(command, "--path", view.Path)
	}
	if view.Line > 0 {
		command = append(command, "--line", strconv.Itoa(view.Line))
	}
	if view.Conversation != "" {
		command = append(command, "--conversation", view.Conversation)
	}
	if view.Annotation != 0 {
		command = append(command, "--annotation", strconv.FormatUint(view.Annotation, 10))
	}
	return command
}

// attachArgument names another server for `kit attach`: its home, URL,
// saved SSH server name, or SSH destination.
func attachArgument(target attachTarget) string {
	server := target.Home
	switch {
	case target.URL != "":
		server = target.URL
	case target.SSH != "" && target.Name != target.SSH:
		server = target.Name
	case target.SSH != "":
		server = target.SSH
	}
	return server
}

// ViewLauncher builds the command lines that show workspace views of
// sessions on the client's server, for the client to run in panes.
type ViewLauncher struct {
	executable string
	home       string
	target     attachTarget
}

// Command returns the command line that shows view of sessionID, or nil
// from a nil ViewLauncher.
func (l *ViewLauncher) Command(sessionID string, view View) []string {
	if l == nil {
		return nil
	}
	return viewCommand(l.executable, l.home, l.target, sessionID, view)
}
