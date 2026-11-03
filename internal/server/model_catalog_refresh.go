package server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/akonwi/kit/droids"
)

// modelCatalogRefreshPolicy schedules the daemon's background model-catalog
// refreshes.
type modelCatalogRefreshPolicy struct {
	// Timeout bounds one refresh, so an unreachable catalog cannot hold a
	// connection open.
	Timeout time.Duration
	// Interval is the wall-clock age at which a successfully refreshed catalog
	// is refreshed again.
	Interval time.Duration
	// Check is how often the daemon compares the catalog's age with Interval,
	// and how soon it retries a refresh that failed.
	Check time.Duration
}

// defaultModelCatalogRefresh refreshes at startup and then daily. It checks
// hourly against wall-clock time, so a computer that sleeps still refreshes
// about once a day, and a daemon that started offline retries within an hour.
var defaultModelCatalogRefresh = modelCatalogRefreshPolicy{
	Timeout:  30 * time.Second,
	Interval: 24 * time.Hour,
	Check:    time.Hour,
}

// startModelCatalogRefresh refreshes providers' model catalog in the
// background immediately and then as policy schedules. A failed refresh
// leaves the cached or embedded catalog in place and is logged. The returned
// stop cancels a refresh in flight, ends the schedule, and waits for its
// goroutine to return; it is safe to call more than once.
func startModelCatalogRefresh(ctx context.Context, providers droids.Providers, policy modelCatalogRefreshPolicy, logger *slog.Logger) (stop func()) {
	stopping, stopRefresh := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(policy.Check)
		defer ticker.Stop()
		var refreshed time.Time
		for {
			// Round(0) drops the monotonic reading, so the age is measured in
			// wall-clock time and includes time the computer slept.
			if refreshed.IsZero() || time.Now().Round(0).Sub(refreshed) >= policy.Interval {
				if refreshModelCatalog(stopping, providers, policy.Timeout, logger) {
					refreshed = time.Now().Round(0)
				}
			}
			select {
			case <-stopping.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		stopRefresh()
		<-done
	}
}

// refreshModelCatalog runs one bounded refresh and reports whether it
// succeeded.
func refreshModelCatalog(stopping context.Context, providers droids.Providers, timeout time.Duration, logger *slog.Logger) bool {
	refreshContext, cancel := context.WithTimeout(stopping, timeout)
	defer cancel()
	started := time.Now()
	err := providers.RefreshModels(refreshContext)
	switch {
	case err == nil:
		logger.Info("model catalog refreshed", "models", len(providers.Models()), "duration", time.Since(started).Round(time.Millisecond))
		return true
	case stopping.Err() != nil:
		// The daemon is stopping; the refresh is no longer wanted.
	case errors.Is(refreshContext.Err(), context.DeadlineExceeded):
		logger.Warn("model catalog refresh timed out; keeping the current catalog", "timeout", timeout)
	default:
		logger.Warn("model catalog refresh failed; keeping the current catalog", "error", err)
	}
	return false
}
