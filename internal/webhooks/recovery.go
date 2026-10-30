package webhooks

import (
	"context"
	"time"

	"github.com/ribeirogab/zapgate/internal/store"
)

const defaultRedeliverPeriod = 24 * time.Hour

func (r *Registry) Enable(ctx context.Context, name string) (store.Webhook, error) {
	webhook, err := r.deps.Store.EnableWebhook(ctx, name)
	if err == nil {
		r.deps.Wake()
	}
	return webhook, err
}

func (r *Registry) Redeliver(ctx context.Context, name, since string) (store.Webhook, int, error) {
	period, err := redeliverPeriod(since)
	if err != nil {
		return store.Webhook{}, 0, err
	}
	webhook, err := r.deps.Store.WebhookByName(ctx, name)
	if err != nil {
		return store.Webhook{}, 0, err
	}
	now := r.deps.Clock.Now()
	reopened, err := r.deps.Store.RedeliverFailed(ctx, webhook.ID, now.Add(-period), now)
	if err == nil {
		r.deps.Wake()
	}
	return webhook, reopened, err
}

func redeliverPeriod(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultRedeliverPeriod, nil
	}
	period, err := time.ParseDuration(raw)
	if err != nil {
		return 0, invalid("the period %q is not a duration: use a value such as 24h", raw)
	}
	if period <= 0 {
		return 0, invalid("the period %s is not valid: use a duration greater than 0s", period)
	}
	return period, nil
}
