package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	codeLifetime        = 60 * time.Second
	codeBytes           = 32
	maxAccessNameRunes  = 32
	decisionDeny        = "deny"
	invalidRequestTitle = "Invalid authorization request"
	serverErrorTitle    = "Server error"
)

var (
	challengePattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)
	nameSeparators    = regexp.MustCompile(`[^a-z0-9]+`)
	requestParameters = []string{"response_type", "client_id", "redirect_uri", "code_challenge", "code_challenge_method", "state", "scope", "resource"}
)

type authorizationRequest struct {
	values url.Values
}

func readAuthorizationRequest(source url.Values) authorizationRequest {
	values := url.Values{}
	for _, key := range requestParameters {
		if value := source.Get(key); value != "" {
			values.Set(key, value)
		}
	}
	return authorizationRequest{values: values}
}

func (r authorizationRequest) get(key string) string { return r.values.Get(key) }

func (r authorizationRequest) hidden() []hiddenField {
	fields := make([]hiddenField, 0, len(r.values))
	for _, key := range requestParameters {
		if r.values.Has(key) {
			fields = append(fields, hiddenField{Name: key, Value: r.values.Get(key)})
		}
	}
	return fields
}

type redirectError struct {
	code        string
	description string
}

func (s *Server) authorizePage(w http.ResponseWriter, r *http.Request) {
	req := readAuthorizationRequest(r.URL.Query())
	client, ok := s.trustedClient(w, r.Context(), req)
	if !ok {
		return
	}
	if rerr := s.checkProtocol(req); rerr != nil {
		s.redirectError(w, r, req, rerr)
		return
	}
	renderAuthorizePage(w, http.StatusOK, s.pageFor(r.Context(), client, req, suggestedAccessName(client.Name), nil, ""))
}

func (s *Server) authorizeDecision(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		renderErrorPage(w, http.StatusBadRequest, invalidRequestTitle, "The form arrived incomplete.")
		return
	}
	req := readAuthorizationRequest(r.PostForm)
	client, ok := s.trustedClient(w, r.Context(), req)
	if !ok {
		return
	}
	if rerr := s.checkProtocol(req); rerr != nil {
		s.redirectError(w, r, req, rerr)
		return
	}
	ip := audit.ClientIP(r)
	if r.PostForm.Get("decision") == decisionDeny {
		s.auditAuthorize(r.Context(), client, "", audit.ResultDenied, audit.ReasonOwnerDeclined, ip)
		s.redirectError(w, r, req, &redirectError{code: "access_denied", description: "the owner declined the access"})
		return
	}
	choice := readChoice(r.PostForm)
	if retry, reserved := s.limiter.reserve(ip); !reserved {
		s.auditAuthorize(r.Context(), client, choice.name, audit.ResultBlocked, audit.ReasonWrongPassword, ip)
		renderBlockedPage(w, retry)
		return
	}
	if !equalSecrets(r.PostForm.Get("password"), s.deps.OwnerPassword) {
		s.auditAuthorize(r.Context(), client, choice.name, audit.ResultDenied, audit.ReasonWrongPassword, ip)
		renderAuthorizePage(w, http.StatusUnauthorized, s.pageFor(r.Context(), client, req, choice.name, choice.accounts, "Wrong password."))
		return
	}
	s.limiter.reset(ip)
	if problem := s.checkChoice(r.Context(), choice); problem != "" {
		renderAuthorizePage(w, http.StatusBadRequest, s.pageFor(r.Context(), client, req, choice.name, choice.accounts, problem))
		return
	}
	s.issueCode(w, r, client, req, choice, ip)
}

func (s *Server) trustedClient(w http.ResponseWriter, ctx context.Context, req authorizationRequest) (store.OAuthClient, bool) {
	clientID := req.get("client_id")
	if clientID == "" {
		renderErrorPage(w, http.StatusBadRequest, invalidRequestTitle, "The request does not say which app asks for access.")
		return store.OAuthClient{}, false
	}
	client, err := s.deps.Store.OAuthClient(ctx, clientID)
	if errors.Is(err, store.ErrNotFound) {
		renderErrorPage(w, http.StatusBadRequest, invalidRequestTitle, "This app is not registered on this server.")
		return client, false
	}
	if err != nil {
		s.warn(ctx, "cannot read an OAuth client", err)
		renderErrorPage(w, http.StatusInternalServerError, serverErrorTitle, "The server could not read the app.")
		return client, false
	}
	if !redirectRegistered(client.RedirectURIs, req.get("redirect_uri")) {
		renderErrorPage(w, http.StatusBadRequest, invalidRequestTitle, "The redirect address is not the one this app registered.")
		return client, false
	}
	return client, true
}

func (s *Server) checkProtocol(req authorizationRequest) *redirectError {
	switch {
	case req.get("response_type") != responseTypeCode:
		return &redirectError{code: "unsupported_response_type", description: "only response_type=code is supported"}
	case req.get("code_challenge_method") != challengeMethodS256:
		return &redirectError{code: "invalid_request", description: "PKCE with code_challenge_method=S256 is required"}
	case !challengePattern.MatchString(req.get("code_challenge")):
		return &redirectError{code: "invalid_request", description: "code_challenge must be a base64url S256 challenge"}
	case !s.knownResource(req.get("resource")):
		return &redirectError{code: "invalid_target", description: "the resource must be " + s.resource()}
	}
	return nil
}

