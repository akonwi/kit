package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

type bashAdmission struct {
	id                 string
	value              string
	command            string
	excludeFromContext bool
	abort              atomic.Bool
}

func parseDirectBash(value string) (command string, excludeFromContext, ok bool) {
	if !strings.HasPrefix(value, "!") {
		return "", false, false
	}
	excludeFromContext = strings.HasPrefix(value, "!!")
	if excludeFromContext {
		command = strings.TrimSpace(value[2:])
	} else {
		command = strings.TrimSpace(value[1:])
	}
	return command, excludeFromContext, command != ""
}

func (s *appState) startDirectBash(value, command string, excludeFromContext bool) {
	if s.bound == nil || s.reloadPending {
		return
	}
	if s.bashStarting || s.activeBashID != "" {
		s.SetState(func() { s.status = "A bash command is already running" })
		return
	}
	executionID := ""
	if s.bashAdmission != nil && s.bashAdmission.value == value && s.bashAdmission.command == command && s.bashAdmission.excludeFromContext == excludeFromContext {
		executionID = s.bashAdmission.id
	} else {
		var err error
		executionID, err = identifier.New("bash_")
		if err != nil {
			s.SetState(func() { s.status = "Bash failed: " + err.Error() })
			return
		}
	}
	admission := &bashAdmission{
		id: executionID, value: value, command: command, excludeFromContext: excludeFromContext,
	}
	bound := s.bound
	operation := s.operation
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.bashAdmission = admission
		s.composer = ""
		s.bashStarting = true
		s.status = "Starting bash…"
		s.bashHistory.Close()
	})
	go func() {
		execution, err := bound.StartBash(s.ctx, executionID, command, excludeFromContext)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation {
				return
			}
			if err != nil {
				s.SetState(func() {
					s.bashStarting = false
					if admission.abort.Load() {
						s.bashAdmission = nil
						s.status = ""
						return
					}
					if s.composer == "" {
						s.composer = value
						s.composerCursorEndGeneration++
					}
					s.status = "Bash failed: " + err.Error()
				})
				if admission.abort.Load() {
					s.abortBashAdmission(bound, executionID)
				}
				return
			}
			s.SetState(func() {
				s.bashStarting = false
				s.bashAdmission = nil
				s.activeBash = execution
				s.activeBashID = execution.ID()
				s.upsertBashExecution(execution.State())
				s.status = ""
			})
			if admission.abort.Load() {
				go func() {
					abortContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					_ = execution.Abort(abortContext)
				}()
			}
			s.watchBash(execution, operation)
		})
	}()
}

func (s *appState) abortBashAdmission(bound sessionclient.Session, executionID string) {
	go func() {
		for attempt := 0; attempt < 6; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := bound.AbortBash(ctx, executionID)
			cancel()
			if err == nil {
				return
			}
			time.Sleep(time.Duration(1<<attempt) * 50 * time.Millisecond)
		}
	}()
}

func (s *appState) resumeBash(bound sessionclient.Session, operation uint64, executionID string) {
	if executionID == "" {
		return
	}
	runtime := s.Context().Runtime()
	go func() {
		execution, err := bound.Bash(s.ctx, executionID)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation {
				return
			}
			if err != nil {
				s.SetState(func() { s.status = "Could not resume bash: " + err.Error() })
				s.scheduleBashResume(operation, executionID)
				return
			}
			s.SetState(func() {
				s.activeBash = execution
				s.activeBashID = execution.ID()
				s.upsertBashExecution(execution.State())
			})
			s.watchBash(execution, operation)
		})
	}()
}

func (s *appState) watchBash(execution sessionclient.BashExecution, operation uint64) {
	runtime := s.Context().Runtime()
	go func() {
		outcome, err := execution.Wait(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation || s.activeBashID != execution.ID() {
				return
			}
			if err != nil {
				s.SetState(func() { s.status = "Bash update failed: " + err.Error() })
				s.scheduleBashResume(operation, execution.ID())
				return
			}
			s.SetState(func() {
				s.activeBash = nil
				s.activeBashID = ""
				s.upsertBashExecution(outcome)
				if s.runPending {
					s.status = "esc abort · ctrl+c detach"
				} else {
					s.status = ""
				}
			})
		})
	}()
}

