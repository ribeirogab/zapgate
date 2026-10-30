package webhooks

import (
	"context"
	"slices"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
)

type Options struct {
	URL                 string
	AllExistingAccounts bool
	Accounts            []string
	Chats               string
	Events              []string
}

type config struct {
	webhook store.Webhook
	chats   []types.JID
}

func defaultConfig(name string) config {
	return config{webhook: store.Webhook{
		Name:    name,
		Format:  FormatStandard,
		Content: ContentMinimal,
		Events:  defaultEvents,
		Chats:   ChatsAll,
		Quiet:   DefaultQuiet,
		MaxWait: DefaultMaxWait,
	}}
}

func (o Options) accountsGiven() bool {
	return o.AllExistingAccounts || o.Accounts != nil
}

func (o Options) apply(ctx context.Context, resolver Resolver, c config) (config, error) {
	if err := o.applyURL(ctx, resolver, &c); err != nil {
		return config{}, err
	}
	steps := []func(context.Context, *config) error{
		o.applyEvents,
		o.applyChats,
	}
	for _, step := range steps {
		if err := step(ctx, &c); err != nil {
			return config{}, err
		}
	}
	return c, nil
}

func (o Options) applyURL(ctx context.Context, resolver Resolver, c *config) error {
	if o.URL == "" {
		return nil
	}
	if err := validateURL(ctx, resolver, o.URL); err != nil {
		return err
	}
	c.webhook.URL = o.URL
	return nil
}

func (o Options) applyEvents(_ context.Context, c *config) error {
	if o.Events == nil {
		return nil
	}
	for _, event := range o.Events {
		if !slices.Contains(knownEvents, event) {
			return invalid("the event %q is not known: use %s", event, knownEventsLabel)
		}
	}
	if len(o.Events) == 0 {
		return invalid("the webhook needs at least one event: use %s", knownEventsLabel)
	}
	c.webhook.Events = slices.DeleteFunc(slices.Clone(knownEvents), func(event string) bool { return !slices.Contains(o.Events, event) })
	return nil
}

func (o Options) applyChats(ctx context.Context, c *config) error {
	if o.Chats == "" {
		return nil
	}
	switch kind := strings.TrimSpace(o.Chats); kind {
	case ChatsAll, ChatsDirect, ChatsGroups:
		c.webhook.Chats, c.chats = kind, nil
		return nil
	}
	chats, err := parseChatList(ctx, o.Chats)
	if err != nil {
		return err
	}
	c.webhook.Chats, c.chats = ChatsListed, chats
	return nil
}
