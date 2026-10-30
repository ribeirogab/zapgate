package webhooks

import (
	"context"
	"strconv"
	"time"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	deliveryIDPrefix  = "dlv_"
	maxDeliveryEvents = 100
)

type attempt struct {
	webhook  store.Webhook
	account  store.Account
	delivery store.Delivery
	at       time.Time
}

func deliveryID(d store.Delivery) string {
	return deliveryIDPrefix + strconv.FormatInt(d.ID, 10)
}

func placer(w store.Webhook) func(store.Event, *store.Delivery) store.Placement {
	return func(e store.Event, newest *store.Delivery) store.Placement {
		return place(w, e, newest)
	}
}

func place(w store.Webhook, e store.Event, newest *store.Delivery) store.Placement {
	if e.Kind != store.EventMessageReceived {
		return store.Placement{DueAt: e.CreatedAt}
	}
	if joinable(newest) {
		return store.Placement{Join: true, DueAt: dueAt(w, newest.OpenedAt, e.CreatedAt)}
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

func attemptResult(result Result, next store.AttemptOutcome) string {
	if result.Blocked() {
		return audit.ResultBlocked
	}
	return next.State
}

func (d *Dispatcher) send(ctx context.Context, delivery store.Delivery) {
	a, err := d.prepare(ctx, delivery)
	if err != nil {
		d.report(ctx, err)
		return
	}
	post, err := d.post(ctx, a)
	if err != nil {
		d.report(ctx, err)
		return
	}
	result := d.deps.Sender.Send(ctx, post)
	if ctx.Err() != nil {
		return
	}
	d.settle(ctx, a, result)
}

func (d *Dispatcher) prepare(ctx context.Context, delivery store.Delivery) (attempt, error) {
	webhook, err := d.deps.Store.Webhook(ctx, delivery.WebhookID)
	if err != nil {
		return attempt{}, err
	}
	account, err := d.deps.Store.Account(ctx, delivery.AccountID)
	return attempt{webhook: webhook, account: account, delivery: delivery, at: d.deps.Clock.Now()}, err
}

func (d *Dispatcher) post(ctx context.Context, a attempt) (Post, error) {
	body, err := d.body(ctx, a)
	if err != nil {
		return Post{}, err
	}
	encoded, err := encode(body)
	return Post{
		URL:       a.webhook.URL,
		ID:        deliveryID(a.delivery),
		Timestamp: a.at,
		Body:      encoded,
		Secrets:   SigningSecrets(a.webhook, a.at),
		Headers:   a.webhook.Headers,
	}, err
}

func (d *Dispatcher) body(ctx context.Context, a attempt) (Body, error) {
	if a.delivery.ChatJID == "" {
		return d.stateBody(ctx, a)
	}
	data, err := d.receivedData(ctx, a)
	if err != nil {
		return Body{}, err
	}
	return receivedBody(a, data)
}

func (d *Dispatcher) settle(ctx context.Context, a attempt, result Result) {
	next := d.outcome(a, result, d.deps.Clock.Now())
	d.report(ctx, d.deps.Store.RecordAttempt(ctx, a.delivery.ID, next))
	d.deps.Audit.WebhookDelivery(ctx, audit.WebhookDelivery{
		Webhook:  a.webhook.Name,
		Account:  a.account.Name,
		Delivery: deliveryID(a.delivery),
		Events:   a.delivery.EventCount,
		Attempt:  a.delivery.Attempts,
		Status:   result.Status,
		Result:   attemptResult(result, next),
	})
}
