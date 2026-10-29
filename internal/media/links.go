package media

import (
	"errors"
	"strings"
	"time"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/signed"
)

const (
	LinkPath     = "/media/"
	LinkLifetime = 15 * time.Minute
	linkKeyLabel = "zapgate media link v1"
)

var (
	errLinkInvalid = errors.New("the media link is not valid")
	errLinkExpired = errors.New("the media link expired")
)

type Links struct {
	signer    signed.Signer
	clock     clock.Clock
	publicURL string
}

type linkClaims struct {
	Path    string `json:"p"`
	Expires int64  `json:"e"`
}

func NewLinks(signingKey []byte, c clock.Clock, publicURL string) *Links {
	return &Links{signer: signed.NewSigner(signingKey, linkKeyLabel), clock: c, publicURL: strings.TrimRight(publicURL, "/")}
}

func (l *Links) Sign(path string) (string, time.Time) {
	expires := l.clock.Now().Add(LinkLifetime).Truncate(time.Second)
	return l.publicURL + LinkPath + l.signer.Sign(linkClaims{Path: path, Expires: expires.Unix()}), expires
}

func (l *Links) verify(token string) (string, error) {
	var claims linkClaims
	if err := l.signer.Open(token, &claims); err != nil || claims.Path == "" {
		return "", errLinkInvalid
	}
	if !l.clock.Now().Before(time.Unix(claims.Expires, 0)) {
		return "", errLinkExpired
	}
	return claims.Path, nil
}
