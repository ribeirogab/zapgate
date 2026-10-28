package oauth

import (
	"errors"
	"net/http"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if oerr := parseTokenForm(r); oerr != nil {
		writeOAuthError(w, oerr)
		return
	}
	client, oerr := s.authenticateClient(w, r)
	if oerr != nil {
		writeOAuthError(w, oerr)
		return
	}
	token := r.PostForm.Get("token")
	if token == "" {
		writeOAuthError(w, newOAuthError(http.StatusBadRequest, "invalid_request", "token is required"))
		return
	}
	grant, err := s.deps.Store.RevokeGrantOfToken(r.Context(), HashToken(token), s.deps.Clock.Now(), func(g store.Grant) bool {
		return g.ClientID == client.ID
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		s.warn(r.Context(), "cannot revoke an OAuth token", err)
		writeOAuthError(w, newOAuthError(http.StatusServiceUnavailable, "temporarily_unavailable", "the token could not be revoked"))
		return
	default:
		s.deps.Grants.GrantRevoked(grant.ID)
		s.auditToken(r, audit.EventTokenRevoke, client, grant, audit.ResultOK, "")
	}
	w.Header().Set(headerCacheControl, cacheControlNoStore)
	w.WriteHeader(http.StatusOK)
}
