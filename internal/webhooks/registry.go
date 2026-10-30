package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	testIDPrefix = "test_"
	testIDBytes  = 12
)

type Clients interface {
	Client(accountID int64) (whatsapp.Client, bool)
}

type RegistryDeps struct {
	Store    *store.Store
	Clock    clock.Clock
	Sender   *Sender
	Resolver Resolver
	Clients  Clients
}

type Registry struct {
	deps RegistryDeps
}

func NewRegistry(deps RegistryDeps) *Registry {
	return &Registry{deps: deps}
}

type Registration struct {
	Name string
	Options
}

type Registered struct {
	store.Webhook
	Accounts    []string
	ListedChats []string
}

func (r Registration) validate(ctx context.Context, resolver Resolver) (config, error) {
	if err := validateName(r.Name); err != nil {
		return config{}, err
	}
	if r.URL == "" {
		return config{}, invalid("the webhook needs a URL")
	}
	if err := validateAccountChoice(r.AllExistingAccounts, r.Accounts); err != nil {
		return config{}, err
	}
	return r.apply(ctx, resolver, defaultConfig(r.Name))
}

func (r *Registry) Add(ctx context.Context, reg Registration) (Registered, string, error) {
	c, err := reg.validate(ctx, r.deps.Resolver)
	if err != nil {
		return Registered{}, "", err
	}
	secret, err := NewSecret()
	if err != nil {
		return Registered{}, "", err
	}
	webhook, err := r.deps.Store.CreateWebhook(ctx, store.NewWebhook{
		Name:                c.webhook.Name,
		URL:                 c.webhook.URL,
		Format:              c.webhook.Format,
		Content:             c.webhook.Content,
		Events:              c.webhook.Events,
		Chats:               c.webhook.Chats,
		Quiet:               c.webhook.Quiet,
		MaxWait:             c.webhook.MaxWait,
		Headers:             c.webhook.Headers,
		Secret:              secret,
		AllExistingAccounts: reg.AllExistingAccounts,
		AccountNames:        reg.Accounts,
		ListedChats:         r.listedChats(ctx, c),
		CreatedAt:           r.deps.Clock.Now(),
	})
	if err != nil {
		return Registered{}, "", err
	}
	registered, err := r.registered(ctx, webhook)
	return registered, secret, err
}

func (r *Registry) List(ctx context.Context) ([]Registered, error) {
	webhooks, err := r.deps.Store.ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]Registered, 0, len(webhooks))
	for _, webhook := range webhooks {
		registered, err := r.registered(ctx, webhook)
		if err != nil {
			return nil, err
		}
		list = append(list, registered)
	}
	return list, nil
}

func (r *Registry) Remove(ctx context.Context, name string) (store.Webhook, error) {
	return r.deps.Store.DeleteWebhook(ctx, name)
}

func (r *Registry) Test(ctx context.Context, name string) (store.Webhook, Result, error) {
	webhook, err := r.deps.Store.WebhookByName(ctx, name)
	if err != nil {
		return store.Webhook{}, Result{}, err
	}
	now := r.deps.Clock.Now()
	body, err := encode(testBody(webhook.Name, now))
	if err != nil {
		return store.Webhook{}, Result{}, err
	}
	result := r.deps.Sender.Send(ctx, Post{
		URL:       webhook.URL,
		ID:        newTestID(),
		Timestamp: now,
		Body:      body,
		Secrets:   SigningSecrets(webhook, now),
		Headers:   webhook.Headers,
	})
	return webhook, result, nil
}

func (r *Registry) registered(ctx context.Context, webhook store.Webhook) (Registered, error) {
	accounts, err := r.deps.Store.WebhookAccounts(ctx, webhook)
	if err != nil {
		return Registered{}, err
	}
	names := make([]string, 0, len(accounts))
	for _, account := range accounts {
		names = append(names, account.Name)
	}
	chats, err := r.listedChatJIDs(ctx, webhook)
	return Registered{Webhook: webhook, Accounts: names, ListedChats: chats}, err
}

func newTestID() string {
	buf := make([]byte, testIDBytes)
	rand.Read(buf)
	return testIDPrefix + hex.EncodeToString(buf)
}
