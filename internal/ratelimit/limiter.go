package ratelimit

import (
	"context"
	"time"
)

type Limiter struct {
	tokens chan struct{}
	done   chan struct{}
}

func New(rps int) *Limiter {
	if rps <= 0 {
		rps = 1
	}
	l := &Limiter{
		tokens: make(chan struct{}, rps),
		done:   make(chan struct{}),
	}
	// Pre-fill the bucket
	for i := 0; i < rps; i++ {
		l.tokens <- struct{}{}
	}
	// Refill goroutine
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(rps))
		defer ticker.Stop()
		for {
			select {
			case <-l.done:
				return
			case <-ticker.C:
				select {
				case l.tokens <- struct{}{}:
				default:
				}
			}
		}
	}()
	return l
}

func (l *Limiter) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.tokens:
		return nil
	}
}

func (l *Limiter) Stop() {
	close(l.done)
}
