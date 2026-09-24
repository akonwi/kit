package tui

import (
	"context"
	"errors"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

func (s *appState) sessionPaletteCommands(snapshot protocol.SessionSnapshot) []paletteCommand {
	var commands []paletteCommand
	if _, ok := s.bound.(sessionclient.ScratchpadSession); ok {
		commands = append(commands, scratchpadPaletteCommand())
	}
	if _, ok := s.bound.(sessionclient.PluginCommandSession); ok {
		commands = append(commands, pluginPaletteCommands(snapshot.PluginCommands)...)
	}
	if s.pluginCommandID != "" {
		for index := range commands {
			if commands[index].Plugin != nil {
				commands[index].DisabledReason = "command running"
			}
		}
	}
	return append(commands, promptPaletteCommands(snapshot.PromptCommands)...)
}

func stalePluginCommandToast() toastInput {
	return toastInput{Title: "Command unavailable", Subtitle: "Plugin commands changed. Select a command again.", Variant: toastWarning}
}

func (s *appState) runPluginPaletteCommand(command protocol.PluginCommand, args string) {
	s.runPluginCommandWithDispatch(command, args, s.Context().Runtime().Dispatch)
}

func (s *appState) runPluginCommandWithDispatch(command protocol.PluginCommand, args string, dispatch func(func())) {
	executor, ok := s.bound.(sessionclient.PluginCommandSession)
	if !ok {
		return
	}
	if s.pluginCommandID != "" {
		s.showPluginCommandToast(toastInput{Title: "Plugin command in progress", Subtitle: s.pluginCommandID + " is still running. Reload to interrupt plugin work.", Variant: toastWarning})
		return
	}
	input := protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: args}
	if err := input.Validate(); err != nil {
		s.showPluginCommandToast(toastInput{Title: "Invalid command arguments", Subtitle: err.Error(), Variant: toastError})
		return
	}
	// One owned request per attachment; never block model work or mutate its draft.
	parent := s.attachmentCtx
	if parent == nil {
		parent = s.ctx
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	bound, operation := s.bound, s.operation
	s.pluginCommandGeneration++
	generation := s.pluginCommandGeneration
	s.replacingPalette = true
	s.inputGeneration++
	s.SetState(func() { s.pluginCommandCancel = cancel; s.setPluginCommandPending(command.ID); s.palette.Close() })
	s.replacingPalette = false
	s.showPluginCommandToast(toastInput{Title: "Running plugin command", Subtitle: command.ID, Variant: toastInfo})
	go func() {
		defer cancel()
		err := executor.ExecutePluginCommand(ctx, input)
		if parent.Err() != nil {
			return
		}
		dispatch(func() {
			if parent.Err() != nil || s.bound != bound || s.operation != operation || s.pluginCommandGeneration != generation {
				return
			}
			s.SetState(func() { s.pluginCommandCancel = nil; s.setPluginCommandPending("") })
			s.showPluginCommandToast(pluginCommandResultToast(command.ID, err))
		})
	}()
}

func pluginCommandResultToast(id string, err error) toastInput {
	if err == nil {
		return toastInput{}
	}
	var failure *protocol.PluginCommandError
	if errors.As(err, &failure) && failure.Code == protocol.PluginCommandUnavailable {
		return stalePluginCommandToast()
	}
	return toastInput{Title: "Plugin command failed", Subtitle: id + ": " + err.Error() + " Effects may have partially completed; the command was not retried.", Variant: toastError}
}

func (s *appState) setPluginCommandPending(id string) {
	s.pluginCommandID = id
	for index := range s.palette.Contributions {
		if s.palette.Contributions[index].Plugin != nil {
			s.palette.Contributions[index].DisabledReason = ""
			if id != "" {
				s.palette.Contributions[index].DisabledReason = "command running"
			}
		}
	}
}

// Invocation feedback replaces its own progress notice and follows attachment
// lifetime; it is separate from eventual server-owned plugin notifications.
func (s *appState) showPluginCommandToast(input toastInput) {
	if s.pluginCommandToastID != 0 {
		s.dismissToast(s.pluginCommandToastID)
	}
	previous := s.toasts.nextID
	if input.Title != "" {
		s.showToast(input)
	}
	s.pluginCommandToastID = 0
	if s.toasts.nextID != previous {
		s.pluginCommandToastID = s.toasts.nextID
	}
}
