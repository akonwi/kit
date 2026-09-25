package session

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestPluginNotificationHubHasNoOfflineOrReconnectReplay(t *testing.T) {
	var hub pluginNotificationHub
	defer hub.close()
	if err := hub.publish(t.Context(), PluginToast{Title: "offline"}); err != nil {
		t.Fatal(err)
	}
	first, closeFirst, err := hub.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 0 {
		t.Fatal("offline notification replayed")
	}
	second, closeSecond, err := hub.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer closeSecond()
	expected := PluginToast{PluginID: "demo", Instance: "owner:1", Title: "live", Variant: "info"}
	if err := hub.publish(t.Context(), expected); err != nil {
		t.Fatal(err)
	}
	for _, subscriber := range []chan PluginToast{first, second} {
		if got := <-subscriber; got != expected {
			t.Fatalf("fanout = %#v", got)
		}
	}
	closeFirst()
	closeFirst()
	if err := hub.publish(t.Context(), PluginToast{Title: "detached"}); err != nil {
		t.Fatal(err)
	}
	reconnect, closeReconnect, err := hub.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer closeReconnect()
	if len(reconnect) != 0 {
		t.Fatal("reconnected subscriber received old toast")
	}
}

func TestPluginNotificationHubBoundsSlowClientsAndClosesSafely(t *testing.T) {
	var hub pluginNotificationHub
	var subscriptions []chan PluginToast
	var closers []func()
	for range maxPluginToastSubscribers {
		stream, closeStream, err := hub.subscribe()
		if err != nil {
			t.Fatal(err)
		}
		subscriptions = append(subscriptions, stream)
		closers = append(closers, closeStream)
	}
	if _, _, err := hub.subscribe(); !errors.Is(err, ErrPluginNotificationCapacity) {
		t.Fatalf("subscriber limit = %v", err)
	}
	for range pluginToastQueueSize * 4 {
		if err := hub.publish(t.Context(), PluginToast{Title: "Notice"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, stream := range subscriptions {
		if len(stream) != pluginToastQueueSize {
			t.Fatalf("queue depth = %d", len(stream))
		}
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = hub.publish(context.Background(), PluginToast{Title: "Concurrent"})
		}
	}()
	go func() {
		defer wg.Done()
		for _, closeStream := range closers {
			closeStream()
		}
	}()
	go func() { defer wg.Done(); hub.close() }()
	wg.Wait()
	if _, _, err := hub.subscribe(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed subscription = %v", err)
	}
	for _, stream := range subscriptions {
		for range stream {
		}
	}
}
