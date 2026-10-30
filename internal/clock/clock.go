package clock

import (
	"slices"
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	At(t time.Time) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (systemClock) At(t time.Time) <-chan time.Time { return time.After(time.Until(t)) }

func System() Clock { return systemClock{} }

type Manual struct {
	mu     sync.Mutex
	now    time.Time
	timers []manualTimer
}

type manualTimer struct {
	at   time.Time
	fire chan time.Time
}

func NewManual(t time.Time) *Manual { return &Manual{now: t} }

func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *Manual) After(d time.Duration) <-chan time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.timerAt(m.now.Add(d))
}

func (m *Manual) At(t time.Time) <-chan time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.timerAt(t)
}

func (m *Manual) timerAt(t time.Time) <-chan time.Time {
	timer := manualTimer{at: t, fire: make(chan time.Time, 1)}
	m.timers = append(m.timers, timer)
	m.fireDue()
	return timer.fire
}

func (m *Manual) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
	m.fireDue()
}

func (m *Manual) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = t
	m.fireDue()
}

func (m *Manual) fireDue() {
	m.timers = slices.DeleteFunc(m.timers, func(timer manualTimer) bool {
		if timer.at.After(m.now) {
			return false
		}
		timer.fire <- m.now
		return true
	})
}