func (s *appState) scheduleBashResume(operation uint64, executionID string) {
	bound := s.bound
	if bound == nil {
		return
	}
	runtime := s.Context().Runtime()
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}
		runtime.Dispatch(func() {
			if operation == s.operation && s.activeBashID == executionID {
				s.resumeBash(bound, operation, executionID)
			}
		})
	}()
}

func (s *appState) abortBash() {
	execution := s.activeBash
	executionID := s.activeBashID
	bound := s.bound
	if execution == nil && (executionID == "" || bound == nil) {
		return
	}
	s.SetState(func() { s.status = "Stopping bash…" })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if execution != nil {
			_ = execution.Abort(ctx)
			return
		}
		_ = bound.AbortBash(ctx, executionID)
	}()
}

func (s *appState) upsertBashExecution(execution protocol.BashExecution) {
	copy := cloneBashExecution(execution)
	update := func(messages []transcriptMessage) bool {
		for index := range messages {
			if messages[index].Role == "bash" && messages[index].ID == execution.ID {
				messages[index].Bash = &copy
				messages[index].Pending = execution.Status == protocol.BashExecutionRunning
				return true
			}
		}
		return false
	}
	if update(s.messages) || update(s.liveMessages) {
		s.requestTranscriptScroll()
		return
	}
	message := transcriptMessage{
		ID: execution.ID, Role: "bash", Bash: &copy,
		Pending: execution.Status == protocol.BashExecutionRunning,
	}
	if s.runPending {
		s.liveMessages = append(s.liveMessages, message)
	} else {
		s.messages = append(s.messages, message)
	}
	s.requestTranscriptScroll()
}

func findBashExecution(primary, live []transcriptMessage, executionID string) (protocol.BashExecution, bool) {
	for _, messages := range [][]transcriptMessage{primary, live} {
		for _, message := range messages {
			if message.Role == "bash" && message.ID == executionID && message.Bash != nil {
				return *message.Bash, true
			}
		}
	}
	return protocol.BashExecution{}, false
}

func cloneBashExecution(execution protocol.BashExecution) protocol.BashExecution {
	if execution.ExitCode != nil {
		exitCode := *execution.ExitCode
		execution.ExitCode = &exitCode
	}
	return execution
}

func (s *appState) openBashHistory(_ int) bool {
	entries := s.bashHistoryEntries()
	opened := false
	s.SetState(func() { opened = s.bashHistory.OpenFor(entries, s.composer) })
	return opened
}

func (s *appState) bashHistoryEntries() []bashHistoryEntry {
	messages := make([]transcriptMessage, 0, len(s.messages)+len(s.liveMessages))
	messages = append(messages, s.messages...)
	messages = append(messages, s.liveMessages...)
	entries := make([]bashHistoryEntry, 0)
	for index := len(messages) - 1; index >= 0; index-- {
		execution := messages[index].Bash
		if messages[index].Role != "bash" || execution == nil || execution.Status == protocol.BashExecutionRunning || strings.TrimSpace(execution.Command) == "" {
			continue
		}
		entries = append(entries, bashHistoryEntry{
			ID: execution.ID, Command: execution.Command,
			ExcludeFromContext: execution.ExcludeFromContext,
		})
	}
	return entries
}

func (s *appState) selectBashHistory(_ ui.EventContext, executionID string) {
	entry, ok := s.bashHistory.Selected()
	if executionID != "" && (!ok || entry.ID != executionID) {
		for _, candidate := range s.bashHistory.Entries {
			if candidate.ID == executionID {
				entry, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return
	}
	s.SetState(func() {
		s.composer = bashHistoryComposerText(entry)
		s.composerCursorEndGeneration++
		s.bashHistory.Close()
	})
}

func bashHistoryComposerText(entry bashHistoryEntry) string {
	prefix := "!"
	if entry.ExcludeFromContext {
		prefix = "!!"
	}
	return prefix + entry.Command
}
