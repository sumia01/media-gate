package worker

import (
	"sync"
	"time"
)

// Registry holds references to all worker loops that should be exposed via the API.
type Registry struct {
	mu        sync.RWMutex
	workers   []*Loop
	publisher EventPublisher
}

// NewRegistry creates a worker registry with an optional state-change publisher.
func NewRegistry(publisher EventPublisher) *Registry {
	return &Registry{publisher: publisher}
}

// Register adds a worker loop to the registry and injects the event publisher.
func (r *Registry) Register(l *Loop) {
	if r.publisher != nil {
		l.cfg.OnStateChange = r.publisher
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workers = append(r.workers, l)
}

// All returns the status of all registered workers.
func (r *Registry) All() []Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	statuses := make([]Status, 0, len(r.workers))
	for _, w := range r.workers {
		statuses = append(statuses, w.Status())
	}
	return statuses
}

// RunByName triggers an immediate run of the worker with the given name.
// Returns false if the worker was not found.
func (r *Registry) RunByName(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, w := range r.workers {
		if w.cfg.Name == name {
			w.RunNow()
			return true
		}
	}
	return false
}

// MakePublisher creates an EventPublisher that publishes to the given bus-like function.
// busPublish should wrap eventbus.Bus.Publish (passed as a func to avoid import cycle).
func MakePublisher(busPublish func(eventType string, payload any)) EventPublisher {
	return func(name string, running bool, lastRunAt, nextRunAt time.Time) {
		var evType string
		if running {
			evType = "worker.started"
		} else {
			evType = "worker.finished"
		}
		payload := map[string]any{
			"name":    name,
			"running": running,
		}
		if !lastRunAt.IsZero() {
			payload["lastRunAt"] = lastRunAt.Format(time.RFC3339)
		}
		if !nextRunAt.IsZero() {
			payload["nextRunAt"] = nextRunAt.Format(time.RFC3339)
		}
		busPublish(evType, payload)
	}
}
