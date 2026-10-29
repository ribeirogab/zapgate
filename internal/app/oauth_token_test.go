package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func requireTokenError(t *testing.T, resp *http.Response, body map[string]any, status int, code string) {
	t.Helper()
	if resp.StatusCode != status || body["error"] != code {
		t.Fatalf("status %d, body %v; want %d %s", resp.StatusCode, body, status, code)
	}
}

const initializeRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`

func requireMCPStatus(t *testing.T, h *apptest.Harness, token string, status int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.URL+"/mcp", strings.NewReader(initializeRequest))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	if resp := h.Do(t, req); resp.StatusCode != status {
		t.Fatalf("POST /mcp initialize status %d, want %d", resp.StatusCode, status)
	}
}

func TestOAuthLoginGivesTokensForTheChosenAccounts(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.AddAccount(t, "personal", personalPhone, "Alex")

			_, tokens := h.OAuthLogin(t, "claude", "work")

			if !strings.HasPrefix(tokens.Access, "wmcp_at_") || !strings.HasPrefix(tokens.Refresh, "wmcp_rt_") {
				t.Fatalf("tokens %v", tokens.Body)
			}
			delete(tokens.Body, "access_token")
			delete(tokens.Body, "refresh_token")
			if got := canonicalJSON(t, tokens.Body); got != `{"expires_in":3600,"scope":"whatsapp","token_type":"Bearer"}` {
				t.Fatalf("token response %s", got)
			}
			session := h.Connect(t, tokens.Access, apptest.WithProtocol(path.protocol))
			if got := accountNames(t, session); strings.Join(got, ",") != "work" {
				t.Fatalf("the OAuth access reaches %v, want work", got)
			}
			if rows := tableRows(t, h.CLI(t, "access", "list").Stdout); len(rows) != 2 || rows[1] != "claude oauth work read-write elicitation 2026-10-13T12:00:00Z" {
				t.Fatalf("access list %q", rows)
			}
		})
	}
}

func TestOAuthLoginWithTwoAccounts(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")

	_, tokens := h.OAuthLogin(t, "claude", "work", "personal")

	session := h.Connect(t, tokens.Access)
	if got := accountNames(t, session); strings.Join(got, ",") != "personal,work" {
		t.Fatalf("the OAuth access reaches %v, want personal,work", got)
	}
	if rows := tableRows(t, h.CLI(t, "access", "list").Stdout); rows[1] != "claude oauth personal,work read-write elicitation 2026-10-13T12:00:00Z" {
		t.Fatalf("access list %q", rows)
	}
}

func TestOAuthLoginWithoutAccountsReachesNone(t *testing.T) {
	h := apptest.Start(t)

	_, tokens := h.OAuthLogin(t, "claude")

	session := h.Connect(t, tokens.Access)
	if got := accountNames(t, session); len(got) != 0 {
		t.Fatalf("the OAuth access reaches %v, want none", got)
	}
}

func TestAccessSetChangesTheAccountsOfAnOAuthAccess(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	login, tokens := h.OAuthLogin(t, "claude", "work", "personal")
	session := h.Connect(t, tokens.Access)

	set := h.CLI(t, "access", "set", "claude", "--accounts", "personal")

	if set.Code != 0 {
		t.Fatalf("exit %d, stderr %q", set.Code, set.Stderr)
	}
	if got := accountNames(t, session); strings.Join(got, ",") != "personal" {
		t.Fatalf("the open session reaches %v after access set, want personal", got)
	}
	_, refreshed := h.Refresh(t, login.Client, tokens.Refresh)
	if got := accountNames(t, h.Connect(t, apptest.TokensFrom(refreshed).Access)); strings.Join(got, ",") != "personal" {
		t.Fatalf("a refreshed token reaches %v, want personal", got)
	}
}

func TestAuthorizationCodeIsSingleUse(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	code := h.Authorize(t, login, "claude")
	_, first := h.ExchangeCode(t, login, code)
	tokens := apptest.TokensFrom(first)

	resp, body := h.ExchangeCode(t, login, code)

	requireTokenError(t, resp, body, http.StatusBadRequest, "invalid_grant")
	requireMCPStatus(t, h, tokens.Access, http.StatusUnauthorized)
	if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
		t.Fatalf("a reused code left the access alive: %q", listed.Stdout)
	}
	refresh, refreshBody := h.Refresh(t, login.Client, tokens.Refresh)
	requireTokenError(t, refresh, refreshBody, http.StatusBadRequest, "invalid_grant")
}

func TestAuthorizationCodeLastsSixtySeconds(t *testing.T) {
	cases := []struct {
		wait   time.Duration
		status int
	}{
		{wait: 59 * time.Second, status: http.StatusOK},
		{wait: 60 * time.Second, status: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.wait.String(), func(t *testing.T) {
			h := apptest.Start(t)
			login := h.NewLogin(t)
			code := h.Authorize(t, login, "claude")
			h.Clock.Advance(tc.wait)

			resp, body := h.ExchangeCode(t, login, code)

			if resp.StatusCode != tc.status {
				t.Fatalf("status %d, want %d: %v", resp.StatusCode, tc.status, body)
			}
		})
	}
}

func TestCodeExchangeChecksEveryBinding(t *testing.T) {
	cases := []struct {
		name   string
		change func(form url.Values, other apptest.OAuthClient)
		status int
		error  string
	}{
		{name: "other redirect", change: func(f url.Values, _ apptest.OAuthClient) { f.Set("redirect_uri", "https://claude.ai/other") }, status: 400, error: "invalid_grant"},
		{name: "no redirect", change: func(f url.Values, _ apptest.OAuthClient) { f.Del("redirect_uri") }, status: 400, error: "invalid_grant"},
		{name: "wrong verifier", change: func(f url.Values, _ apptest.OAuthClient) { f.Set("code_verifier", apptest.NewPKCE().Verifier) }, status: 400, error: "invalid_grant"},
		{name: "no verifier", change: func(f url.Values, _ apptest.OAuthClient) { f.Del("code_verifier") }, status: 400, error: "invalid_grant"},
		{name: "other client", change: func(f url.Values, o apptest.OAuthClient) { f.Set("client_id", o.ID) }, status: 400, error: "invalid_grant"},
		{name: "unknown client", change: func(f url.Values, _ apptest.OAuthClient) { f.Set("client_id", "ghost") }, status: 401, error: "invalid_client"},
		{name: "no client", change: func(f url.Values, _ apptest.OAuthClient) { f.Del("client_id") }, status: 401, error: "invalid_client"},
		{name: "unknown code", change: func(f url.Values, _ apptest.OAuthClient) { f.Set("code", "not-a-code") }, status: 400, error: "invalid_grant"},
		{name: "other resource", change: func(f url.Values, _ apptest.OAuthClient) { f.Set("resource", "https://other.example/mcp") }, status: 400, error: "invalid_target"},
		{name: "implicit grant", change: func(f url.Values, _ apptest.OAuthClient) { f.Set("grant_type", "password") }, status: 400, error: "unsupported_grant_type"},
		{name: "no grant type", change: func(f url.Values, _ apptest.OAuthClient) { f.Del("grant_type") }, status: 400, error: "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			login := h.NewLogin(t)
			other := h.RegisterOAuthClient(t, map[string]any{"client_name": "Other"})
			code := h.Authorize(t, login, "claude")
			form := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {login.Client.RedirectURI},
				"client_id":     {login.Client.ID},
				"code_verifier": {login.PKCE.Verifier},
				"resource":      {apptest.PublicURL + "/mcp"},
			}
			tc.change(form, other)

			resp, body := h.TokenRequest(t, form)

			requireTokenError(t, resp, body, tc.status, tc.error)
			if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
				t.Fatalf("a refused exchange created an access: %q", listed.Stdout)
			}
		})
	}
}

func TestTokenResponsesAreNotCached(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	code := h.Authorize(t, login, "claude")

	resp, _ := h.ExchangeCode(t, login, code)

	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("headers %v", resp.Header)
	}
}

func TestAccessTokenLastsOneHour(t *testing.T) {
	h := apptest.Start(t)
	_, tokens := h.OAuthLogin(t, "claude")

	h.Clock.Advance(59*time.Minute + 59*time.Second)
	requireMCPStatus(t, h, tokens.Access, http.StatusOK)
	h.Clock.Advance(time.Second)
	requireMCPStatus(t, h, tokens.Access, http.StatusUnauthorized)
}

func TestRefreshRotatesTheRefreshToken(t *testing.T) {
	h := apptest.Start(t)
	login, tokens := h.OAuthLogin(t, "claude")
	h.Clock.Advance(2 * time.Hour)

	resp, body := h.Refresh(t, login.Client, tokens.Refresh)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %v", resp.StatusCode, body)
	}
	next := apptest.TokensFrom(body)
	if next.Refresh == "" || next.Refresh == tokens.Refresh || next.Access == tokens.Access || body["expires_in"] != float64(3600) {
		t.Fatalf("refresh answered %v", body)
	}
	requireMCPStatus(t, h, next.Access, http.StatusOK)
	again, againBody := h.Refresh(t, login.Client, next.Refresh)
	if again.StatusCode != http.StatusOK {
		t.Fatalf("the rotated refresh token failed: %d %v", again.StatusCode, againBody)
	}
}

func TestReusingAnOldRefreshTokenRevokesTheAccess(t *testing.T) {
	h := apptest.Start(t)
	login, tokens := h.OAuthLogin(t, "claude")
	_, body := h.Refresh(t, login.Client, tokens.Refresh)
	next := apptest.TokensFrom(body)

	resp, reused := h.Refresh(t, login.Client, tokens.Refresh)

	requireTokenError(t, resp, reused, http.StatusBadRequest, "invalid_grant")
	requireMCPStatus(t, h, next.Access, http.StatusUnauthorized)
	after, afterBody := h.Refresh(t, login.Client, next.Refresh)
	requireTokenError(t, after, afterBody, http.StatusBadRequest, "invalid_grant")
	if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
		t.Fatalf("access list %q", listed.Stdout)
	}
	events := linesWithMessage(h.AuditLines(t), "oauth")
	last := events[len(events)-1]
	assertFields(t, last, map[string]string{"event": "token_refresh", "result": "denied", "reason": "refresh_reused", "grant": "claude"})
}

func TestRefreshTokenLastsNinetyDaysFromItsIssue(t *testing.T) {
	h := apptest.Start(t)
	login, tokens := h.OAuthLogin(t, "claude")
	h.Clock.Advance(89 * 24 * time.Hour)
	_, body := h.Refresh(t, login.Client, tokens.Refresh)
	next := apptest.TokensFrom(body)
	if next.Refresh == "" {
		t.Fatalf("refresh after 89 days failed: %v", body)
	}

	h.Clock.Advance(90 * 24 * time.Hour)
	resp, expired := h.Refresh(t, login.Client, next.Refresh)

	requireTokenError(t, resp, expired, http.StatusBadRequest, "invalid_grant")
}

func TestRefreshBelongsToItsClient(t *testing.T) {
	h := apptest.Start(t)
	login, tokens := h.OAuthLogin(t, "claude")
	other := h.RegisterOAuthClient(t, map[string]any{"client_name": "Other"})

	resp, body := h.Refresh(t, other, tokens.Refresh)

	requireTokenError(t, resp, body, http.StatusBadRequest, "invalid_grant")
	if again, againBody := h.Refresh(t, login.Client, tokens.Refresh); again.StatusCode != http.StatusOK {
		t.Fatalf("the rightful client cannot refresh after a refused attempt: %v", againBody)
	}
}

func TestAccessRevokeEndsAnOAuthAccess(t *testing.T) {
	h := apptest.Start(t)
	login, tokens := h.OAuthLogin(t, "claude")
	session := h.Connect(t, tokens.Access)

	revoked := h.CLI(t, "access", "revoke", "claude")

	if revoked.Code != 0 {
		t.Fatalf("exit %d, stderr %q", revoked.Code, revoked.Stderr)
	}
	if _, err := session.ListTools(t.Context(), nil); err == nil {
		t.Fatal("the open session still answers after the revoke")
	}
	requireMCPStatus(t, h, tokens.Access, http.StatusUnauthorized)
	resp, body := h.Refresh(t, login.Client, tokens.Refresh)
	requireTokenError(t, resp, body, http.StatusBadRequest, "invalid_grant")
}

func TestRevokeEndpointEndsTheAccess(t *testing.T) {
	for _, kind := range []string{"refresh", "access"} {
		t.Run(kind, func(t *testing.T) {
			h := apptest.Start(t)
			login, tokens := h.OAuthLogin(t, "claude")
			token := tokens.Refresh
			if kind == "access" {
				token = tokens.Access
			}

			resp := h.PostForm(t, "/oauth/revoke", url.Values{"token": {token}, "client_id": {login.Client.ID}})

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, apptest.ReadBody(t, resp))
			}
			requireMCPStatus(t, h, tokens.Access, http.StatusUnauthorized)
			refresh, body := h.Refresh(t, login.Client, tokens.Refresh)
			requireTokenError(t, refresh, body, http.StatusBadRequest, "invalid_grant")
			if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
				t.Fatalf("access list %q", listed.Stdout)
			}
		})
	}
}

func TestRevokeEndpointIgnoresUnknownAndForeignTokens(t *testing.T) {
	h := apptest.Start(t)
	_, tokens := h.OAuthLogin(t, "claude")
	other := h.RegisterOAuthClient(t, map[string]any{"client_name": "Other"})
	static := h.StaticToken(t, "bot", nil, false)

	for _, form := range []url.Values{
		{"token": {"wmcp_rt_unknown"}, "client_id": {other.ID}},
		{"token": {tokens.Refresh}, "client_id": {other.ID}},
		{"token": {static}, "client_id": {other.ID}},
	} {
		if resp := h.PostForm(t, "/oauth/revoke", form); resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", resp.StatusCode)
		}
	}

	requireMCPStatus(t, h, tokens.Access, http.StatusOK)
	requireMCPStatus(t, h, static, http.StatusOK)
}

func TestConfidentialClientMustAuthenticate(t *testing.T) {
	cases := []struct {
		name   string
		method string
		auth   func(form url.Values, client apptest.OAuthClient) []apptest.RequestOption
		status int
	}{
		{name: "post secret", method: "client_secret_post", status: http.StatusOK, auth: func(f url.Values, c apptest.OAuthClient) []apptest.RequestOption {
			f.Set("client_secret", c.Secret)
			return nil
		}},
		{name: "basic secret", method: "client_secret_basic", status: http.StatusOK, auth: func(f url.Values, c apptest.OAuthClient) []apptest.RequestOption {
			f.Del("client_id")
			return []apptest.RequestOption{apptest.WithBasicAuth(c.ID, c.Secret)}
		}},
		{name: "no secret", method: "client_secret_post", status: http.StatusUnauthorized, auth: func(url.Values, apptest.OAuthClient) []apptest.RequestOption { return nil }},
		{name: "wrong secret", method: "client_secret_basic", status: http.StatusUnauthorized, auth: func(f url.Values, c apptest.OAuthClient) []apptest.RequestOption {
			return []apptest.RequestOption{apptest.WithBasicAuth(c.ID, "wmcp_cs_wrong")}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			client := h.RegisterOAuthClient(t, map[string]any{"client_name": "ChatGPT", "token_endpoint_auth_method": tc.method})
			login := h.LoginFor(client)
			code := h.Authorize(t, login, "chatgpt")
			form := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {client.RedirectURI},
				"client_id":     {client.ID},
				"code_verifier": {login.PKCE.Verifier},
			}
			opts := tc.auth(form, client)

			resp, body := h.TokenRequest(t, form, opts...)

			if resp.StatusCode != tc.status {
				t.Fatalf("status %d, want %d: %v", resp.StatusCode, tc.status, body)
			}
			if tc.status == http.StatusUnauthorized && body["error"] != "invalid_client" {
				t.Fatalf("error %v, want invalid_client", body)
			}
		})
	}
}

func TestLoopbackRedirectMayChangeItsPort(t *testing.T) {
	h := apptest.Start(t)
	client := h.RegisterOAuthClient(t, map[string]any{"client_name": "Claude Code", "redirect_uris": []string{"http://localhost:3118/callback"}})
	client.RedirectURI = "http://localhost:51234/callback"
	login := h.LoginFor(client)

	code := h.Authorize(t, login, "claude-code")
	resp, body := h.ExchangeCode(t, login, code)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %v", resp.StatusCode, body)
	}
	login.Request.Set("redirect_uri", "http://localhost:51234/other")
	if form := h.OpenAuthorizePage(t, login.Request); form.StatusCode != http.StatusBadRequest {
		t.Fatalf("a loopback redirect with another path got status %d", form.StatusCode)
	}
	login.Request.Set("redirect_uri", "http://127.0.0.1:51234/callback")
	if form := h.OpenAuthorizePage(t, login.Request); form.StatusCode != http.StatusBadRequest {
		t.Fatalf("a loopback redirect with another host got status %d", form.StatusCode)
	}
}

func TestOAuthEventsAreAuditedWithoutSecrets(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	code := h.Authorize(t, login, "claude")
	_, body := h.ExchangeCode(t, login, code)
	tokens := apptest.TokensFrom(body)
	_, refreshed := h.Refresh(t, login.Client, tokens.Refresh)
	next := apptest.TokensFrom(refreshed)
	h.PostForm(t, "/oauth/revoke", url.Values{"token": {next.Refresh}, "client_id": {login.Client.ID}})

	var got []string
	for _, line := range linesWithMessage(h.AuditLines(t), "oauth") {
		got = append(got, line["event"].(string)+":"+line["result"].(string)+":"+line["grant"].(string))
	}
	want := "client_register:ok:,authorize:ok:claude,token_issue:ok:claude,token_refresh:ok:claude,token_revoke:ok:claude"
	if strings.Join(got, ",") != want {
		t.Fatalf("oauth audit %v, want %s", got, want)
	}
	for _, secret := range []string{code, tokens.Access, tokens.Refresh, next.Access, next.Refresh, login.PKCE.Verifier, apptest.OwnerPassword} {
		if strings.Contains(h.AuditRaw(), secret) {
			t.Fatalf("the audit log contains a secret: %s", h.AuditRaw())
		}
	}
}

func TestExchangeFailsWhenTheNameWasTakenMeanwhile(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	code := h.Authorize(t, login, "claude")
	static := h.StaticToken(t, "claude", nil, false)

	resp, body := h.ExchangeCode(t, login, code)

	requireTokenError(t, resp, body, http.StatusBadRequest, "invalid_grant")
	requireMCPStatus(t, h, static, http.StatusOK)
	again, againBody := h.ExchangeCode(t, login, code)
	requireTokenError(t, again, againBody, http.StatusBadRequest, "invalid_grant")
	requireMCPStatus(t, h, static, http.StatusOK)
}

func TestExchangeFailsWhenAnAccountWasRemovedMeanwhile(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	login := h.NewLogin(t)
	code := h.Authorize(t, login, "claude", "work")
	if removed := h.CLI(t, "accounts", "remove", "work", "--yes"); removed.Code != 0 {
		t.Fatalf("remove: exit %d, stderr %q", removed.Code, removed.Stderr)
	}

	resp, body := h.ExchangeCode(t, login, code)

	requireTokenError(t, resp, body, http.StatusBadRequest, "invalid_grant")
	if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
		t.Fatalf("access list %q", listed.Stdout)
	}
}
