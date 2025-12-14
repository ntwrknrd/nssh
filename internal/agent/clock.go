package agent

import "time"

// clock provides a testable source of wall-clock time and timers.
// The implementation strips the monotonic component so comparisons behave
// correctly across system sleep or clock adjustments.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) clockTimer
}

type clockTimer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

type realClock struct{}

func (realClock) Now() time.Time {
	return stripMonotonic(time.Now())
}

func (realClock) NewTimer(d time.Duration) clockTimer {
	return &realTimer{t: time.NewTimer(d)}
}

type realTimer struct {
	t *time.Timer
}

func (rt *realTimer) C() <-chan time.Time { return rt.t.C }

func (rt *realTimer) Stop() bool { return rt.t.Stop() }

func (rt *realTimer) Reset(d time.Duration) bool { return rt.t.Reset(d) }
