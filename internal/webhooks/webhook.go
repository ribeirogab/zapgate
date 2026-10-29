package webhooks

import (
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

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != httpScheme && u.Scheme != httpsScheme) || u.Host == "" {
		return invalid("the webhook URL %q is not valid: use an http or https URL with a host", raw)
	}
	return nil
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
