package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const nativeCallback = "http://localhost:38111/callback"

func browserApproving(t *testing.T, h *apptest.Harness, accessName string, accounts ...string) auth.AuthorizationCodeFetcher {
	return func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		authorizeURL, err := url.Parse(args.URL)
		if err != nil {
			return nil, err
		}
		if got := authorizeURL.Scheme + "://" + authorizeURL.Host + authorizeURL.Path; got != h.URL+"/oauth/authorize" {
			t.Errorf("the client opened %s, want the authorize page", got)
		}
		form := h.OpenAuthorizePage(t, authorizeURL.Query())
		resp := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, accessName, accounts...))
		params := apptest.RedirectParams(t, resp, nativeCallback)
		return &auth.AuthorizationResult{Code: params.Get("code"), State: params.Get("state"), Iss: params.Get("iss")}, nil
	}
}

func TestSDKClientConnectsWithOnlyTheURL(t *testing.T) {
	for _, path := range protocolPaths {
		t.Run(path.name, func(t *testing.T) {
			h := apptest.Start(t, apptest.WithPublicURLOfTheListener())
			h.AddAccount(t, "work", workPhone, "Alex")
			h.AddAccount(t, "personal", personalPhone, "Alex")
			handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
				DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
					ClientName:              "Claude Code (whatsapp)",
					RedirectURIs:            []string{nativeCallback},
					GrantTypes:              []string{"authorization_code", "refresh_token"},
					ResponseTypes:           []string{"code"},
					TokenEndpointAuthMethod: "none",
				}},
				AuthorizationCodeFetcher: browserApproving(t, h, "claude-code", "work"),
				RequestRefreshToken:      true,
			})
			if err != nil {
				t.Fatal(err)
			}
			client := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "1"}, nil)
			transport := &mcp.StreamableClientTransport{Endpoint: h.MCPURL(), OAuthHandler: handler, HTTPClient: &http.Client{}, MaxRetries: -1}

			session, err := client.Connect(context.Background(), transport, &mcp.ClientSessionOptions{ProtocolVersion: path.protocol})

			if err != nil {
				t.Fatalf("connect with only the URL: %v", err)
			}
			t.Cleanup(func() { session.Close() })
			if got := accountNames(t, session); strings.Join(got, ",") != "work" {
				t.Fatalf("the client reaches %v, want work", got)
			}
			if rows := tableRows(t, h.CLI(t, "access", "list").Stdout); len(rows) != 2 || !strings.HasPrefix(rows[1], "claude-code oauth work read-write") {
				t.Fatalf("access list %q", rows)
			}
		})
	}
}
