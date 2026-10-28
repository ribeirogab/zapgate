package signed

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalid = errors.New("the signed token is not valid")

type Signer struct {
	key []byte
}

func NewSigner(secret []byte, label string) Signer {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(label))
	return Signer{key: mac.Sum(nil)}
}

func (s Signer) Sign(claims any) string {
	body, err := json.Marshal(claims)
	if err != nil {
		panic(fmt.Sprintf("marshal signed claims: %v", err))
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + s.signature(payload)
}

func (s Signer) Open(token string, claims any) error {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(signature), []byte(s.signature(payload))) {
		return ErrInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(body, claims) != nil {
		return ErrInvalid
	}
	return nil
}

func (s Signer) signature(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
