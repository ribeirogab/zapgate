package webhooks

import "context"

type postSlots chan struct{}

func newPostSlots() postSlots {
	return make(postSlots, MaxConcurrent)
}

func (s postSlots) tryTake() bool {
	select {
	case s <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s postSlots) take(ctx context.Context) error {
	select {
	case s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s postSlots) release() {
	<-s
}

func (s postSlots) free() int {
	return cap(s) - len(s)
}
