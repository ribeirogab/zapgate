package webhooks

import (
	"context"

	"github.com/ribeirogab/zapgate/internal/store"
)

func (r *Registry) Set(ctx context.Context, name string, opts Options) (Registered, error) {
	current, err := r.current(ctx, name)
	if err != nil {
		return Registered{}, err
	}
	if opts.accountsGiven() {
		if err := validateAccountChoice(opts.AllExistingAccounts, opts.Accounts); err != nil {
			return Registered{}, err
		}
	}
	c, err := opts.apply(ctx, r.deps.Resolver, current)
	if err != nil {
		return Registered{}, err
	}
	webhook, err := r.deps.Store.ChangeWebhook(ctx, store.WebhookChange{
		Webhook:             c.webhook,
		ChangeAccounts:      opts.accountsGiven(),
		AllExistingAccounts: opts.AllExistingAccounts,
		AccountNames:        opts.Accounts,
		ListedChats:         r.listedChats(ctx, c),
	})
	if err != nil {
		return Registered{}, err
	}
	return r.registered(ctx, webhook)
}

func (r *Registry) current(ctx context.Context, name string) (config, error) {
	webhook, err := r.deps.Store.WebhookByName(ctx, name)
	if err != nil {
		return config{}, err
	}
	chats, err := r.storedChats(ctx, webhook)
	return config{webhook: webhook, chats: chats}, err
}
