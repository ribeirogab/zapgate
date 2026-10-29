package webhooks

import (
	"encoding/json"
	"time"
)

type Body struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Data      any    `json:"data"`
}

type testData struct {
	Webhook string `json:"webhook"`
}

func newBody(kind string, at time.Time, data any) Body {
	return Body{Type: kind, Timestamp: at.UTC().Format(time.RFC3339), Data: data}
}

func testBody(webhook string, at time.Time) Body {
	return newBody(eventWebhookTest, at, testData{Webhook: webhook})
}

func encode(body Body) ([]byte, error) {
	return json.Marshal(body)
}
