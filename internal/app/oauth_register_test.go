package app_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const claudeCallback = "https://claude.ai/api/mcp/auth_callback"

func TestDynamicRegistrationOfAPublicClientAnswersCreated(t *testing.T) {
	h := apptest.Start(t)

	resp := h.PostJSON(t, "/oauth/register", map[string]any{
		"client_name":                "Claude",
		"redirect_uris":              []string{claudeCallback},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %s", resp.StatusCode, apptest.ReadBody(t, resp))
	}
	body := apptest.ReadJSON(t, resp)
	clientID, _ := body["client_id"].(string)
	if len(clientID) < 20 {
		t.Fatalf("client_id %q is too short to be random", clientID)
	}
	delete(body, "client_id")
	want := `{"client_id_issued_at":1791892800,"client_name":"Claude","grant_types":["authorization_code","refresh_token"],"redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	if got := canonicalJSON(t, body); got != want {
		t.Fatalf("registration\n got %s\nwant %s", got, want)
	}
}

func TestDynamicRegistrationWithoutAuthMethodMakesAPublicClient(t *testing.T) {
	h := apptest.Start(t)

	resp := h.PostJSON(t, "/oauth/register", map[string]any{"redirect_uris": []string{"http://localhost:33418/callback"}})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %s", resp.StatusCode, apptest.ReadBody(t, resp))
	}
	body := apptest.ReadJSON(t, resp)
	if body["token_endpoint_auth_method"] != "none" || body["client_secret"] != nil {
		t.Fatalf("registration %v, want a public client", body)
	}
}

func TestDynamicRegistrationOfAConfidentialClientReturnsASecret(t *testing.T) {
	for _, method := range []string{"client_secret_post", "client_secret_basic"} {
		t.Run(method, func(t *testing.T) {
			h := apptest.Start(t)

			resp := h.PostJSON(t, "/oauth/register", map[string]any{
				"redirect_uris":              []string{"https://chatgpt.com/connector_platform_oauth_redirect"},
				"token_endpoint_auth_method": method,
			})

			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("status %d: %s", resp.StatusCode, apptest.ReadBody(t, resp))
			}
			body := apptest.ReadJSON(t, resp)
			secret, _ := body["client_secret"].(string)
			if len(secret) < 40 || body["token_endpoint_auth_method"] != method || body["client_secret_expires_at"] != float64(0) {
				t.Fatalf("registration %v, want a secret that never expires", body)
			}
			if strings.Contains(h.AuditRaw(), secret) {
				t.Fatal("the audit log contains the client secret")
			}
		})
	}
}

func TestDynamicRegistrationAcceptsNativeRedirects(t *testing.T) {
	for _, uri := range []string{
		"http://127.0.0.1:43210/callback",
		"http://[::1]/callback",
		"http://localhost/callback",
		"cursor://anysphere.cursor-retrieval/oauth/user-mcp/callback",
	} {
		t.Run(uri, func(t *testing.T) {
			h := apptest.Start(t)

			resp := h.PostJSON(t, "/oauth/register", map[string]any{"redirect_uris": []string{uri}})

			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("status %d: %s", resp.StatusCode, apptest.ReadBody(t, resp))
			}
		})
	}
}

func TestDynamicRegistrationRejectsBadMetadata(t *testing.T) {
	cases := []struct {
		name  string
		body  any
		error string
	}{
		{name: "no redirect", body: map[string]any{"client_name": "x"}, error: "invalid_redirect_uri"},
		{name: "plain http", body: map[string]any{"redirect_uris": []string{"http://evil.example/callback"}}, error: "invalid_redirect_uri"},
		{name: "fragment", body: map[string]any{"redirect_uris": []string{"https://claude.ai/cb#x"}}, error: "invalid_redirect_uri"},
		{name: "javascript", body: map[string]any{"redirect_uris": []string{"javascript:alert(1)"}}, error: "invalid_redirect_uri"},
		{name: "relative", body: map[string]any{"redirect_uris": []string{"/callback"}}, error: "invalid_redirect_uri"},
		{name: "jwt auth", body: map[string]any{"redirect_uris": []string{claudeCallback}, "token_endpoint_auth_method": "private_key_jwt"}, error: "invalid_client_metadata"},
		{name: "client credentials", body: map[string]any{"redirect_uris": []string{claudeCallback}, "grant_types": []string{"client_credentials"}}, error: "invalid_client_metadata"},
		{name: "implicit", body: map[string]any{"redirect_uris": []string{claudeCallback}, "response_types": []string{"token"}}, error: "invalid_client_metadata"},
		{name: "not json", body: "redirect_uris", error: "invalid_client_metadata"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)

			resp := h.PostJSON(t, "/oauth/register", tc.body)

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", resp.StatusCode)
			}
			if body := apptest.ReadJSON(t, resp); body["error"] != tc.error || body["error_description"] == "" {
				t.Fatalf("error %v, want %s with a description", body, tc.error)
			}
		})
	}
}

func TestDynamicRegistrationIsAudited(t *testing.T) {
	h := apptest.Start(t)

	resp := h.PostJSON(t, "/oauth/register", map[string]any{"client_name": "Claude", "redirect_uris": []string{claudeCallback}}, apptest.FromIP("198.51.100.4"))

	clientID := apptest.ReadJSON(t, resp)["client_id"].(string)
	events := linesWithMessage(h.AuditLines(t), "oauth")
	if len(events) != 1 {
		t.Fatalf("audit has %d oauth lines, want 1: %s", len(events), h.AuditRaw())
	}
	assertFields(t, events[0], map[string]string{"event": "client_register", "result": "ok", "client_id": clientID, "client": "Claude", "ip": "198.51.100.4"})
}

func TestRegistrationForgetsClientsThatNeverLoggedInAfterADay(t *testing.T) {
	h := apptest.Start(t)
	abandoned := h.RegisterOAuthClient(t, map[string]any{"client_name": "Scanner"})
	login, tokens := h.OAuthLogin(t, "claude")
	h.Clock.Advance(time.Hour)
	recent := h.RegisterOAuthClient(t, map[string]any{"client_name": "Recent"})
	h.Clock.Advance(23*time.Hour + time.Minute)

	h.RegisterOAuthClient(t, map[string]any{"client_name": "Next"})

	gone := h.OpenAuthorizePage(t, h.LoginFor(abandoned).Request)
	if gone.StatusCode != http.StatusBadRequest || !strings.Contains(gone.Page, "This app is not registered on this server.") {
		t.Fatalf("a client without a login after 24 hours still opens the page: status %d", gone.StatusCode)
	}
	if page := h.OpenAuthorizePage(t, h.LoginFor(recent).Request); page.StatusCode != http.StatusOK {
		t.Fatalf("a client registered 23 hours ago lost its registration: status %d", page.StatusCode)
	}
	if resp, body := h.Refresh(t, login.Client, tokens.Refresh); resp.StatusCode != http.StatusOK {
		t.Fatalf("a client with an access lost its registration: status %d: %v", resp.StatusCode, body)
	}
}
