package apptest

import (
	"context"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	LegacyProtocol    = "2025-11-25"
	StatelessProtocol = "2026-07-28"
)

type connectOptions struct {
	protocol   string
	ip         string
	client     mcp.ClientOptions
	middleware []mcp.Middleware
}

type ConnectOption func(*connectOptions)

func WithProtocol(version string) ConnectOption {
	return func(o *connectOptions) { o.protocol = version }
}

func WithElicitation(handler func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) ConnectOption {
	return func(o *connectOptions) { o.client.ElicitationHandler = handler }
}

func WithoutMultiRoundTrip() ConnectOption {
	return func(o *connectOptions) { o.client.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true} }
}

func WithReceivingMiddleware(middleware mcp.Middleware) ConnectOption {
	return func(o *connectOptions) { o.middleware = append(o.middleware, middleware) }
}

func WithIP(ip string) ConnectOption {
	return func(o *connectOptions) { o.ip = ip }
}

func WithToolListChanged(handler func(context.Context, *mcp.ToolListChangedRequest)) ConnectOption {
	return func(o *connectOptions) { o.client.ToolListChangedHandler = handler }
}

func (h *Harness) Connect(t testing.TB, token string, opts ...ConnectOption) *mcp.ClientSession {
	t.Helper()
	session, err := h.TryConnect(token, opts...)
	if err != nil {
		t.Fatalf("connect to %s: %v", h.MCPURL(), err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func (h *Harness) TryConnect(token string, opts ...ConnectOption) (*mcp.ClientSession, error) {
	o := connectOptions{ip: DefaultIP}
	for _, opt := range opts {
		opt(&o)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "apptest", Version: "1"}, &o.client)
	client.AddReceivingMiddleware(o.middleware...)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   h.MCPURL(),
		HTTPClient: &http.Client{Transport: clientHeaders{token: token, ip: o.ip, base: h.web}},
		MaxRetries: -1,
	}
	return client.Connect(context.Background(), transport, &mcp.ClientSessionOptions{ProtocolVersion: o.protocol})
}

func (h *Harness) MCPURL() string {
	return h.URL + "/mcp"
}

type clientHeaders struct {
	token string
	ip    string
	base  http.RoundTripper
}

func (c clientHeaders) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.ip != "" {
		r.Header.Set("CF-Connecting-IP", c.ip)
	}
	return c.base.RoundTrip(r)
}
