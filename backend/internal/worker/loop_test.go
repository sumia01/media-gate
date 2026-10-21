package worker_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sumia01/media-gate/internal/worker"
)

type loopSettings struct {
	changes           chan string
	unsubscribed      chan struct{}
	beforeUnsubscribe func()
	mu                sync.Mutex
	intervals         map[string]time.Duration
}

func newLoopSettings() *loopSettings {
	return &loopSettings{
		changes:      make(chan string, 16),
		unsubscribed: make(chan struct{}),
		intervals:    make(map[string]time.Duration),
	}
}

func (s *loopSettings) Subscribe() <-chan string { return s.changes }

func (s *loopSettings) Unsubscribe(ch <-chan string) {
	if ch != s.changes {
		panic("unsubscribing an unknown settings channel")
	}
	if s.beforeUnsubscribe != nil {
		s.beforeUnsubscribe()
	}
	close(s.changes)
	close(s.unsubscribed)
}

func (s *loopSettings) GetDurationWithDefault(key string, fallback time.Duration) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.intervals[key] == 0 {
		return fallback
	}
	return s.intervals[key]
}

func (s *loopSettings) setInterval(key string, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intervals[key] = interval
}

// Wait establishes quiescence before these non-blocking assertions. No real
// sleeps or assumptions about how quickly the worker goroutine is scheduled.
func requireClosed(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	default:
		t.Fatalf("%s has not completed", description)
	}
}

func requireOpen(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s completed too early", description)
	default:
	}
}

func TestLoopStopWaitsForProcessAndUnsubscribe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		settings := newLoopSettings()
		processStarted := make(chan struct{})
		processGate := make(chan struct{})
		unsubscribeStarted := make(chan struct{})
		unsubscribeGate := make(chan struct{})
		releaseProcess := sync.OnceFunc(func() { close(processGate) })
		releaseUnsubscribe := sync.OnceFunc(func() { close(unsubscribeGate) })
		settings.beforeUnsubscribe = func() {
			close(unsubscribeStarted)
			<-unsubscribeGate
		}
		loop := worker.New(worker.Config{
			Name:            "stop-test",
			Settings:        settings,
			DefaultInterval: time.Hour,
			Process: func() {
				close(processStarted)
				<-processGate
			},
		})
		loop.Start()
		stopped := make(chan struct{})
		// Only the launch is guarded by Once, so cleanup never waits on its mutex.
		requestStop := sync.OnceFunc(func() {
			go func() {
				loop.Stop()
				close(stopped)
			}()
		})
		defer func() {
			releaseProcess()
			releaseUnsubscribe()
			requestStop()
			select {
			case <-stopped:
			case <-time.After(5 * time.Second): // Fake-clock failure deadline.
				t.Error("Stop did not return after releasing process and subscription cleanup")
			}
		}()

		loop.RunNow()
		synctest.Wait()
		requireClosed(t, processStarted, "process start")
		requestStop()
		synctest.Wait()
		requireOpen(t, stopped, "Stop while processing")
		requireOpen(t, unsubscribeStarted, "unsubscribe while processing")
		if !loop.Status().Running {
			t.Fatal("worker should remain running until Process returns")
		}

		releaseProcess()
		synctest.Wait()
		requireClosed(t, unsubscribeStarted, "unsubscribe start")
		requireOpen(t, stopped, "Stop while unsubscribing")
		requireOpen(t, settings.unsubscribed, "subscription cleanup")

		releaseUnsubscribe()
		synctest.Wait()
		requireClosed(t, stopped, "Stop")
		requireClosed(t, settings.unsubscribed, "subscription cleanup")
		status := loop.Status()
		if status.Running || status.LastRunAt.IsZero() {
			t.Fatalf("unexpected status after Stop: %+v", status)
		}
	})
}

func TestLoopStopDuringStartupDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		settings := newLoopSettings()
		var calls atomic.Int32
		loop := worker.New(worker.Config{
			Name:            "startup-test",
			Settings:        settings,
			DefaultInterval: time.Hour,
			StartupDelay:    time.Minute,
			Process:         func() { calls.Add(1) },
		})
		loop.Start()
		stop := sync.OnceFunc(loop.Stop)
		defer stop()
		synctest.Wait() // The startup timer has not fired; the worker is blocked.
		stop()
		requireClosed(t, settings.unsubscribed, "subscription cleanup")
		if got := calls.Load(); got != 0 {
			t.Fatalf("Process called %d times during startup shutdown", got)
		}
	})
}

func TestLoopRunNowCoalescesWithoutOverlapping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		settings := newLoopSettings()
		processGate := make(chan struct{})
		var calls, active atomic.Int32
		var overlap atomic.Bool
		loop := worker.New(worker.Config{
			Name:            "trigger-test",
			Settings:        settings,
			DefaultInterval: time.Hour,
			Process: func() {
				if active.Add(1) != 1 {
					overlap.Store(true)
				}
				defer active.Add(-1)
				calls.Add(1)
				<-processGate
			},
		})
		loop.Start()
		defer func() {
			close(processGate)
			loop.Stop()
		}()
		synctest.Wait()
		loop.RunNow()
		synctest.Wait()
		if got := calls.Load(); got != 1 {
			t.Fatalf("initial trigger ran Process %d times, want 1", got)
		}

		triggered := make(chan struct{})
		go func() {
			for range 100 {
				loop.RunNow()
			}
			close(triggered)
		}()
		synctest.Wait()
		requireClosed(t, triggered, "RunNow calls while processing")
		if got := calls.Load(); got != 1 {
			t.Fatalf("pending triggers started extra work: calls = %d", got)
		}

		processGate <- struct{}{}
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("coalesced trigger ran Process %d times, want 2 total", got)
		}
		processGate <- struct{}{}
		synctest.Wait()
		if got := calls.Load(); got != 2 || loop.Status().Running || overlap.Load() {
			t.Fatalf("unexpected trigger result: calls = %d, active = %d, overlap = %v", got, active.Load(), overlap.Load())
		}
	})
}

