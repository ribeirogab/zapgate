package webhooks

import (
	"context"
	"strconv"
	"time"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

const deliveryIDPrefix = "dlv_"

type attempt struct {
	webhook  store.Webhook
	account  store.Account
	delivery store.Delivery
	at       time.Time
}

func deliveryID(d store.Delivery) string {
	return deliveryIDPrefix + strconv.FormatInt(d.ID, 10)
}

func place(e store.Event, _ *store.Delivery) store.Placement {
	return store.Placement{DueAt: e.CreatedAt}
}

func outcome(result Result, at time.Time) store.AttemptOutcome {
	next := store.AttemptOutcome{State: store.DeliveryFailed, Status: result.Status, SettledAt: at}
	if result.Accepted() {
		next.State = store.DeliveryDelivered
	}
	if result.Err != nil {
		next.Error = result.Err.Error()
	}
	return next
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
	data, err := d.receivedData(ctx, a)
	return newBody(EventMessageReceived, a.at, data), err
}

func (d *Dispatcher) settle(ctx context.Context, a attempt, result Result) {
	next := outcome(result, d.deps.Clock.Now())
	d.report(ctx, d.deps.Store.RecordAttempt(ctx, a.delivery.ID, next))
	d.deps.Audit.WebhookDelivery(ctx, audit.WebhookDelivery{
		Webhook:  a.webhook.Name,
		Account:  a.account.Name,
		Delivery: deliveryID(a.delivery),
		Events:   a.delivery.EventCount,
		Attempt:  a.delivery.Attempts,
		Status:   result.Status,
		Result:   next.State,
	})
}
