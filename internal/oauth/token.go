package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	accessTokenLifetime  = time.Hour
	refreshTokenLifetime = 90 * 24 * time.Hour
	tokenTypeBearer      = "Bearer"
	errInvalidGrant      = "invalid_grant"
)

var verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type issuedTokens struct {
	response tokenResponse
	rows     []store.Token
}

func newTokens(now time.Time) issuedTokens {
	access := newSecret(accessTokenPrefix)
	refresh := newSecret(refreshTokenPrefix)
	return issuedTokens{
		response: tokenResponse{
			AccessToken:  access,
			TokenType:    tokenTypeBearer,
			ExpiresIn:    int64(accessTokenLifetime.Seconds()),
			RefreshToken: refresh,
			Scope:        Scope,
		},
		rows: []store.Token{
			{Hash: HashToken(access), Kind: store.TokenAccess, ExpiresAt: now.Add(accessTokenLifetime), CreatedAt: now},
			{Hash: HashToken(refresh), Kind: store.TokenRefresh, ExpiresAt: now.Add(refreshTokenLifetime), CreatedAt: now},
		},
	}
}

func invalidGrant(description string) *oauthError {
	return newOAuthError(http.StatusBadRequest, errInvalidGrant, "%s", description)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if oerr := parseTokenForm(r); oerr != nil {
		writeOAuthError(w, oerr)
		return
	}
	client, oerr := s.authenticateClient(w, r)
	if oerr != nil {
		writeOAuthError(w, oerr)
		return
	}
	if !s.knownResource(r.PostForm.Get("resource")) {
		writeOAuthError(w, newOAuthError(http.StatusBadRequest, "invalid_target", "the resource must be %s", s.resource()))
		return
	}
	switch grantType := r.PostForm.Get("grant_type"); grantType {
	case grantTypeAuthorizationCode:
		s.exchangeCode(w, r, client)
	case grantTypeRefreshToken:
		s.refreshTokens(w, r, client)
	case "":
		writeOAuthError(w, newOAuthError(http.StatusBadRequest, "invalid_request", "grant_type is required"))
	default:
		writeOAuthError(w, newOAuthError(http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token"))
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request, client store.OAuthClient) {
	code := r.PostForm.Get("code")
	if code == "" {
		writeOAuthError(w, newOAuthError(http.StatusBadRequest, "invalid_request", "code is required"))
		return
	}
	now := s.deps.Clock.Now()
	tokens := newTokens(now)
	grant, err := s.deps.Store.RedeemAuthCode(r.Context(), HashToken(code), now, func(c store.AuthCode) (store.NewGrant, error) {
		if err := checkCode(c, client, r, now); err != nil {
			return store.NewGrant{}, err
		}
		return store.NewGrant{
			ClientID:     client.ID,
			Name:         c.GrantName,
			Kind:         store.GrantOAuth,
			AccountNames: c.AccountNames,
			CreatedAt:    now,
			Tokens:       tokens.rows,
		}, nil
	})
	if err != nil {
		s.failGrant(w, r, client, audit.EventTokenIssue, audit.ReasonCodeReused, err)
		return
	}
	s.auditToken(r, audit.EventTokenIssue, client, grant, audit.ResultOK, "")
	writeJSON(w, http.StatusOK, tokens.response)
}

func checkCode(c store.AuthCode, client store.OAuthClient, r *http.Request, now time.Time) error {
	switch {
	case c.ClientID != client.ID:
		return invalidGrant("the code was issued to another client")
	case !now.Before(c.ExpiresAt):
		return invalidGrant("the code expired")
	case r.PostForm.Get("redirect_uri") != c.RedirectURI:
		return invalidGrant("redirect_uri does not match the authorization request")
	case !verifierMatches(r.PostForm.Get("code_verifier"), c.CodeChallenge):
		return invalidGrant("code_verifier does not match the code_challenge")
	}
	return nil
}

func verifierMatches(verifier, challenge string) bool {
	if !verifierPattern.MatchString(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return equalSecrets(base64.RawURLEncoding.EncodeToString(sum[:]), challenge)
}

func (s *Server) refreshTokens(w http.ResponseWriter, r *http.Request, client store.OAuthClient) {
	refresh := r.PostForm.Get("refresh_token")
	if refresh == "" {
		writeOAuthError(w, newOAuthError(http.StatusBadRequest, "invalid_request", "refresh_token is required"))
		return
	}
	now := s.deps.Clock.Now()
	tokens := newTokens(now)
	grant, err := s.deps.Store.RotateRefreshToken(r.Context(), HashToken(refresh), now, func(g store.Grant) ([]store.Token, error) {
		if g.ClientID != client.ID {
			return nil, invalidGrant("the refresh token was issued to another client")
		}
		return tokens.rows, nil
	})
	if err != nil {
		s.failGrant(w, r, client, audit.EventTokenRefresh, audit.ReasonRefreshReused, err)
		return
	}
	s.auditToken(r, audit.EventTokenRefresh, client, grant, audit.ResultOK, "")
	writeJSON(w, http.StatusOK, tokens.response)
}

func (s *Server) failGrant(w http.ResponseWriter, r *http.Request, client store.OAuthClient, event, reuseReason string, err error) {
	var (
		oerr    *oauthError
		reused  *store.ReusedError
		unknown *store.UnknownAccountError
	)
	switch {
	case errors.As(err, &oerr):
		writeOAuthError(w, oerr)
	case errors.As(err, &reused):
		if reused.Grant.ID != 0 {
			s.deps.Grants.GrantRevoked(reused.Grant.ID)
		}
		s.auditToken(r, event, client, reused.Grant, audit.ResultDenied, reuseReason)
		writeOAuthError(w, invalidGrant("the credential was already used; the access is revoked"))
	case errors.Is(err, store.ErrNotFound):
		writeOAuthError(w, invalidGrant("the credential is unknown, expired or revoked"))
	case errors.Is(err, store.ErrNameInUse):
		writeOAuthError(w, invalidGrant("the access name is now in use by another access"))
	case errors.As(err, &unknown):
		writeOAuthError(w, invalidGrant("the account "+unknown.Name+" no longer exists"))
	default:
		s.warn(r.Context(), "cannot issue OAuth tokens", err)
		writeOAuthError(w, newOAuthError(http.StatusInternalServerError, "server_error", "the tokens could not be issued"))
	}
}

func (s *Server) auditToken(r *http.Request, event string, client store.OAuthClient, grant store.Grant, result, reason string) {
	s.deps.Audit.OAuth(r.Context(), audit.OAuthEvent{
		Event:    event,
		Result:   result,
		Reason:   reason,
		ClientID: client.ID,
		Client:   client.Name,
		GrantID:  grant.ID,
		Grant:    grant.Name,
		IP:       audit.ClientIP(r),
	})
}
