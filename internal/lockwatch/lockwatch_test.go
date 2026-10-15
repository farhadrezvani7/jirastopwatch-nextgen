package lockwatch

import (
	"context"
	"testing"
	"time"
)

func TestWatchReportsOnlyChanges(t *testing.T) {
	states := []bool{false, false, true, true, true, false}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // safety net only
	defer cancel()
	i := 0
	probeFn := func() (bool, bool) {
		if i >= len(states) { // every scripted state has been seen: stop, no wall-clock guessing
			cancel()
			return states[len(states)-1], true
		}
		s := states[i]
		i++
		return s, true
	}
	var got []bool
	watch(ctx, time.Millisecond, probeFn, func(l bool) { got = append(got, l) })
	if len(got) != 2 || got[0] != true || got[1] != false {
		t.Errorf("events = %v, want [true false]", got)
	}
}

func TestUnsupportedPlatformReturnsImmediately(t *testing.T) {
	done := make(chan struct{})
	go func() {
		watch(context.Background(), time.Hour, func() (bool, bool) { return false, false }, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch blocked on unsupported platform")
	}
}
