package webhooks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

var errUnrecordedAttempt = errors.New("an earlier attempt stopped before its result was recorded; zapgate does not fire a routine twice")

type attempt struct {
	webhook  store.Webhook
	account  store.Account
	delivery store.Delivery
	at       time.Time
}

func (a attempt) followsAnUnrecordedAttempt() bool {
	return a.delivery.Attempts-a.delivery.RecordedAttempts > 1
}

func (d *Dispatcher) send(ctx context.Context, delivery store.Delivery) {
	a, err := d.prepare(ctx, delivery)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return
	case err != nil:
		d.fail(ctx, a, unbuilt(err))
	case !dropsDuplicates(a.webhook) && a.followsAnUnrecordedAttempt():
		d.fail(ctx, a, errUnrecordedAttempt)
	default:
		d.sendPost(ctx, a)
	}
}

func (d *Dispatcher) prepare(ctx context.Context, delivery store.Delivery) (attempt, error) {
	a := attempt{delivery: delivery, at: d.deps.Clock.Now()}
	webhook, err := d.deps.Store.Webhook(ctx, delivery.WebhookID)
	if err != nil {
		return a, err
	}
	a.webhook = webhook
	a.account, err = d.deps.Store.Account(ctx, delivery.AccountID)
	return a, err
}

func (d *Dispatcher) sendPost(ctx context.Context, a attempt) {
	post, err := d.buildPost(ctx, a)
	if err != nil {
		d.fail(ctx, a, unbuilt(err))
		return
	}
	result := d.deps.Sender.sendInSlot(ctx, post)
	if ctx.Err() == nil {
		d.settle(ctx, a, result)
	}
}

func unbuilt(err error) error {
	return fmt.Errorf("zapgate cannot build the delivery: %v", err)
}

func (d *Dispatcher) buildPost(ctx context.Context, a attempt) (Post, error) {
	body, err := d.body(ctx, a)
	if err != nil {
		return Post{}, err
	}
	encoded, err := formatted(a.webhook, body)
	return Post{
		URL:       a.webhook.URL,
		ID:        deliveryID(a.delivery),
		Timestamp: a.at,
		Body:      encoded,
		Secrets:   SigningSecrets(a.webhook, a.at),
		Headers:   outgoingHeaders(a.webhook),
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
	d.record(ctx, a, result, d.outcome(a, result, d.deps.Clock.Now()))
}

func (d *Dispatcher) fail(ctx context.Context, a attempt, reason error) {
	if ctx.Err() != nil {
		return
	}
	d.deps.Log.Error("a webhook delivery failed before its POST", "webhook", a.webhook.Name, "delivery", deliveryID(a.delivery), "error", reason)
	result := Result{Err: reason}
	d.record(ctx, a, result, settled(store.DeliveryFailed, result, d.deps.Clock.Now()))
}

func (d *Dispatcher) record(ctx context.Context, a attempt, result Result, next store.AttemptOutcome) {
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

func attemptResult(result Result, next store.AttemptOutcome) string {
	if result.Blocked() {
		return audit.ResultBlocked
	}
	return next.State
}
