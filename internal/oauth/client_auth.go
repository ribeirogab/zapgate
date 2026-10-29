package oauth

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/ribeirogab/zapgate/internal/store"
)

const basicChallenge = `Basic realm="zapgate"`

func (s *Server) authenticateClient(w http.ResponseWriter, r *http.Request) (store.OAuthClient, *oauthError) {
	clientID, secret, usedBasic := clientCredentials(r)
	invalid := newOAuthError(http.StatusUnauthorized, "invalid_client", "the client is unknown or did not authenticate")
	if usedBasic {
		w.Header().Set("WWW-Authenticate", basicChallenge)
	}
	if clientID == "" {
		return store.OAuthClient{}, invalid
	}
	client, err := s.deps.Store.OAuthClient(r.Context(), clientID)
	if errors.Is(err, store.ErrNotFound) {
		return client, invalid
	}
	if err != nil {
		s.warn(r.Context(), "cannot read an OAuth client", err)
		return client, newOAuthError(http.StatusInternalServerError, "server_error", "the client could not be read")
	}
	if client.AuthMethod != authMethodNone && (secret == "" || !equalSecrets(HashToken(secret), client.SecretHash)) {
		return client, invalid
	}
	return client, nil
}

func clientCredentials(r *http.Request) (string, string, bool) {
	if user, password, ok := r.BasicAuth(); ok {
		id, idErr := url.QueryUnescape(user)
		secret, secretErr := url.QueryUnescape(password)
		if idErr == nil && secretErr == nil {
			return id, secret, true
		}
		return "", "", true
	}
	return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret"), false
}

func parseTokenForm(r *http.Request) *oauthError {
	r.Body = http.MaxBytesReader(nil, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		return newOAuthError(http.StatusBadRequest, "invalid_request", "the body must be application/x-www-form-urlencoded")
	}
	return nil
}
