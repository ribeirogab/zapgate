package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func TestAuthorizePageAsksForPasswordNameAndAccounts(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	h.AddAccount(t, "personal", personalPhone, "Alex")
	login := h.NewLogin(t)

	resp := h.Get(t, "/oauth/authorize?"+login.Request.Encode())
	page := apptest.ReadBody(t, resp)

	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for header, want := range map[string]string{
		"Cache-Control":           "no-store",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Fatalf("%s %q, want %q", header, got, want)
		}
	}
	form := h.OpenAuthorizePage(t, login.Request)
	if strings.Join(form.Accounts, ",") != "personal,work" || len(form.Checked) != 0 {
		t.Fatalf("checkboxes %v, checked %v; want one unchecked box per account", form.Accounts, form.Checked)
	}
	if _, ok := form.Fields["password"]; !ok {
		t.Fatalf("no password field in %v", form.Fields)
	}
	if form.Fields["access_name"] != "claude" {
		t.Fatalf("access name field %q, want the client name as a suggestion", form.Fields["access_name"])
	}
	for _, key := range []string{"client_id", "redirect_uri", "code_challenge", "code_challenge_method", "state", "response_type", "scope", "resource"} {
		if form.Hidden.Get(key) != login.Request.Get(key) {
			t.Fatalf("hidden %s %q, want %q", key, form.Hidden.Get(key), login.Request.Get(key))
		}
	}
	for _, text := range []string{`<html lang="en">`, `name="viewport"`, "Owner password", "Access name", "Accounts", "Claude", "claude.ai", ">Authorize<", ">Deny<"} {
		if !strings.Contains(page, text) {
			t.Fatalf("the page has no %q:\n%s", text, page)
		}
	}
	for _, external := range []string{"<script", "<link", "src=", "@import", "url("} {
		if strings.Contains(page, external) {
			t.Fatalf("the page loads an external asset (%q)", external)
		}
	}
}

func TestAuthorizePageWarnsAboutALoopbackRedirect(t *testing.T) {
	h := apptest.Start(t)
	client := h.RegisterOAuthClient(t, map[string]any{"client_name": "Claude Code (whatsapp)", "redirect_uris": []string{"http://localhost:51234/callback"}})

	form := h.OpenAuthorizePage(t, h.LoginFor(client).Request)

	if form.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", form.StatusCode, form.Page)
	}
	if form.Fields["access_name"] != "claude-code-whatsapp" {
		t.Fatalf("access name suggestion %q", form.Fields["access_name"])
	}
	if !strings.Contains(form.Page, "localhost") || !strings.Contains(form.Page, "started the connection on this computer") {
		t.Fatalf("the page does not warn about the local redirect:\n%s", form.Page)
	}
	if !strings.Contains(form.Page, "No account is linked yet.") {
		t.Fatalf("the page does not say there is no account:\n%s", form.Page)
	}
}

func TestAuthorizePageEscapesTheClientName(t *testing.T) {
	h := apptest.Start(t)
	client := h.RegisterOAuthClient(t, map[string]any{"client_name": `<img src=x onerror=alert(1)>`})

	form := h.OpenAuthorizePage(t, h.LoginFor(client).Request)

	if strings.Contains(form.Page, "<img") || !strings.Contains(form.Page, "&lt;img") {
		t.Fatalf("the client name is not escaped:\n%s", form.Page)
	}
}

func TestAuthorizeRefusesARequestItCannotTrust(t *testing.T) {
	cases := []struct {
		name   string
		change func(url.Values)
	}{
		{name: "unknown client", change: func(q url.Values) { q.Set("client_id", "ghost") }},
		{name: "no client", change: func(q url.Values) { q.Del("client_id") }},
		{name: "other redirect", change: func(q url.Values) { q.Set("redirect_uri", "https://evil.example/callback") }},
		{name: "redirect with a longer path", change: func(q url.Values) { q.Set("redirect_uri", apptest.ClaudeCallback+"/x") }},
		{name: "no redirect", change: func(q url.Values) { q.Del("redirect_uri") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			login := h.NewLogin(t)
			tc.change(login.Request)

			resp := h.Get(t, "/oauth/authorize?"+login.Request.Encode())

			if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("Location") != "" {
				t.Fatalf("status %d, location %q; want 400 without a redirect", resp.StatusCode, resp.Header.Get("Location"))
			}
			page := apptest.ReadBody(t, resp)
			if strings.Contains(page, `name="password"`) {
				t.Fatal("the error page asks for the password")
			}
			if !strings.Contains(page, "Invalid authorization request") || !strings.Contains(page, "Go back to the app") {
				t.Fatalf("the error page does not explain the refusal:\n%s", page)
			}
		})
	}
}

