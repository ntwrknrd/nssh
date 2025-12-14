package agent

import (
	"sync"
	"time"
)

// fakeClock is a controllable clock for deterministic tests. It is safe for
// concurrent use by a single test driving time forward while the agent runs.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*fakeTimer]struct{}
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{
		now:    stripMonotonic(start),
		timers: make(map[*fakeTimer]struct{}),
	}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) clockTimer {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := &fakeTimer{
		clock:    c,
		ch:       make(chan time.Time, 1),
		deadline: c.now.Add(d),
	}
	c.timers[t] = struct{}{}
	return t
}

// Advance moves the clock forward and fires any timers that have reached
// their deadlines. Timers fire at most once; Reset can schedule them again.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	timers := make([]*fakeTimer, 0, len(c.timers))
	for t := range c.timers {
		timers = append(timers, t)
	}
	c.mu.Unlock()

	for _, t := range timers {
		t.fireIfDue()
	}
}

type fakeTimer struct {
	clock    *fakeClock
	ch       chan time.Time
	deadline time.Time
	fired    bool
	stopped  bool

	mu sync.Mutex
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return false
	}
	active := !t.fired
	t.stopped = true
	return active
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	active := !t.stopped && !t.fired
	// Drain any pending tick to mirror time.Timer semantics.
	select {
	case <-t.ch:
	default:
	}

	fc := t.clock
	fc.mu.Lock()
	defer fc.mu.Unlock()

	t.deadline = fc.now.Add(d)
	t.fired = false
	t.stopped = false
	return active
}

func (t *fakeTimer) fireIfDue() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped || t.fired {
		return
	}

	fc := t.clock
	fc.mu.Lock()
	now := fc.now
	fc.mu.Unlock()

	if t.deadline.After(now) {
		return
	}

	select {
	case t.ch <- now:
	default:
	}
	t.fired = true
}
