package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func canonicalJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestProtectedResourceMetadataNamesTheServerAsItsOwnAuthorizationServer(t *testing.T) {
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		t.Run(path, func(t *testing.T) {
			h := apptest.Start(t)

			resp := h.Get(t, path)

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			want := `{"authorization_servers":["https://wpp.example.test"],"bearer_methods_supported":["header"],"resource":"https://wpp.example.test/mcp","resource_name":"zapgate","scopes_supported":["whatsapp"]}`
			if got := canonicalJSON(t, apptest.ReadJSON(t, resp)); got != want {
				t.Fatalf("metadata\n got %s\nwant %s", got, want)
			}
		})
	}
}

func TestAuthorizationServerMetadataOffersOnlyS256(t *testing.T) {
	h := apptest.Start(t)

	resp := h.Get(t, "/.well-known/oauth-authorization-server")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin %q", got)
	}
	want := `{` +
		`"authorization_endpoint":"https://wpp.example.test/oauth/authorize",` +
		`"authorization_response_iss_parameter_supported":true,` +
		`"code_challenge_methods_supported":["S256"],` +
		`"grant_types_supported":["authorization_code","refresh_token"],` +
		`"issuer":"https://wpp.example.test",` +
		`"registration_endpoint":"https://wpp.example.test/oauth/register",` +
		`"response_modes_supported":["query"],` +
		`"response_types_supported":["code"],` +
		`"revocation_endpoint":"https://wpp.example.test/oauth/revoke",` +
		`"revocation_endpoint_auth_methods_supported":["none","client_secret_post","client_secret_basic"],` +
		`"scopes_supported":["whatsapp"],` +
		`"token_endpoint":"https://wpp.example.test/oauth/token",` +
		`"token_endpoint_auth_methods_supported":["none","client_secret_post","client_secret_basic"]` +
		`}`
	if got := canonicalJSON(t, apptest.ReadJSON(t, resp)); got != want {
		t.Fatalf("metadata\n got %s\nwant %s", got, want)
	}
}

func TestMetadataAnswersCORSPreflight(t *testing.T) {
	h := apptest.Start(t)
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server", "/oauth/register", "/oauth/token", "/oauth/revoke"} {
		t.Run(path, func(t *testing.T) {
			resp := h.Options(t, path)

			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("status %d, want 204", resp.StatusCode)
			}
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("Access-Control-Allow-Origin %q", got)
			}
		})
	}
}
