package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const maxRedirectURIChars = 2048

var (
	loopbackHosts   = []string{"localhost", "127.0.0.1", "::1"}
	forbiddenScheme = []string{"javascript", "data", "file", "vbscript", "about", "blob", "ftp", "ws", "wss"}
)

func validateRedirectURI(raw string) error {
	if len(raw) > maxRedirectURIChars {
		return fmt.Errorf("the redirect URI is longer than %d characters", maxRedirectURIChars)
	}
	if strings.Contains(raw, "#") {
		return errors.New("the redirect URI must not have a fragment")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("the redirect URI %q is not an absolute URI", raw)
	}
	if u.User != nil {
		return errors.New("the redirect URI must not carry user information")
	}
	return validateScheme(u)
}

func validateScheme(u *url.URL) error {
	switch scheme := strings.ToLower(u.Scheme); {
	case scheme == "https":
		if u.Host == "" {
			return errors.New("an https redirect URI needs a host")
		}
	case scheme == "http":
		if !isLoopback(u) {
			return errors.New("an http redirect URI must point to a loopback address")
		}
	case slices.Contains(forbiddenScheme, scheme):
		return fmt.Errorf("the redirect URI scheme %q is not allowed", scheme)
	case u.Host == "" && u.Path == "" && u.Opaque == "":
		return errors.New("a private-use redirect URI needs a path")
	}
	return nil
}

func isLoopback(u *url.URL) bool {
	return strings.EqualFold(u.Scheme, "http") && slices.Contains(loopbackHosts, strings.ToLower(u.Hostname()))
}

func redirectRegistered(registered []string, requested string) bool {
	if slices.Contains(registered, requested) {
		return true
	}
	asked, err := url.Parse(requested)
	if err != nil || !isLoopback(asked) {
		return false
	}
	for _, candidate := range registered {
		if sameLoopbackIgnoringPort(candidate, asked) {
			return true
		}
	}
	return false
}

func sameLoopbackIgnoringPort(registered string, asked *url.URL) bool {
	known, err := url.Parse(registered)
	if err != nil || !isLoopback(known) {
		return false
	}
	return known.Scheme == asked.Scheme &&
		known.Hostname() == asked.Hostname() &&
		known.Path == asked.Path &&
		known.RawQuery == asked.RawQuery &&
		asked.User == nil &&
		asked.Fragment == ""
}