func (s *Server) knownResource(resource string) bool {
	return slices.Contains([]string{"", s.resource(), s.issuer(), s.issuer() + "/"}, resource)
}

func (s *Server) redirectError(w http.ResponseWriter, r *http.Request, req authorizationRequest, rerr *redirectError) {
	s.redirect(w, r, req, url.Values{"error": {rerr.code}, "error_description": {rerr.description}})
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, req authorizationRequest, params url.Values) {
	target, _ := url.Parse(req.get("redirect_uri"))
	query := target.Query()
	for key, values := range params {
		query[key] = values
	}
	if state := req.get("state"); state != "" {
		query.Set("state", state)
	}
	query.Set("iss", s.issuer())
	target.RawQuery = query.Encode()
	w.Header().Set(headerCacheControl, cacheControlNoStore)
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

type choice struct {
	name     string
	accounts []string
}

func readChoice(form url.Values) choice {
	var accounts []string
	for _, name := range form["accounts"] {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(accounts, name) {
			accounts = append(accounts, name)
		}
	}
	return choice{name: strings.TrimSpace(form.Get("access_name")), accounts: accounts}
}

func (s *Server) checkChoice(ctx context.Context, c choice) string {
	if ValidateGrantName(c.name) != nil {
		return fmt.Sprintf("Invalid name: use lowercase letters, digits and hyphens, up to %d characters.", maxGrantNameChars)
	}
	if _, err := s.deps.Store.GrantByName(ctx, c.name); err == nil {
		return fmt.Sprintf("The name %s is already used by another access. Choose another one.", c.name)
	}
	accounts, err := s.deps.Store.ListAccounts(ctx)
	if err != nil {
		s.warn(ctx, "cannot list the accounts", err)
		return "The server could not read the accounts. Try again."
	}
	if len(accounts) > 0 && len(c.accounts) == 0 {
		return "Select at least one account."
	}
	for _, name := range c.accounts {
		if !slices.ContainsFunc(accounts, func(a store.Account) bool { return a.Name == name }) {
			return fmt.Sprintf("The account %s does not exist.", name)
		}
	}
	return ""
}

func (s *Server) issueCode(w http.ResponseWriter, r *http.Request, client store.OAuthClient, req authorizationRequest, c choice, ip string) {
	code := randomText(codeBytes)
	now := s.deps.Clock.Now()
	err := s.deps.Store.CreateAuthCode(r.Context(), store.AuthCode{
		Hash:          HashToken(code),
		ClientID:      client.ID,
		GrantName:     c.name,
		AccountNames:  c.accounts,
		CodeChallenge: req.get("code_challenge"),
		RedirectURI:   req.get("redirect_uri"),
		ExpiresAt:     now.Add(codeLifetime),
		CreatedAt:     now,
	})
	if err != nil {
		s.warn(r.Context(), "cannot store an authorization code", err)
		renderErrorPage(w, http.StatusInternalServerError, serverErrorTitle, "The server could not finish the authorization. Try again.")
		return
	}
	s.auditAuthorize(r.Context(), client, c.name, audit.ResultOK, "", ip)
	s.redirect(w, r, req, url.Values{"code": {code}})
}

func (s *Server) auditAuthorize(ctx context.Context, client store.OAuthClient, grant, result, reason, ip string) {
	s.deps.Audit.OAuth(ctx, audit.OAuthEvent{Event: audit.EventAuthorize, Result: result, Reason: reason, ClientID: client.ID, Client: client.Name, Grant: grant, IP: ip})
}

func (s *Server) pageFor(ctx context.Context, client store.OAuthClient, req authorizationRequest, accessName string, checked []string, problem string) pageData {
	redirect, _ := url.Parse(req.get("redirect_uri"))
	data := pageData{
		ClientName:   clientLabel(client),
		RedirectHost: redirectHost(redirect),
		Loopback:     isLoopback(redirect),
		Error:        problem,
		Hidden:       req.hidden(),
		AccessName:   accessName,
	}
	accounts, err := s.deps.Store.ListAccounts(ctx)
	if err != nil {
		s.warn(ctx, "cannot list the accounts", err)
	}
	for _, account := range accounts {
		data.Accounts = append(data.Accounts, accountChoice{Name: account.Name, Detail: accountDetail(account), Checked: slices.Contains(checked, account.Name)})
	}
	return data
}

func clientLabel(client store.OAuthClient) string {
	if client.Name == "" {
		return "An unnamed app"
	}
	return client.Name
}

func redirectHost(u *url.URL) string {
	if u.Host != "" {
		return u.Host
	}
	return u.Scheme + ":"
}

func accountDetail(a store.Account) string {
	detail := a.Phone
	if a.ProfileName != "" {
		detail += " · " + a.ProfileName
	}
	if a.Description != "" {
		detail += " · " + a.Description
	}
	return detail
}

func suggestedAccessName(clientName string) string {
	slug := strings.Trim(nameSeparators.ReplaceAllString(strings.ToLower(clientName), "-"), "-")
	if len(slug) > maxAccessNameRunes {
		slug = strings.TrimRight(slug[:maxAccessNameRunes], "-")
	}
	return slug
}
