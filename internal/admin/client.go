package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	socketBaseURL = "http://owner"
	clientTimeout = 30 * time.Second
)

type Client struct {
	http      *http.Client
	streaming *http.Client
}

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func Dial(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		http:      &http.Client{Transport: transport, Timeout: clientTimeout},
		streaming: &http.Client{Transport: transport},
	}
}

func (c *Client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

var errPairingCut = errors.New("the server closed the pairing before the account connected")

func (c *Client) AddAccount(ctx context.Context, req AddAccountRequest, progress func(PairingEvent)) (AccountView, error) {
	httpReq, err := newRequest(ctx, http.MethodPost, "/accounts", req)
	if err != nil {
		return AccountView{}, err
	}
	resp, err := c.streaming.Do(httpReq)
	if err != nil {
		return AccountView{}, fmt.Errorf("cannot reach the server on its owner socket: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return AccountView{}, responseError(resp)
	}
	return readPairing(resp.Body, progress)
}

func readPairing(body io.Reader, progress func(PairingEvent)) (AccountView, error) {
	decoder := json.NewDecoder(body)
	for {
		var event PairingEvent
		if err := decoder.Decode(&event); err != nil {
			return AccountView{}, errPairingCut
		}
		switch event.Event {
		case PairingConnected:
			if event.Account == nil {
				return AccountView{}, errPairingCut
			}
			return *event.Account, nil
		case PairingFailed:
			return AccountView{}, errors.New(event.Error)
		default:
			progress(event)
		}
	}
}

func (c *Client) Account(ctx context.Context, name string) (AccountView, error) {
	var out accountResponse
	err := c.call(ctx, http.MethodGet, "/accounts/"+url.PathEscape(name), nil, &out)
	return out.Account, err
}

func (c *Client) DescribeAccount(ctx context.Context, name, description string) (AccountView, error) {
	var out accountResponse
	err := c.call(ctx, http.MethodPut, "/accounts/"+url.PathEscape(name)+"/description", DescribeAccountRequest{Description: description}, &out)
	return out.Account, err
}

func (c *Client) RemoveAccount(ctx context.Context, name string) (RemovedAccount, error) {
	var out RemovedAccount
	err := c.call(ctx, http.MethodDelete, "/accounts/"+url.PathEscape(name), nil, &out)
	return out, err
}

func (c *Client) SetAccess(ctx context.Context, name string, req SetAccessRequest) (AccessView, error) {
	var out accessResponse
	err := c.call(ctx, http.MethodPatch, "/access/"+url.PathEscape(name), req, &out)
	return out.Access, err
}

func (c *Client) Status(ctx context.Context) (StatusView, error) {
	var out StatusView
	err := c.call(ctx, http.MethodGet, "/status", nil, &out)
	return out, err
}

func (c *Client) SetRetention(ctx context.Context, name string, months int) (AccountView, error) {
	var out accountResponse
	err := c.call(ctx, http.MethodPut, "/accounts/"+url.PathEscape(name)+"/retention", SetRetentionRequest{Months: months}, &out)
	return out.Account, err
}

func (c *Client) Accounts(ctx context.Context) ([]AccountView, error) {
	var out accountsResponse
	err := c.call(ctx, http.MethodGet, "/accounts", nil, &out)
	return out.Accounts, err
}

func (c *Client) CreateToken(ctx context.Context, req CreateTokenRequest) (CreatedToken, error) {
	var out CreatedToken
	err := c.call(ctx, http.MethodPost, "/tokens", req, &out)
	return out, err
}

func (c *Client) Access(ctx context.Context) ([]AccessView, error) {
	var out accessListResponse
	err := c.call(ctx, http.MethodGet, "/access", nil, &out)
	return out.Access, err
}

func (c *Client) RevokeAccess(ctx context.Context, name string) (AccessView, error) {
	var out accessResponse
	err := c.call(ctx, http.MethodPost, "/access/"+url.PathEscape(name)+"/revoke", nil, &out)
	return out.Access, err
}

func (c *Client) AddWebhook(ctx context.Context, req AddWebhookRequest) (CreatedWebhook, error) {
	var out CreatedWebhook
	err := c.call(ctx, http.MethodPost, "/webhooks", req, &out)
	return out, err
}

func (c *Client) Webhooks(ctx context.Context) ([]WebhookView, error) {
	var out webhookListResponse
	err := c.call(ctx, http.MethodGet, "/webhooks", nil, &out)
	return out.Webhooks, err
}

func (c *Client) RemoveWebhook(ctx context.Context, name string) (RemovedWebhook, error) {
	var out RemovedWebhook
	err := c.call(ctx, http.MethodDelete, "/webhooks/"+url.PathEscape(name), nil, &out)
	return out, err
}

func (c *Client) TestWebhook(ctx context.Context, name string) (WebhookTest, error) {
	var out WebhookTest
	err := c.call(ctx, http.MethodPost, "/webhooks/"+url.PathEscape(name)+"/test", nil, &out)
	return out, err
}

func (c *Client) RotateWebhookSecret(ctx context.Context, name string) (RotatedWebhookSecret, error) {
	var out RotatedWebhookSecret
	err := c.call(ctx, http.MethodPost, "/webhooks/"+url.PathEscape(name)+"/secret", nil, &out)
	return out, err
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	req, err := newRequest(ctx, method, path, in)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the server on its owner socket: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return responseError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func newRequest(ctx context.Context, method, path string, in any) (*http.Request, error) {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, socketBaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func responseError(resp *http.Response) error {
	var body errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Error == "" {
		return &Error{Status: resp.StatusCode, Message: fmt.Sprintf("the server answered %s", resp.Status)}
	}
	return &Error{Status: resp.StatusCode, Message: body.Error}
}
