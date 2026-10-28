package apptest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type RequestOption func(*http.Request)

func FromIP(ip string) RequestOption {
	return func(r *http.Request) { r.Header.Set("CF-Connecting-IP", ip) }
}

func WithBearer(token string) RequestOption {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func WithBasicAuth(user, password string) RequestOption {
	return func(r *http.Request) { r.SetBasicAuth(url.QueryEscape(user), url.QueryEscape(password)) }
}

func (h *Harness) Get(t testing.TB, path string, opts ...RequestOption) *http.Response {
	t.Helper()
	return h.send(t, http.MethodGet, path, nil, "", opts)
}

func (h *Harness) Options(t testing.TB, path string, opts ...RequestOption) *http.Response {
	t.Helper()
	return h.send(t, http.MethodOptions, path, nil, "", opts)
}

func (h *Harness) PostForm(t testing.TB, path string, form url.Values, opts ...RequestOption) *http.Response {
	t.Helper()
	return h.send(t, http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", opts)
}

func (h *Harness) PostJSON(t testing.TB, path string, body any, opts ...RequestOption) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return h.send(t, http.MethodPost, path, bytes.NewReader(encoded), "application/json", opts)
}

func (h *Harness) Post(t testing.TB, path string, body io.Reader, contentType string, opts ...RequestOption) *http.Response {
	t.Helper()
	return h.send(t, http.MethodPost, path, body, contentType, opts)
}

func (h *Harness) send(t testing.TB, method, path string, body io.Reader, contentType string, opts []RequestOption) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, h.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("CF-Connecting-IP", DefaultIP)
	for _, opt := range opts {
		opt(req)
	}
	resp, err := h.browser.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func ReadBody(t testing.TB, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func ReadJSON(t testing.TB, resp *http.Response) map[string]any {
	t.Helper()
	body := ReadBody(t, resp)
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("response %d is not a JSON object: %v: %q", resp.StatusCode, err, body)
	}
	return out
}
