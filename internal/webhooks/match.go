package webhooks

import (
	"context"
	"slices"

	"github.com/ribeirogab/zapgate/internal/store"
)

type matcher struct {
	webhook  store.Webhook
	accounts map[int64]bool
}

func (d *Dispatcher) matcherFor(ctx context.Context, w store.Webhook) (matcher, error) {
	accounts, err := d.deps.Store.WebhookAccounts(ctx, w)
	m := matcher{webhook: w, accounts: map[int64]bool{}}
	for _, account := range accounts {
		m.accounts[account.ID] = true
	}
	return m, err
}

func (m matcher) keep(journal []store.Event) []store.Event {
	var kept []store.Event
	for _, event := range journal {
		if m.matches(event) {
			kept = append(kept, event)
		}
	}
	return kept
}

func (m matcher) matches(e store.Event) bool {
	return !m.webhook.Disabled() && m.accounts[e.AccountID] && slices.Contains(m.webhook.Events, e.Kind)
}
