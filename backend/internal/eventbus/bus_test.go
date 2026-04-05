package eventbus

import (
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	bus := New(16)
	bus.Start()
	defer bus.Stop()

	var received Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(DownloadCreated, func(e Event) {
		received = e
		wg.Done()
	})

	bus.Publish(DownloadCreated, DownloadPayload{
		DownloadID:  1,
		MediaItemID: 10,
		Title:       "Test Movie",
	})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	if received.Type != DownloadCreated {
		t.Errorf("expected type %s, got %s", DownloadCreated, received.Type)
	}
	payload, ok := received.Payload.(DownloadPayload)
	if !ok {
		t.Fatal("payload is not DownloadPayload")
	}
	if payload.DownloadID != 1 || payload.Title != "Test Movie" {
		t.Errorf("unexpected payload: %+v", payload)
	}
}

func TestSubscribeAll(t *testing.T) {
	bus := New(16)
	bus.Start()
	defer bus.Stop()

	var mu sync.Mutex
	var events []Event
	var wg sync.WaitGroup
	wg.Add(2)

	bus.SubscribeAll(func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		wg.Done()
	})

	bus.Publish(DownloadCreated, DownloadPayload{DownloadID: 1})
	bus.Publish(ImportCompleted, ImportPayload{DownloadID: 2})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != DownloadCreated {
		t.Errorf("first event type: got %s, want %s", events[0].Type, DownloadCreated)
	}
	if events[1].Type != ImportCompleted {
		t.Errorf("second event type: got %s, want %s", events[1].Type, ImportCompleted)
	}
}

func TestMultipleHandlers(t *testing.T) {
	bus := New(16)
	bus.Start()
	defer bus.Stop()

	var wg sync.WaitGroup
	wg.Add(2)

	count := 0
	var mu sync.Mutex

	for range 2 {
		bus.Subscribe(DownloadCompleted, func(e Event) {
			mu.Lock()
			count++
			mu.Unlock()
			wg.Done()
		})
	}

	bus.Publish(DownloadCompleted, DownloadPayload{DownloadID: 1})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	mu.Lock()
	defer mu.Unlock()
	if count != 2 {
		t.Errorf("expected 2 handler calls, got %d", count)
	}
}

func TestHandlerPanicRecovery(t *testing.T) {
	bus := New(16)
	bus.Start()
	defer bus.Stop()

	var wg sync.WaitGroup
	wg.Add(1)

	// First handler panics
	bus.Subscribe(DownloadCreated, func(e Event) {
		panic("boom")
	})

	// Second handler should still run
	bus.Subscribe(DownloadCreated, func(e Event) {
		wg.Done()
	})

	bus.Publish(DownloadCreated, DownloadPayload{DownloadID: 1})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — second handler did not run after panic")
	}
}

func TestNoSubscribersDoesNotBlock(t *testing.T) {
	bus := New(16)
	bus.Start()
	defer bus.Stop()

	// Should not block or panic
	bus.Publish(DownloadCreated, DownloadPayload{DownloadID: 1})
	time.Sleep(50 * time.Millisecond)
}
