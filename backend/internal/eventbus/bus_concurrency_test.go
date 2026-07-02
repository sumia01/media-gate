package eventbus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSlowHandlerDoesNotStallDispatch verifies that a handler blocked on
// (simulated) slow I/O does not stall the dispatch loop: a fast handler on a
// different event type must still run promptly. With the old serial dispatch
// this would deadlock/time out because the fast handler waited behind the slow
// one on the single dispatch goroutine.
func TestSlowHandlerDoesNotStallDispatch(t *testing.T) {
	bus := New(16)

	release := make(chan struct{})
	bus.Subscribe(DownloadCreated, func(e Event) {
		<-release // block as if doing slow network I/O
	})

	fastRan := make(chan struct{}, 1)
	bus.Subscribe(ImportCompleted, func(e Event) {
		fastRan <- struct{}{}
	})

	bus.Start()
	defer func() { close(release); bus.Stop() }()

	// Kick off the slow handler, then publish a fast event of another type.
	bus.Publish(DownloadCreated, DownloadPayload{DownloadID: 1})
	bus.Publish(ImportCompleted, ImportPayload{DownloadID: 2})

	select {
	case <-fastRan:
	case <-time.After(2 * time.Second):
		t.Fatal("fast handler did not run while slow handler was blocked — dispatch stalled")
	}
}

// TestSlowHandlerDoesNotStallPublish verifies that while a handler is blocked,
// the dispatch loop keeps draining b.ch into the worker pool, so a burst of
// events for a second, fast event type is still delivered. The buffer is sized
// larger than the burst so the bounded, non-blocking Publish is never the
// limiting factor — the only thing that could prevent delivery is a stalled
// dispatch loop. With the old serial dispatch, the blocked handler would pin
// the single dispatch goroutine and none of the fast events would ever run.
func TestSlowHandlerDoesNotStallPublish(t *testing.T) {
	const nFast = 50
	bus := New(256)

	release := make(chan struct{})
	bus.Subscribe(DownloadCreated, func(e Event) {
		<-release // stay blocked, occupying a worker
	})

	var got atomic.Int64
	var wg sync.WaitGroup
	wg.Add(nFast)
	bus.Subscribe(ImportCompleted, func(e Event) {
		got.Add(1)
		wg.Done()
	})

	bus.Start()
	defer func() { close(release); bus.Stop() }()

	// Occupy one worker with the slow handler.
	bus.Publish(DownloadCreated, DownloadPayload{DownloadID: 1})
	// Now flood the bus with fast events; none should be dropped.
	for i := range nFast {
		bus.Publish(ImportCompleted, ImportPayload{DownloadID: uint(i)})
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("only %d/%d fast events delivered — dispatch stalled or events dropped", got.Load(), nFast)
	}
}

// TestHandlersRunConcurrently verifies that multiple handlers for the SAME
// event run concurrently (each subscriber has its own goroutine) rather than
// serially. Each handler signals it has started, then blocks; if execution
// were serial the second handler would never start and the test would time out.
func TestHandlersRunConcurrently(t *testing.T) {
	const n = 4
	bus := New(16)

	var started sync.WaitGroup
	started.Add(n)
	release := make(chan struct{})
	for range n {
		bus.Subscribe(DownloadCompleted, func(e Event) {
			started.Done()
			<-release
		})
	}

	bus.Start()
	defer func() { close(release); bus.Stop() }()

	bus.Publish(DownloadCompleted, DownloadPayload{DownloadID: 1})

	done := make(chan struct{})
	go func() { started.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handlers did not run concurrently — pool executed them serially")
	}
}

// TestStopDrainsInFlightHandlers verifies that Stop blocks until an in-flight
// handler finishes, i.e. it drains rather than returning immediately.
func TestStopDrainsInFlightHandlers(t *testing.T) {
	bus := New(16)

	var completed atomic.Bool
	started := make(chan struct{})
	proceed := make(chan struct{})
	bus.Subscribe(DownloadCreated, func(e Event) {
		close(started)
		<-proceed
		completed.Store(true)
	})

	bus.Start()

	bus.Publish(DownloadCreated, DownloadPayload{DownloadID: 1})

	// Wait until the handler is actually in-flight on a worker.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	// Release the handler slightly after we call Stop, so Stop must wait.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(proceed)
	}()

	bus.Stop() // must block until the in-flight handler returns

	if !completed.Load() {
		t.Fatal("Stop returned before in-flight handler completed — did not drain")
	}
}

// TestStopIsIdempotent verifies Stop can be called more than once without
// panicking (double close) and still returns.
func TestStopIsIdempotent(t *testing.T) {
	bus := New(16)
	bus.Start()

	bus.Stop()
	bus.Stop() // must not panic on double close
}
