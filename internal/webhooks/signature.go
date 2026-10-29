package webhooks

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	SecretPrefix     = "whsec_"
	secretBytes      = 32
	signatureVersion = "v1"
)

var errSecretFormat = errors.New("the webhook secret is not whsec_ plus base64")

func NewSecret() (string, error) {
	key := make([]byte, secretBytes)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return SecretPrefix + base64.StdEncoding.EncodeToString(key), nil
}

func Sign(id string, at time.Time, body []byte, secrets ...string) (string, error) {
	signatures := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		signature, err := signWith(secret, signedContent(id, at, body))
		if err != nil {
			return "", err
		}
		signatures = append(signatures, signatureVersion+","+signature)
	}
	return strings.Join(signatures, " "), nil
}

func signedContent(id string, at time.Time, body []byte) []byte {
	content := make([]byte, 0, len(id)+len(body)+24)
	content = append(content, id...)
	content = append(content, '.')
	content = strconv.AppendInt(content, at.Unix(), 10)
	content = append(content, '.')
	return append(content, body...)
}

func signWith(secret string, content []byte) (string, error) {
	key, err := secretKey(secret)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(content)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func secretKey(secret string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, SecretPrefix))
	if err != nil || len(key) == 0 {
		return nil, errSecretFormat
	}
	return key, nil
}
