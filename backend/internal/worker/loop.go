package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
)

// SettingsSubscriber provides settings subscription and interval reading.
type SettingsSubscriber interface {
	Subscribe() <-chan string
	Unsubscribe(ch <-chan string)
	GetDurationWithDefault(key string, fallback time.Duration) time.Duration
}

// EventPublisher is a function that broadcasts worker state changes.
// The worker package uses this to avoid importing eventbus directly.
type EventPublisher func(name string, running bool, lastRunAt, nextRunAt time.Time)

// Config defines a worker loop's parameters.
type Config struct {
	Name            string             // Log prefix (e.g. "download")
	DefaultInterval time.Duration      // Fallback poll interval
	IntervalKey     string             // Settings key for interval
	Settings        SettingsSubscriber // Settings service
	Process         func()             // Work function called on each tick
	StartupDelay    time.Duration      // If >0, sleep then run Process once before entering loop
	OnStateChange   EventPublisher     // Optional: called on start/finish of each run
}

// Status holds the current state of a worker loop.
type Status struct {
	Name      string
	Running   bool
	LastRunAt time.Time
	NextRunAt time.Time
	Interval  time.Duration
}

// Loop is a ticker-based worker that runs Process periodically and reacts to
// settings changes for its poll interval.
type Loop struct {
	cfg    Config
	stopCh chan struct{}
	doneCh chan struct{} // closed when run() goroutine exits
	runCh  chan struct{} // trigger immediate execution

	mu        sync.RWMutex
	lastRunAt time.Time
	nextRunAt time.Time
	interval  time.Duration
	running   bool
}

// New creates a Loop from the given config.
func New(cfg Config) *Loop {
	return &Loop{
		cfg:    cfg,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		runCh:  make(chan struct{}, 1),
	}
}

// Start launches the background goroutine.
func (l *Loop) Start() {
	go l.run()
}

// Stop signals the worker to shut down and waits for the current Process
// invocation (if any) to complete before returning.
func (l *Loop) Stop() {
	close(l.stopCh)
	<-l.doneCh
}

// RunNow triggers an immediate execution of the worker's process function.
// Non-blocking: if a trigger is already pending, this is a no-op.
func (l *Loop) RunNow() {
	select {
	case l.runCh <- struct{}{}:
	default:
	}
}

// Status returns the current status of the worker loop.
func (l *Loop) Status() Status {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Status{
		Name:      l.cfg.Name,
		Running:   l.running,
		LastRunAt: l.lastRunAt,
		NextRunAt: l.nextRunAt,
		Interval:  l.interval,
	}
}

// process runs the work function wrapped in an OTel span.
// When the global TracerProvider is noop, this adds zero overhead.
func (l *Loop) process() {
	l.mu.Lock()
	l.running = true
	l.mu.Unlock()

	if l.cfg.OnStateChange != nil {
		slog.Debug("worker state change", "worker", l.cfg.Name, "running", true)
		l.cfg.OnStateChange(l.cfg.Name, true, l.lastRunAt, l.nextRunAt)
	}

	_, span := otel.Tracer("worker").Start(context.Background(), "worker/"+l.cfg.Name)
	defer span.End()
	l.cfg.Process()

	l.mu.Lock()
	l.running = false
	l.lastRunAt = time.Now()
	l.nextRunAt = l.lastRunAt.Add(l.interval)
	lastRun := l.lastRunAt
	nextRun := l.nextRunAt
	l.mu.Unlock()

	if l.cfg.OnStateChange != nil {
		slog.Debug("worker state change", "worker", l.cfg.Name, "running", false)
		l.cfg.OnStateChange(l.cfg.Name, false, lastRun, nextRun)
	}
}

func (l *Loop) run() {
	defer close(l.doneCh)

	settingsCh := l.cfg.Settings.Subscribe()
	defer l.cfg.Settings.Unsubscribe(settingsCh)

	interval := l.cfg.Settings.GetDurationWithDefault(l.cfg.IntervalKey, l.cfg.DefaultInterval)

	l.mu.Lock()
	l.interval = interval
	l.mu.Unlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info(l.cfg.Name+" worker started", "interval", interval)

	if l.cfg.StartupDelay > 0 {
		select {
		case <-l.stopCh:
			return
		case <-l.runCh:
			l.process()
			ticker.Reset(interval)
		case <-time.After(l.cfg.StartupDelay):
			l.process()
		}
	}

	// Set initial nextRunAt
	l.mu.Lock()
	if l.nextRunAt.IsZero() {
		l.nextRunAt = time.Now().Add(interval)
	}
	l.mu.Unlock()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.process()
		case <-l.runCh:
			l.process()
			ticker.Reset(interval)
		case key := <-settingsCh:
			if key == l.cfg.IntervalKey {
				newInterval := l.cfg.Settings.GetDurationWithDefault(l.cfg.IntervalKey, l.cfg.DefaultInterval)
				if newInterval != interval {
					interval = newInterval
					l.mu.Lock()
					l.interval = newInterval
					l.nextRunAt = time.Now().Add(newInterval)
					l.mu.Unlock()
					ticker.Reset(interval)
					slog.Info(l.cfg.Name+" interval updated", "interval", interval)
				}
			}
		}
	}
}
