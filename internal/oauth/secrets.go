package oauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

const (
	accessTokenPrefix  = "wmcp_at_"
	refreshTokenPrefix = "wmcp_rt_"
	clientSecretPrefix = "wmcp_cs_"
	secretBytes        = 32
	clientIDBytes      = 18
)

func randomText(n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func newSecret(prefix string) string {
	return prefix + randomText(secretBytes)
}

func equalSecrets(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
