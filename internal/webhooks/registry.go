package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	testIDPrefix = "test_"
	testIDBytes  = 12
)

type RegistryDeps struct {
	Store  *store.Store
	Clock  clock.Clock
	Sender *Sender
}

type Registry struct {
	deps RegistryDeps
}

func NewRegistry(deps RegistryDeps) *Registry {
	return &Registry{deps: deps}
}

type Registration struct {
	Name                string
	URL                 string
	AllExistingAccounts bool
	Accounts            []string
}

type Registered struct {
	store.Webhook
	Accounts []string
}

func (r Registration) validate() error {
	if err := validateName(r.Name); err != nil {
		return err
	}
	if err := validateURL(r.URL); err != nil {
		return err
	}
	return validateAccountChoice(r.AllExistingAccounts, r.Accounts)
}

func (r *Registry) Add(ctx context.Context, reg Registration) (Registered, string, error) {
	if err := reg.validate(); err != nil {
		return Registered{}, "", err
	}
	secret, err := NewSecret()
	if err != nil {
		return Registered{}, "", err
	}
	webhook, err := r.deps.Store.CreateWebhook(ctx, store.NewWebhook{
		Name:                reg.Name,
		URL:                 reg.URL,
		Format:              FormatStandard,
		Content:             ContentMinimal,
		Events:              defaultEvents,
		Chats:               ChatsAll,
		Quiet:               DefaultQuiet,
		MaxWait:             DefaultMaxWait,
		Secret:              secret,
		AllExistingAccounts: reg.AllExistingAccounts,
		AccountNames:        reg.Accounts,
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
		Secrets:   []string{webhook.Secret},
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
	return Registered{Webhook: webhook, Accounts: names}, nil
}

func newTestID() string {
	buf := make([]byte, testIDBytes)
	rand.Read(buf)
	return testIDPrefix + hex.EncodeToString(buf)
}
