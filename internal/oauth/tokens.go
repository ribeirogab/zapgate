package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/names"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	StaticTokenPrefix = "wmcp_"
	GrantNameExtra    = "grant_name"
	maxGrantNameChars = 64
)

func NewStaticToken() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return StaticTokenPrefix + base64.RawURLEncoding.EncodeToString(secret), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func ValidateGrantName(name string) error {
	if !names.Valid(name, maxGrantNameChars) {
		return fmt.Errorf("the access name %q is not valid: use lowercase letters, digits and single hyphens, at most %d characters", name, maxGrantNameChars)
	}
	return nil
}

func Verifier(st *store.Store, clk clock.Clock) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		now := clk.Now()
		bearer, err := st.GrantForBearer(ctx, HashToken(token), now)
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: unknown, revoked or expired token", auth.ErrInvalidToken)
		}
		if err != nil {
			return nil, err
		}
		return &auth.TokenInfo{
			UserID:     strconv.FormatInt(bearer.Grant.ID, 10),
			Expiration: wallClockExpiry(bearer.ExpiresAt, now),
			Extra:      map[string]any{GrantNameExtra: bearer.Grant.Name},
		}, nil
	}
}

func wallClockExpiry(expiresAt, now time.Time) time.Time {
	if expiresAt.IsZero() {
		return time.Time{}
	}
	return time.Now().Add(expiresAt.Sub(now))
}

func GrantID(info *auth.TokenInfo) (int64, bool) {
	if info == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(info.UserID, 10, 64)
	return id, err == nil
}

func GrantName(info *auth.TokenInfo) string {
	if info == nil {
		return ""
	}
	name, _ := info.Extra[GrantNameExtra].(string)
	return name
}
