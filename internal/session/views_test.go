package session

import (
	"errors"
	"testing"
)

func TestViewRevealHubSendsOnlyToWatchersOfTheView(t *testing.T) {
	var hub viewRevealHub
	first, stopFirst, err := hub.watch("view_a")
	if err != nil {
		t.Fatal(err)
	}
	defer stopFirst()
	second, stopSecond, err := hub.watch("view_a")
	if err != nil {
		t.Fatal(err)
	}
	defer stopSecond()
	other, stopOther, err := hub.watch("view_b")
	if err != nil {
		t.Fatal(err)
	}
	defer stopOther()

	if !hub.reveal("view_a", ViewReveal{Line: 40}) {
		t.Fatal("reveal reported no watcher")
	}
	for _, watcher := range []<-chan ViewReveal{first, second} {
		if got := <-watcher; got != (ViewReveal{Line: 40}) {
			t.Fatalf("watcher received %+v, want line 40", got)
		}
	}
	select {
	case got := <-other:
		t.Fatalf("another view's watcher received %+v", got)
	default:
	}
	if hub.reveal("view_c", ViewReveal{Line: 1}) {
		t.Fatal("reveal to an unwatched view reported a watcher")
	}
}

func TestViewRevealHubKeepsOnlyTheLatestUnreadReveal(t *testing.T) {
	var hub viewRevealHub
	reveals, stop, err := hub.watch("view_a")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	hub.reveal("view_a", ViewReveal{Line: 1})
	hub.reveal("view_a", ViewReveal{Annotation: 7})
	if got := <-reveals; got != (ViewReveal{Annotation: 7}) {
		t.Fatalf("watcher received %+v, want annotation 7", got)
	}
	select {
	case got := <-reveals:
		t.Fatalf("watcher kept a replaced reveal %+v", got)
	default:
	}
}

func TestViewRevealHubBoundsWatchersAndFreesThemOnStop(t *testing.T) {
	var hub viewRevealHub
	stops := make([]func(), 0, maxViewWatchers)
	for range maxViewWatchers {
		_, stop, err := hub.watch("view_a")
		if err != nil {
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}
	if _, _, err := hub.watch("view_b"); !errors.Is(err, ErrViewRevealCapacity) {
		t.Fatalf("watch over the limit = %v, want ErrViewRevealCapacity", err)
	}
	stops[0]()
	stops[0]()
	reveals, stop, err := hub.watch("view_b")
	if err != nil {
		t.Fatalf("watch after a stop = %v", err)
	}
	stop()
	if _, open := <-reveals; open {
		t.Fatal("a stopped watch stayed open")
	}
	for _, stop := range stops[1:] {
		stop()
	}
	if hub.count != 0 || len(hub.watchers) != 0 {
		t.Fatalf("stopped watches left count %d and views %v", hub.count, hub.watchers)
	}
}

func TestViewRevealHubCloseEndsWatchesAndRefusesNewOnes(t *testing.T) {
	var hub viewRevealHub
	reveals, stop, err := hub.watch("view_a")
	if err != nil {
		t.Fatal(err)
	}
	hub.close()
	if _, open := <-reveals; open {
		t.Fatal("closing the hub left a watch open")
	}
	stop()
	if hub.reveal("view_a", ViewReveal{Line: 1}) {
		t.Fatal("a closed hub reported a watcher")
	}
	if _, _, err := hub.watch("view_a"); !errors.Is(err, ErrClosed) {
		t.Fatalf("watch after close = %v, want ErrClosed", err)
	}
}
