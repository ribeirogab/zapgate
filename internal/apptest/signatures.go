package apptest

import (
	"strings"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
)

func (r Received) SignedBy(secrets ...string) []string {
	signatures := strings.Fields(r.Header.Get(standardwebhooks.HeaderWebhookSignature))
	signers := make([]string, 0, len(signatures))
	for _, signature := range signatures {
		signers = append(signers, r.signerOf(signature, secrets))
	}
	return signers
}

func (r Received) signerOf(signature string, secrets []string) string {
	header := r.Header.Clone()
	header.Set(standardwebhooks.HeaderWebhookSignature, signature)
	for _, secret := range secrets {
		webhook, err := standardwebhooks.NewWebhook(secret)
		if err == nil && webhook.VerifyIgnoringTimestamp(r.Body, header) == nil {
			return secret
		}
	}
	return ""
}
