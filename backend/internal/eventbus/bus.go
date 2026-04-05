package eventbus

import (
	"log/slog"
	"sync"
	"time"
)

// Handler is a callback invoked when an event is dispatched.
type Handler func(Event)

// Bus is a simple in-process event bus backed by a buffered Go channel.
type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
	wildcard []Handler // handlers registered for all events
	ch       chan Event
	done     chan struct{}
}

// New creates a Bus with the given channel buffer size.
func New(bufSize int) *Bus {
	return &Bus{
		handlers: make(map[EventType][]Handler),
		ch:       make(chan Event, bufSize),
		done:     make(chan struct{}),
	}
}

// Start launches the dispatch goroutine. Call Stop to shut it down.
func (b *Bus) Start() {
	go b.dispatch()
	slog.Info("event bus started")
}

// Stop signals the dispatch goroutine to exit and waits for it to drain.
func (b *Bus) Stop() {
	close(b.done)
}

// Subscribe registers a handler for a specific event type.
// Must be called before Start (not safe to call concurrently with dispatch).
func (b *Bus) Subscribe(eventType EventType, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// SubscribeAll registers a handler that receives every event.
func (b *Bus) SubscribeAll(handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wildcard = append(b.wildcard, handler)
}

// Publish sends an event to the bus. Non-blocking: if the buffer is full the
// event is dropped and a warning is logged.
func (b *Bus) Publish(eventType EventType, payload any) {
	event := Event{
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	select {
	case b.ch <- event:
	default:
		slog.Warn("event bus: buffer full, event dropped",
			"type", eventType)
	}
}

func (b *Bus) dispatch() {
	for {
		select {
		case <-b.done:
			return
		case event := <-b.ch:
			b.mu.RLock()
			handlers := b.handlers[event.Type]
			wildcards := b.wildcard
			b.mu.RUnlock()

			for _, h := range handlers {
				b.safeCall(h, event)
			}
			for _, h := range wildcards {
				b.safeCall(h, event)
			}
		}
	}
}

func (b *Bus) safeCall(h Handler, event Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("event bus: handler panicked",
				"type", event.Type, "panic", r)
		}
	}()
	h(event)
}
