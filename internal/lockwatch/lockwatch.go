// Package lockwatch reports when the user's session is locked or unlocked, so
// running timers can be paused while the user is away (like the original
// StopWatch's "pause on session lock" setting).
package lockwatch

import (
	"context"
	"time"
)

// Watch polls the OS and calls onChange whenever the lock state flips. It
// returns immediately if the platform can't tell.
func Watch(ctx context.Context, interval time.Duration, onChange func(locked bool)) {
	watch(ctx, interval, probe, onChange)
}

func watch(ctx context.Context, interval time.Duration, probe func() (locked, ok bool), onChange func(bool)) {
	last, ok := probe()
	if !ok {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cur, ok := probe()
			if ok && cur != last {
				last = cur
				onChange(cur)
			}
		}
	}
}
