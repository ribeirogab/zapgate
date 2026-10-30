package apptest

import "sync"

const NoJitter = 0.5

type Random struct {
	mu    sync.Mutex
	value float64
}

func newRandom() *Random {
	return &Random{value: NoJitter}
}

func (r *Random) Set(value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value = value
}

func (r *Random) Float64() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value
}
