package webhooks

import (
	"context"
	"time"

	"github.com/ribeirogab/zapgate/internal/store"
)

const rotationOverlap = 24 * time.Hour

func SigningSecrets(w store.Webhook, now time.Time) []string {
	if w.PreviousSecret == "" || !now.Before(w.PreviousSecretUntil) {
		return []string{w.Secret}
	}
	return []string{w.Secret, w.PreviousSecret}
}

func (r *Registry) RotateSecret(ctx context.Context, name string) (store.Webhook, error) {
	secret, err := NewSecret()
	if err != nil {
		return store.Webhook{}, err
	}
	return r.deps.Store.RotateWebhookSecret(ctx, name, secret, r.deps.Clock.Now().Add(rotationOverlap))
}
