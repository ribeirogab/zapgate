package webhooks

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/ribeirogab/zapgate/internal/names"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	EventMessageReceived = "message.received"
	EventAccountState    = "account.state"
	eventWebhookTest     = "webhook.test"
)

const (
	ContentMinimal = "minimal"
	ContentFull    = "full"
)

const (
	FormatStandard      = "standard"
	FormatClaudeRoutine = "claude-routine"
)

const (
	ChatsAll    = "all"
	ChatsDirect = "direct"
	ChatsGroups = "groups"
	ChatsListed = "listed"
)

const (
	StateEnabled  = "enabled"
	StateDisabled = "disabled"
)

const (
	DefaultQuiet   = 5 * time.Second
	DefaultMaxWait = 30 * time.Second
	maxNameChars   = 64
	httpScheme     = "http"
	httpsScheme    = "https"
)

var defaultEvents = []string{EventMessageReceived}

type InvalidError struct {
	Message string
}

func (e *InvalidError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

func State(w store.Webhook) string {
	if w.Disabled() {
		return StateDisabled
	}
	return StateEnabled
}

func validateName(name string) error {
	if !names.Valid(name, maxNameChars) {
		return invalid("the webhook name %q is not valid: use lowercase letters, digits and single hyphens, at most %d characters", name, maxNameChars)
	}
	return nil
}

func validateURL(ctx context.Context, resolver Resolver, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != httpScheme && u.Scheme != httpsScheme) || u.Hostname() == "" {
		return invalid("the webhook URL %q is not valid: use an http or https URL with a host", raw)
	}
	if u.Scheme == httpsScheme {
		return nil
	}
	addrs, err := lookup(ctx, resolver, u.Hostname())
	if err != nil {
		return invalid("the webhook URL %q is not valid: the host %s does not resolve", raw, u.Hostname())
	}
	if err := requireLocal(u.Hostname(), addrs); err != nil {
		return invalid("the webhook URL %q is not valid: %v", raw, err)
	}
	return nil
}

func validateContent(content string) error {
	switch content {
	case "", ContentMinimal, ContentFull:
		return nil
	}
	return invalid("the content level %q is not valid: use %s or %s", content, ContentMinimal, ContentFull)
}

func contentOrDefault(content string) string {
	if content == "" {
		return ContentMinimal
	}
	return content
}

func validateAccountChoice(allExisting bool, accounts []string) error {
	if allExisting && len(accounts) > 0 {
		return invalid("choose all existing accounts or a list of accounts, not both")
	}
	if !allExisting && len(accounts) == 0 {
		return invalid("the webhook needs a list of accounts, or all existing accounts")
	}
	return nil
}