func TestAuthorizeSendsProtocolErrorsBackToTheClient(t *testing.T) {
	cases := []struct {
		name   string
		change func(url.Values)
		error  string
	}{
		{name: "plain PKCE", change: func(q url.Values) { q.Set("code_challenge_method", "plain") }, error: "invalid_request"},
		{name: "no PKCE method", change: func(q url.Values) { q.Del("code_challenge_method") }, error: "invalid_request"},
		{name: "no PKCE", change: func(q url.Values) { q.Del("code_challenge") }, error: "invalid_request"},
		{name: "short challenge", change: func(q url.Values) { q.Set("code_challenge", "abc") }, error: "invalid_request"},
		{name: "implicit flow", change: func(q url.Values) { q.Set("response_type", "token") }, error: "unsupported_response_type"},
		{name: "other resource", change: func(q url.Values) { q.Set("resource", "https://other.example/mcp") }, error: "invalid_target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			login := h.NewLogin(t)
			tc.change(login.Request)

			resp := h.Get(t, "/oauth/authorize?"+login.Request.Encode())

			params := apptest.RedirectParams(t, resp, apptest.ClaudeCallback)
			if params.Get("error") != tc.error || params.Get("state") != login.State || params.Get("iss") != apptest.PublicURL || params.Get("code") != "" {
				t.Fatalf("redirect parameters %v, want error %s with state and iss", params, tc.error)
			}
		})
	}
}

func TestAuthorizeAcceptsTheResourceWithoutPath(t *testing.T) {
	for _, resource := range []string{"", apptest.PublicURL, apptest.PublicURL + "/"} {
		t.Run(resource, func(t *testing.T) {
			h := apptest.Start(t)
			login := h.NewLogin(t)
			login.Request.Set("resource", resource)
			if resource == "" {
				login.Request.Del("resource")
			}

			form := h.OpenAuthorizePage(t, login.Request)

			if form.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", form.StatusCode, form.Page)
			}
		})
	}
}

func TestOwnerCanDeclineTheAuthorization(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	form := h.OpenAuthorizePage(t, login.Request)
	values := form.Submission("", "")
	values.Set("decision", "deny")

	resp := h.SubmitAuthorize(t, values)

	params := apptest.RedirectParams(t, resp, apptest.ClaudeCallback)
	if params.Get("error") != "access_denied" || params.Get("state") != login.State || params.Get("iss") != apptest.PublicURL {
		t.Fatalf("redirect parameters %v", params)
	}
	if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
		t.Fatalf("access list %q", listed.Stdout)
	}
}

func TestWrongPasswordDoesNotAuthorize(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	login := h.NewLogin(t)
	form := h.OpenAuthorizePage(t, login.Request)

	resp := h.SubmitAuthorize(t, form.Submission("wrong-password", "claude", "work"))

	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Location") != "" {
		t.Fatalf("status %d, location %q; want 401 and no redirect", resp.StatusCode, resp.Header.Get("Location"))
	}
	again := apptest.ParseAuthorizeForm(resp.StatusCode, apptest.ReadBody(t, resp))
	if !strings.Contains(again.Page, "Wrong password.") {
		t.Fatalf("the page does not say the password is wrong:\n%s", again.Page)
	}
	if again.Fields["password"] != "" || strings.Contains(again.Page, "wrong-password") {
		t.Fatal("the page echoes the password")
	}
	if again.Fields["access_name"] != "claude" || strings.Join(again.Checked, ",") != "work" || again.Hidden.Get("state") != login.State {
		t.Fatalf("the page lost the choices: name %q, checked %v, hidden %v", again.Fields["access_name"], again.Checked, again.Hidden)
	}
	if listed := h.CLI(t, "access", "list"); listed.Stdout != "no access yet\n" {
		t.Fatalf("access list %q", listed.Stdout)
	}
}

func TestAuthorizeRejectsABadChoice(t *testing.T) {
	cases := []struct {
		name     string
		access   string
		accounts []string
		message  string
	}{
		{name: "no account", access: "claude", message: "Select at least one account."},
		{name: "unknown account", access: "claude", accounts: []string{"sales"}, message: "The account sales does not exist."},
		{name: "bad name", access: "Claude AI", accounts: []string{"work"}, message: "Invalid name"},
		{name: "name in use", access: "bot", accounts: []string{"work"}, message: "The name bot is already used by another access."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			h.AddAccount(t, "work", workPhone, "Alex")
			h.StaticToken(t, "bot", []string{"work"}, false)
			form := h.OpenAuthorizePage(t, h.NewLogin(t).Request)

			resp := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, tc.access, tc.accounts...))

			page := apptest.ReadBody(t, resp)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, tc.message) {
				t.Fatalf("status %d, want 400 with %q:\n%s", resp.StatusCode, tc.message, page)
			}
		})
	}
}

func TestAuthorizeRefusesAnOversizedForm(t *testing.T) {
	h := apptest.Start(t)
	form := h.OpenAuthorizePage(t, h.NewLogin(t).Request)
	values := form.Submission(apptest.OwnerPassword, "claude")
	values.Set("padding", strings.Repeat("a", 70<<10))

	resp := h.SubmitAuthorize(t, values)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}
