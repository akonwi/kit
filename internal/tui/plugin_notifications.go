package tui

import (
	"context"
	"errors"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/sessionclient"
)

func (s *appState) watchPluginToasts(bound sessionclient.Session, operation uint64) {
	watcher, ok := bound.(sessionclient.PluginToastSession)
	if !ok {
		return
	}
	if s.pluginToastWatchCancel != nil {
		s.pluginToastWatchCancel()
	}
	ctx, cancel := context.WithCancel(s.attachmentCtx)
	s.pluginToastWatchCancel = cancel
	runtime := s.Context().Runtime()
	stop := func(err error) bool {
		return s.reportDaemonMismatch(runtime, bound, operation, err) || pluginToastWatchTerminal(err)
	}
	deliver := func(toast protocol.PluginToast) bool {
		// At most one callback waits in the UI queue for this subscription.
		applied := make(chan struct{})
		runtime.Dispatch(func() {
			defer close(applied)
			if ctx.Err() == nil && s.bound == bound && s.operation == operation {
				s.showPluginToast(toast)
			}
		})
		select {
		case <-ctx.Done():
			return false
		case <-applied:
			return true
		}
	}
	go runPluginToastWatch(ctx, watcher, time.Second, stop, deliver)
}

// runPluginToastWatch consumes fresh live streams until ctx ends or stop
// reports a terminal failure, waiting retry between transient endings. It
// returns when deliver reports that the watch is no longer current.
func runPluginToastWatch(ctx context.Context, watcher sessionclient.PluginToastSession, retry time.Duration, stop func(error) bool, deliver func(protocol.PluginToast) bool) {
	for ctx.Err() == nil {
		stream, err := watcher.WatchPluginToasts(ctx)
		if stop(err) {
			return
		}
		if err == nil {
			for toast := range stream.Updates() {
				if ctx.Err() != nil || !deliver(toast) {
					return
				}
			}
			if stop(stream.Err()) {
				return
			}
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// pluginToastWatchTerminal reports failures that must stop reconnection
// (ADR 0035); clean endings and transient failures reconnect.
func pluginToastWatchTerminal(err error) bool {
	var terminal *sessionclient.StreamWatchTerminalError
	return errors.As(err, &terminal)
}

func (s *appState) showPluginToast(toast protocol.PluginToast) {
	if toast.Validate() != nil {
		return
	}
	variant := toastInfo
	switch toast.Variant {
	case "warning":
		variant = toastWarning
	case "error":
		variant = toastError
	}
	before := s.toasts.nextID
	s.showToast(toastInput{Title: toast.Title, Subtitle: toast.PluginID + pluginToastSubtitle(toast.Subtitle), Variant: variant, Persistent: toast.Persistent})
	if s.pluginToastIDs == nil {
		s.pluginToastIDs = make(map[uint64]bool)
	}
	if s.toasts.nextID != before {
		s.pluginToastIDs[s.toasts.nextID] = true
	}
	// The shell's bounded toast stack owns eviction. Retain only live IDs.
	live := make(map[uint64]bool)
	for _, record := range s.toasts.Snapshot() {
		live[record.ID] = true
	}
	for id := range s.pluginToastIDs {
		if !live[id] {
			delete(s.pluginToastIDs, id)
		}
	}
}

func pluginToastSubtitle(subtitle string) string {
	if subtitle == "" {
		return ""
	}
	return " " + glyphMiddleDot + " " + subtitle
}

func (s *appState) clearPluginToasts() {
	for id := range s.pluginToastIDs {
		s.dismissToast(id)
	}
	s.pluginToastIDs = nil
}
