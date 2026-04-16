package clock

import (
	"sync"
	"time"
)

// Fake is a Clock whose timers only fire when Advance is called. This lets
// tests control exactly when polling loops unblock without sleeping.
//
// Typical test pattern:
//
//	fake := clock.NewFake()
//	// inject fake into the system under test, then in a driver goroutine:
//	select {
//	case <-fake.TimerAdded():
//	    fake.Advance(5 * time.Second)
//	case <-done:
//	}
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*fakeTimer
	timerCh chan struct{} // receives when any timer is registered
}

type fakeTimer struct {
	deadline time.Time
	ch       chan time.Time
}

// NewFake creates a Fake clock starting at the Unix epoch.
func NewFake() *Fake {
	return &Fake{
		now:     time.Unix(0, 0),
		timerCh: make(chan struct{}, 1),
	}
}

// After registers a timer and returns a channel that only fires when
// Advance moves the clock past the timer's deadline.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	ch := make(chan time.Time, 1)
	f.timers = append(f.timers, &fakeTimer{
		deadline: f.now.Add(d),
		ch:       ch,
	})
	f.mu.Unlock()

	// Non-blocking: if the channel already has a pending signal, a new one
	// is not needed — Advance fires all due timers at once.
	select {
	case f.timerCh <- struct{}{}:
	default:
	}
	return ch
}

// Advance moves the fake clock forward by d, firing any timers whose
// deadline has been reached.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	var remaining []*fakeTimer
	for _, t := range f.timers {
		if !t.deadline.After(f.now) {
			t.ch <- f.now
		} else {
			remaining = append(remaining, t)
		}
	}
	f.timers = remaining
}

// TimerAdded returns a channel that receives when at least one new timer
// has been registered since the last receive. The channel has capacity 1:
// multiple rapid registrations collapse to one signal because Advance fires
// all due timers at once anyway.
func (f *Fake) TimerAdded() <-chan struct{} {
	return f.timerCh
}
