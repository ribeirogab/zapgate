package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	maxRedirectURIs       = 10
	unusedClientLifetime  = 24 * time.Hour
	maxClientNameRunes    = 100
	errInvalidRedirectURI = "invalid_redirect_uri"
	errInvalidClientMeta  = "invalid_client_metadata"
)

var (
	supportedGrantTypes    = []string{grantTypeAuthorizationCode, grantTypeRefreshToken}
	supportedResponseTypes = []string{responseTypeCode}
)

type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
}

type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientSecretExpiresAt   *int64   `json:"client_secret_expires_at,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type oauthError struct {
	status      int
	Code        string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func (e *oauthError) Error() string { return e.Code + ": " + e.Description }

func newOAuthError(status int, code, format string, args ...any) *oauthError {
	return &oauthError{status: status, Code: code, Description: fmt.Sprintf(format, args...)}
}

func writeOAuthError(w http.ResponseWriter, err *oauthError) {
	writeJSON(w, err.status, err)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	req, oerr := decodeRegistration(r)
	if oerr == nil {
		oerr = validateRegistration(&req)
	}
	if oerr != nil {
		writeOAuthError(w, oerr)
		return
	}
	now := s.deps.Clock.Now()
	client := store.OAuthClient{
		ID:           randomText(clientIDBytes),
		Name:         cleanClientName(req.ClientName),
		RedirectURIs: req.RedirectURIs,
		AuthMethod:   req.TokenEndpointAuthMethod,
		CreatedAt:    now,
	}
	resp := registrationResponse{
		ClientID:                client.ID,
		ClientIDIssuedAt:        now.Unix(),
		ClientName:              client.Name,
		RedirectURIs:            client.RedirectURIs,
		GrantTypes:              supportedGrantTypes,
		ResponseTypes:           supportedResponseTypes,
		TokenEndpointAuthMethod: client.AuthMethod,
	}
	if client.AuthMethod != authMethodNone {
		resp.ClientSecret = newSecret(clientSecretPrefix)
		resp.ClientSecretExpiresAt = new(int64)
		client.SecretHash = HashToken(resp.ClientSecret)
	}
	if err := s.deps.Store.ForgetUnusedOAuthClients(r.Context(), now.Add(-unusedClientLifetime), now); err != nil {
		s.warn(r.Context(), "cannot forget the unused OAuth clients", err)
	}
	if err := s.deps.Store.CreateOAuthClient(r.Context(), client); err != nil {
		s.warn(r.Context(), "cannot store an OAuth client", err)
		writeOAuthError(w, newOAuthError(http.StatusInternalServerError, "server_error", "the client could not be stored"))
		return
	}
	s.deps.Audit.OAuth(r.Context(), audit.OAuthEvent{Event: audit.EventClientRegister, Result: audit.ResultOK, ClientID: client.ID, Client: client.Name, IP: audit.ClientIP(r)})
	writeJSON(w, http.StatusCreated, resp)
}

func decodeRegistration(r *http.Request) (registrationRequest, *oauthError) {
	var req registrationRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxFormBytes)).Decode(&req); err != nil {
		return req, newOAuthError(http.StatusBadRequest, errInvalidClientMeta, "the body is not a JSON client metadata object: %v", err)
	}
	return req, nil
}

func validateRegistration(req *registrationRequest) *oauthError {
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > maxRedirectURIs {
		return newOAuthError(http.StatusBadRequest, errInvalidRedirectURI, "send between 1 and %d redirect_uris", maxRedirectURIs)
	}
	for _, uri := range req.RedirectURIs {
		if err := validateRedirectURI(uri); err != nil {
			return newOAuthError(http.StatusBadRequest, errInvalidRedirectURI, "%v", err)
		}
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = authMethodNone
	}
	if !slices.Contains(supportedAuthMethods, req.TokenEndpointAuthMethod) {
		return newOAuthError(http.StatusBadRequest, errInvalidClientMeta, "token_endpoint_auth_method must be one of %s", strings.Join(supportedAuthMethods, ", "))
	}
	if !subset(req.GrantTypes, supportedGrantTypes) {
		return newOAuthError(http.StatusBadRequest, errInvalidClientMeta, "grant_types must be within %s", strings.Join(supportedGrantTypes, ", "))
	}
	if !subset(req.ResponseTypes, supportedResponseTypes) {
		return newOAuthError(http.StatusBadRequest, errInvalidClientMeta, "response_types must be %s", responseTypeCode)
	}
	return nil
}

func subset(values, allowed []string) bool {
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return false
		}
	}
	return true
}

func cleanClientName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	runes := []rune(name)
	if len(runes) > maxClientNameRunes {
		return string(runes[:maxClientNameRunes])
	}
	return name
}
