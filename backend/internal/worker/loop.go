package worker

import (
	"log/slog"
	"time"
)

// SettingsSubscriber provides settings subscription and interval reading.
type SettingsSubscriber interface {
	Subscribe() <-chan string
	GetDurationWithDefault(key string, fallback time.Duration) time.Duration
}

// Config defines a worker loop's parameters.
type Config struct {
	Name            string             // Log prefix (e.g. "download")
	DefaultInterval time.Duration      // Fallback poll interval
	IntervalKey     string             // Settings key for interval
	Settings        SettingsSubscriber // Settings service
	Process         func()             // Work function called on each tick
	StartupDelay    time.Duration      // If >0, sleep then run Process once before entering loop
}

// Loop is a ticker-based worker that runs Process periodically and reacts to
// settings changes for its poll interval.
type Loop struct {
	cfg    Config
	stopCh chan struct{}
}

// New creates a Loop from the given config.
func New(cfg Config) *Loop {
	return &Loop{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Start launches the background goroutine.
func (l *Loop) Start() {
	go l.run()
}

// Stop signals the worker to shut down.
func (l *Loop) Stop() {
	close(l.stopCh)
}

func (l *Loop) run() {
	settingsCh := l.cfg.Settings.Subscribe()
	interval := l.cfg.Settings.GetDurationWithDefault(l.cfg.IntervalKey, l.cfg.DefaultInterval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info(l.cfg.Name+" worker started", "interval", interval)

	if l.cfg.StartupDelay > 0 {
		select {
		case <-l.stopCh:
			return
		case <-time.After(l.cfg.StartupDelay):
			l.cfg.Process()
		}
	}

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.cfg.Process()
		case key := <-settingsCh:
			if key == l.cfg.IntervalKey {
				newInterval := l.cfg.Settings.GetDurationWithDefault(l.cfg.IntervalKey, l.cfg.DefaultInterval)
				if newInterval != interval {
					interval = newInterval
					ticker.Reset(interval)
					slog.Info(l.cfg.Name+" interval updated", "interval", interval)
				}
			}
		}
	}
}
