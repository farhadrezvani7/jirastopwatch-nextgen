package lockwatch

import (
	"context"
	"testing"
	"time"
)

func TestWatchReportsOnlyChanges(t *testing.T) {
	states := []bool{false, false, true, true, true, false}
	i := 0
	probeFn := func() (bool, bool) {
		s := states[min(i, len(states)-1)]
		i++
		return s, true
	}
	var got []bool
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	watch(ctx, 10*time.Millisecond, probeFn, func(l bool) { got = append(got, l) })
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
