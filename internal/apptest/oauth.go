package apptest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

const ClaudeCallback = "https://claude.ai/api/mcp/auth_callback"

var (
	inputTag  = regexp.MustCompile(`<input\s[^>]*>`)
	attribute = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
)

type OAuthClient struct {
	ID          string
	Secret      string
	RedirectURI string
}

type PKCE struct {
	Verifier  string
	Challenge string
}

func NewPKCE() PKCE {
	buf := make([]byte, 32)
	rand.Read(buf)
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return PKCE{Verifier: verifier, Challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}

type AuthorizeForm struct {
	Hidden     url.Values
	Fields     map[string]string
	Accounts   []string
	Checked    []string
	Page       string
	StatusCode int
}

type Login struct {
	Client  OAuthClient
	PKCE    PKCE
	State   string
	Request url.Values
}

type Tokens struct {
	Access  string
	Refresh string
	Body    map[string]any
}

func (h *Harness) RegisterOAuthClient(t testing.TB, metadata map[string]any) OAuthClient {
	t.Helper()
	if _, ok := metadata["redirect_uris"]; !ok {
		metadata["redirect_uris"] = []string{ClaudeCallback}
	}
	resp := h.PostJSON(t, "/oauth/register", metadata)
	body := ReadJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register the client: status %d: %v", resp.StatusCode, body)
	}
	secret, _ := body["client_secret"].(string)
	uris := body["redirect_uris"].([]any)
	return OAuthClient{ID: body["client_id"].(string), Secret: secret, RedirectURI: uris[0].(string)}
}

func (h *Harness) NewLogin(t testing.TB) *Login {
	t.Helper()
	client := h.RegisterOAuthClient(t, map[string]any{"client_name": "Claude", "token_endpoint_auth_method": "none"})
	return h.LoginFor(client)
}

func (h *Harness) LoginFor(client OAuthClient) *Login {
	pkce := NewPKCE()
	state := rand.Text()
	return &Login{
		Client: client,
		PKCE:   pkce,
		State:  state,
		Request: url.Values{
			"response_type":         {"code"},
			"client_id":             {client.ID},
			"redirect_uri":          {client.RedirectURI},
			"code_challenge":        {pkce.Challenge},
			"code_challenge_method": {"S256"},
			"state":                 {state},
			"scope":                 {"whatsapp"},
			"resource":              {PublicURL + "/mcp"},
		},
	}
}

func (h *Harness) OpenAuthorizePage(t testing.TB, query url.Values, opts ...RequestOption) AuthorizeForm {
	t.Helper()
	resp := h.Get(t, "/oauth/authorize?"+query.Encode(), opts...)
	return ParseAuthorizeForm(resp.StatusCode, ReadBody(t, resp))
}

func ParseAuthorizeForm(status int, page string) AuthorizeForm {
	form := AuthorizeForm{Hidden: url.Values{}, Fields: map[string]string{}, Page: page, StatusCode: status}
	for _, tag := range inputTag.FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, match := range attribute.FindAllStringSubmatch(tag, -1) {
			attrs[match[1]] = html.UnescapeString(match[2])
		}
		switch attrs["type"] {
		case "hidden":
			form.Hidden.Add(attrs["name"], attrs["value"])
		case "checkbox":
			form.Accounts = append(form.Accounts, attrs["value"])
			if regexp.MustCompile(`\schecked`).MatchString(tag) {
				form.Checked = append(form.Checked, attrs["value"])
			}
		default:
			form.Fields[attrs["name"]] = attrs["value"]
		}
	}
	return form
}

func (f AuthorizeForm) Submission(password, accessName string, accounts ...string) url.Values {
	values := url.Values{}
	for key, list := range f.Hidden {
		values[key] = append([]string(nil), list...)
	}
	values.Set("password", password)
	values.Set("access_name", accessName)
	values.Set("decision", "allow")
	values["accounts"] = accounts
	return values
}

func (h *Harness) SubmitAuthorize(t testing.TB, values url.Values, opts ...RequestOption) *http.Response {
	t.Helper()
	return h.PostForm(t, "/oauth/authorize", values, opts...)
}

func RedirectParams(t testing.TB, resp *http.Response, redirectURI string) url.Values {
	t.Helper()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status %d, want 303: %s", resp.StatusCode, ReadBody(t, resp))
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	base := *location
	base.RawQuery = ""
	if base.String() != redirectURI {
		t.Fatalf("redirected to %s, want %s", base.String(), redirectURI)
	}
	return location.Query()
}

func (h *Harness) Authorize(t testing.TB, login *Login, accessName string, accounts ...string) string {
	t.Helper()
	form := h.OpenAuthorizePage(t, login.Request)
	if form.StatusCode != http.StatusOK {
		t.Fatalf("authorize page: status %d: %s", form.StatusCode, form.Page)
	}
	resp := h.SubmitAuthorize(t, form.Submission(OwnerPassword, accessName, accounts...))
	params := RedirectParams(t, resp, login.Client.RedirectURI)
	if params.Get("state") != login.State || params.Get("code") == "" {
		t.Fatalf("redirect parameters %v", params)
	}
	return params.Get("code")
}

func (h *Harness) TokenRequest(t testing.TB, form url.Values, opts ...RequestOption) (*http.Response, map[string]any) {
	t.Helper()
	resp := h.PostForm(t, "/oauth/token", form, opts...)
	return resp, ReadJSON(t, resp)
}

func (h *Harness) ExchangeCode(t testing.TB, login *Login, code string) (*http.Response, map[string]any) {
	t.Helper()
	return h.TokenRequest(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {login.Client.RedirectURI},
		"client_id":     {login.Client.ID},
		"code_verifier": {login.PKCE.Verifier},
		"resource":      {PublicURL + "/mcp"},
	})
}

func (h *Harness) Refresh(t testing.TB, client OAuthClient, refreshToken string) (*http.Response, map[string]any) {
	t.Helper()
	return h.TokenRequest(t, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {client.ID},
	})
}

func (h *Harness) OAuthLogin(t testing.TB, accessName string, accounts ...string) (*Login, Tokens) {
	t.Helper()
	login := h.NewLogin(t)
	code := h.Authorize(t, login, accessName, accounts...)
	resp, body := h.ExchangeCode(t, login, code)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exchange the code: status %d: %v", resp.StatusCode, body)
	}
	return login, TokensFrom(body)
}

func TokensFrom(body map[string]any) Tokens {
	access, _ := body["access_token"].(string)
	refresh, _ := body["refresh_token"].(string)
	return Tokens{Access: access, Refresh: refresh, Body: body}
}
