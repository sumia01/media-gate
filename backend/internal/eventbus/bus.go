package eventbus

import (
	"log/slog"
	"sync"
	"time"
)

// Handler is a callback invoked when an event is dispatched.
type Handler func(Event)

// subscriber is a single registered handler together with its own ordered work
// queue and goroutine. Giving every subscriber its own goroutine + queue means:
//   - The dispatch loop never runs handler code inline, so a slow handler (e.g.
//     subtitle auto-search or a Discord webhook doing blocking network I/O)
//     cannot stall dispatch, keep b.ch from draining, or make Publish drop
//     events.
//   - Different subscribers run concurrently and independently — a slow
//     subscriber only backs up its own queue, never the fast ones (e.g. the SSE
//     broadcaster).
//   - Each subscriber still observes its events in publish order (its queue is
//     processed serially by one goroutine).
//
// The number of goroutines is bounded by the number of subscriptions, which is
// fixed at startup — there is no per-event goroutine growth.
type subscriber struct {
	handler Handler
	queue   chan Event
}

// Bus is a simple in-process event bus backed by a buffered Go channel.
type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]*subscriber
	wildcard []*subscriber // subscribers registered for all events
	subs     []*subscriber // every subscriber, used to close queues on Stop
	buf      int           // buffer size for b.ch and each subscriber queue
	ch       chan Event
	done     chan struct{}
	wg       sync.WaitGroup // tracks the dispatch goroutine + subscriber goroutines
	stopOnce sync.Once
}

// New creates a Bus with the given channel buffer size. The same buffer size is
// used for each subscriber's queue, so a burst of events for one subscriber can
// be absorbed without blocking the dispatch loop.
func New(bufSize int) *Bus {
	return &Bus{
		handlers: make(map[EventType][]*subscriber),
		buf:      bufSize,
		ch:       make(chan Event, bufSize),
		done:     make(chan struct{}),
	}
}

// Start launches the dispatch goroutine. Call Stop to shut it down.
func (b *Bus) Start() {
	b.wg.Add(1)
	go b.dispatch()
	slog.Info("event bus started")
}

// Stop signals the dispatch goroutine to exit and waits for it — and every
// subscriber goroutine — to finish any in-flight and already-queued handler
// invocations before returning, so graceful shutdown drains cleanly. Safe to
// call more than once.
func (b *Bus) Stop() {
	b.stopOnce.Do(func() {
		close(b.done)
	})
	b.wg.Wait()
}

// Subscribe registers a handler for a specific event type. Safe to call before
// or after Start; each subscriber gets its own goroutine immediately.
func (b *Bus) Subscribe(eventType EventType, handler Handler) {
	s := b.addSubscriber(handler)
	b.mu.Lock()
	b.handlers[eventType] = append(b.handlers[eventType], s)
	b.mu.Unlock()
}

// SubscribeAll registers a handler that receives every event.
func (b *Bus) SubscribeAll(handler Handler) {
	s := b.addSubscriber(handler)
	b.mu.Lock()
	b.wildcard = append(b.wildcard, s)
	b.mu.Unlock()
}

// addSubscriber creates a subscriber, registers it for shutdown tracking, and
// starts its goroutine.
func (b *Bus) addSubscriber(handler Handler) *subscriber {
	s := &subscriber{
		handler: handler,
		queue:   make(chan Event, b.buf),
	}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.wg.Add(1)
	b.mu.Unlock()

	go b.runSubscriber(s)
	return s
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

// dispatch reads events off b.ch and routes each to the relevant subscriber
// queues. It performs no handler work itself, so it never blocks on a slow
// handler. On shutdown it drains any buffered events, then closes every
// subscriber queue so their goroutines finish; it is the sole sender on those
// queues, so closing here is race-free.
func (b *Bus) dispatch() {
	defer b.wg.Done()
	for {
		select {
		case <-b.done:
			b.drain()
			b.closeSubscribers()
			return
		case event := <-b.ch:
			b.route(event)
		}
	}
}

// drain routes every event still buffered in b.ch. Only called during shutdown
// after b.done is closed.
func (b *Bus) drain() {
	for {
		select {
		case event := <-b.ch:
			b.route(event)
		default:
			return
		}
	}
}

// route hands the event to each subscriber's queue. The subscriber slices are
// read under RLock; the (potentially slow) handler calls happen later on the
// subscriber goroutines, never here.
func (b *Bus) route(event Event) {
	b.mu.RLock()
	subs := b.handlers[event.Type]
	wild := b.wildcard
	b.mu.RUnlock()

	for _, s := range subs {
		b.deliver(s, event)
	}
	for _, s := range wild {
		b.deliver(s, event)
	}
}

// deliver enqueues the event for one subscriber. The send is non-blocking so a
// subscriber with a full queue (an overwhelmed slow handler) cannot stall the
// dispatch loop or block delivery to the other, faster subscribers. Only that
// subscriber loses the event, and it is logged — matching Publish's
// drop-when-full policy.
func (b *Bus) deliver(s *subscriber, event Event) {
	select {
	case s.queue <- event:
	default:
		slog.Warn("event bus: subscriber queue full, event dropped",
			"type", event.Type)
	}
}

// runSubscriber processes one subscriber's queue serially (preserving event
// order) until the queue is closed on shutdown. Each invocation is wrapped in
// safeCall so a panicking handler cannot take down the goroutine.
func (b *Bus) runSubscriber(s *subscriber) {
	defer b.wg.Done()
	for event := range s.queue {
		b.safeCall(s.handler, event)
	}
}

// closeSubscribers closes every subscriber's queue. Called from dispatch on
// shutdown, after the last route/drain, so no send races the close.
func (b *Bus) closeSubscribers() {
	b.mu.Lock()
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()

	for _, s := range subs {
		close(s.queue)
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
