package webhooks

import (
	"encoding/json"
	"maps"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	headerAnthropicBeta    = "Anthropic-Beta"
	headerAnthropicVersion = "Anthropic-Version"
	routineBeta            = "experimental-cc-routine-2026-04-01"
	routineAPIVersion      = "2026-10-21"
)

type routineBody struct {
	Text string `json:"text"`
}

func formatted(webhook store.Webhook, body Body) ([]byte, error) {
	encoded, err := encode(body)
	if err != nil || webhook.Format != FormatClaudeRoutine {
		return encoded, err
	}
	return json.Marshal(routineBody{Text: string(encoded)})
}

func outgoingHeaders(webhook store.Webhook) map[string]string {
	if webhook.Format != FormatClaudeRoutine {
		return webhook.Headers
	}
	headers := map[string]string{headerAnthropicBeta: routineBeta, headerAnthropicVersion: routineAPIVersion}
	maps.Copy(headers, webhook.Headers)
	return headers
}
