package session

import (
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
)

// A snapshot waits until Kit has followed every turn the droid started, so a
// queue-started turn is never reported as finished history of an idle session
// before Kit adopts its run.
func TestQueueTurnFeedReportsCaughtUpOnlyAfterTheFollowerDrains(t *testing.T) {
	feed := &queueTurnFeed{}
	follower := make(chan struct{}, 1)
	feed.bind(func() bool { follower <- struct{}{}; return true })
	if caughtUp, _ := feed.caughtUp(); !caughtUp {
		t.Fatal("an idle feed is not caught up")
	}

	feed.started(droids.TurnID("turn_queued"))
	<-follower
	caughtUp, followed := feed.caughtUp()
	if caughtUp {
		t.Fatal("a feed with a started turn reports caught up")
	}
	if turn, ok := feed.next(); !ok || turn != "turn_queued" {
		t.Fatalf("next = %q, %v", turn, ok)
	}
	select {
	case <-followed:
		t.Fatal("the feed signalled while the follower was still following a turn")
	default:
	}
	if _, ok := feed.next(); ok {
		t.Fatal("the feed has another turn")
	}
	select {
	case <-followed:
	case <-time.After(time.Second):
		t.Fatal("the feed did not signal when the follower drained it")
	}
	if caughtUp, _ := feed.caughtUp(); !caughtUp {
		t.Fatal("a drained feed is not caught up")
	}
}
