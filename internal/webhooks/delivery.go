package webhooks

import (
	"strconv"
	"time"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	deliveryIDPrefix  = "dlv_"
	maxDeliveryEvents = 100
)

func deliveryID(d store.Delivery) string {
	return deliveryIDPrefix + strconv.FormatInt(d.ID, 10)
}

func placeFor(w store.Webhook) func(store.Event, *store.Delivery) store.Placement {
	return func(e store.Event, previous *store.Delivery) store.Placement {
		return place(w, e, previous)
	}
}

func place(w store.Webhook, e store.Event, previous *store.Delivery) store.Placement {
	if e.Kind != store.EventMessageReceived {
		return store.Placement{DueAt: e.CreatedAt}
	}
	if joinable(previous) {
		return store.Placement{Join: true, DueAt: dueAt(w, previous.OpenedAt, e.CreatedAt)}
	}
	return store.Placement{DueAt: dueAt(w, e.CreatedAt, e.CreatedAt)}
}

func joinable(d *store.Delivery) bool {
	return d != nil && d.State == store.DeliveryOpen && d.Attempts == 0 && d.EventCount < maxDeliveryEvents
}

func dueAt(w store.Webhook, openedAt, lastEventAt time.Time) time.Time {
	quietEnd, maxWaitEnd := lastEventAt.Add(w.Quiet), openedAt.Add(w.MaxWait)
	if quietEnd.Before(maxWaitEnd) {
		return quietEnd
	}
	return maxWaitEnd
}

func (d *Dispatcher) outcome(a attempt, result Result, at time.Time) store.AttemptOutcome {
	switch {
	case result.Accepted():
		return settled(store.DeliveryDelivered, result, at)
	case result.Gone():
		return disabling(settled(store.DeliveryFailed, result, at))
	case !dropsDuplicates(a.webhook) && result.Unanswered():
		return settled(store.DeliveryFailed, result, at)
	case result.Retryable() && retriesLeft(a.delivery.Attempts):
		return retrying(result, at.Add(d.retryDelay(a.delivery.Attempts, result, at)))
	}
	return settled(store.DeliveryFailed, result, at)
}

func settled(state string, result Result, at time.Time) store.AttemptOutcome {
	return store.AttemptOutcome{State: state, Status: result.Status, Error: errorText(result), SettledAt: at}
}

func disabling(next store.AttemptOutcome) store.AttemptOutcome {
	next.DisablesWebhook = true
	return next
}

func retrying(result Result, dueAt time.Time) store.AttemptOutcome {
	return store.AttemptOutcome{State: store.DeliveryRetrying, DueAt: dueAt, Status: result.Status, Error: errorText(result)}
}

func errorText(result Result) string {
	if result.Err == nil {
		return ""
	}
	return result.Err.Error()
}