func TestLoopIntervalSettings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{name: "fallback", want: time.Hour},
		{name: "configured", interval: 2 * time.Hour, want: 2 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				settings := newLoopSettings()
				settings.setInterval("test_interval", tc.interval)
				var calls atomic.Int32
				loop := worker.New(worker.Config{
					Name:            "interval-test",
					Settings:        settings,
					DefaultInterval: time.Hour,
					IntervalKey:     "test_interval",
					Process:         func() { calls.Add(1) },
				})
				loop.Start()
				defer loop.Stop()
				synctest.Wait()
				initial := loop.Status()
				if initial.Interval != tc.want || initial.Running || !initial.LastRunAt.IsZero() {
					t.Fatalf("unexpected initial status: %+v", initial)
				}

				const updated = 30 * time.Minute
				settings.setInterval("test_interval", updated)
				settings.changes <- "unrelated_setting"
				synctest.Wait()
				if got := loop.Status(); got != initial {
					t.Fatalf("unrelated setting changed status: got %+v, want %+v", got, initial)
				}
				settings.changes <- "test_interval"
				synctest.Wait()
				status := loop.Status()
				if status.Interval != updated || !status.NextRunAt.Equal(time.Now().Add(updated)) {
					t.Fatalf("interval notification did not update schedule: %+v", status)
				}

				// Advance the fake clock to the new tick, not the old interval.
				time.Sleep(updated)
				synctest.Wait()
				if got := calls.Load(); got != 1 {
					t.Fatalf("updated ticker ran Process %d times, want 1", got)
				}
				status = loop.Status()
				if status.Running || !status.LastRunAt.Equal(time.Now()) || !status.NextRunAt.Equal(status.LastRunAt.Add(updated)) {
					t.Fatalf("unexpected status after updated tick: %+v", status)
				}
			})
		})
	}
}

func TestLoopStatusAndCallbacksWithConcurrentReaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type stateChange struct {
			name                 string
			running              bool
			lastRunAt, nextRunAt time.Time
			status               worker.Status
		}
		settings := newLoopSettings()
		processGate := make(chan struct{})
		changes := make(chan stateChange, 8)
		var calls atomic.Int32
		var loop *worker.Loop
		loop = worker.New(worker.Config{
			Name:            "status-test",
			Settings:        settings,
			DefaultInterval: time.Hour,
			Process: func() {
				calls.Add(1)
				<-processGate
			},
			OnStateChange: func(name string, running bool, lastRunAt, nextRunAt time.Time) {
				changes <- stateChange{
					name: name, running: running, lastRunAt: lastRunAt,
					nextRunAt: nextRunAt, status: loop.Status(),
				}
			},
		})
		loop.Start()
		defer func() {
			close(processGate)
			loop.Stop()
		}()
		synctest.Wait()

		const readers = 4
		samples := make(chan worker.Status, readers)
		requests := make([]chan struct{}, readers)
		for i := range requests {
			requests[i] = make(chan struct{}, 1)
			go func(request <-chan struct{}) {
				for range request {
					samples <- loop.Status()
				}
			}(requests[i])
			defer close(requests[i])
		}
		requestSamples := func() {
			for _, request := range requests {
				request <- struct{}{}
			}
		}
		checkSamples := func() {
			t.Helper()
			for range readers {
				var status worker.Status
				select {
				case status = <-samples:
				default:
					t.Fatal("concurrent status reader did not finish")
				}
				if status.Name != "status-test" || status.Interval != time.Hour || status.NextRunAt.IsZero() {
					t.Fatalf("inconsistent status snapshot: %+v", status)
				}
				if !status.LastRunAt.IsZero() && !status.NextRunAt.Equal(status.LastRunAt.Add(status.Interval)) {
					t.Fatalf("inconsistent completion timestamps: %+v", status)
				}
			}
		}
		checkCallback := func(want worker.Status) {
			t.Helper()
			var event stateChange
			select {
			case event = <-changes:
			default:
				t.Fatal("missing state-change callback")
			}
			if event.status != want || event.name != want.Name || event.running != want.Running {
				t.Fatalf("callback disagrees with status: event = %+v, want %+v", event, want)
			}
			if !event.lastRunAt.Equal(want.LastRunAt) || !event.nextRunAt.Equal(want.NextRunAt) {
				t.Fatalf("callback timestamps disagree with status: event = %+v, want %+v", event, want)
			}
		}

		for range 4 {
			loop.RunNow()
			requestSamples() // Reads may race with the transition into Process.
			synctest.Wait()
			checkSamples()
			status := loop.Status()
			if !status.Running {
				t.Fatal("status should be running while Process is blocked")
			}
			checkCallback(status)

			time.Sleep(time.Second) // Advance only the fake clock during this run.
			processGate <- struct{}{}
			requestSamples() // Reads may race with the completion transition.
			synctest.Wait()
			checkSamples()
			status = loop.Status()
			if status.Running || !status.LastRunAt.Equal(time.Now()) {
				t.Fatalf("unexpected completion status: %+v", status)
			}
			checkCallback(status)
		}
		if got := calls.Load(); got != 4 {
			t.Fatalf("Process called %d times, want 4", got)
		}
		if len(changes) != 0 {
			t.Fatal("unexpected additional state-change callbacks")
		}
	})
}
