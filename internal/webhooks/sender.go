package webhooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	ConnectTimeout   = 5 * time.Second
	TotalTimeout     = 15 * time.Second
	MaxResponseBytes = 64 << 10
	MaxConcurrent    = 4
	idleConnExpiry   = 30 * time.Second
	userAgent        = "zapgate"
	jsonContentType  = "application/json"
)

const (
	headerContentType = "Content-Type"
	headerUserAgent   = "User-Agent"
	headerID          = "Webhook-Id"
	headerTimestamp   = "Webhook-Timestamp"
	headerSignature   = "Webhook-Signature"
)

type Post struct {
	URL       string
	ID        string
	Timestamp time.Time
	Body      []byte
	Secrets   []string
	Headers   map[string]string
}

type Result struct {
	Status int
	Err    error
}

func (r Result) Accepted() bool {
	return r.Err == nil && r.Status >= 200 && r.Status <= 299
}

type Sender struct {
	client *http.Client
}

func NewSender() *Sender {
	return &Sender{client: newClient()}
}

func newClient() *http.Client {
	dialer := &net.Dialer{Timeout: ConnectTimeout}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: ConnectTimeout,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        MaxConcurrent,
		IdleConnTimeout:     idleConnExpiry,
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       TotalTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (s *Sender) Send(ctx context.Context, p Post) Result {
	req, err := newRequest(ctx, p)
	if err != nil {
		return Result{Err: err}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Result{Err: explain(err)}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseBytes))
	return Result{Status: resp.StatusCode}
}

func newRequest(ctx context.Context, p Post) (*http.Request, error) {
	signature, err := Sign(p.ID, p.Timestamp, p.Body, p.Secrets...)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(p.Body))
	if err != nil {
		return nil, err
	}
	for name, value := range p.Headers {
		req.Header.Set(name, value)
	}
	req.Header.Set(headerContentType, jsonContentType)
	req.Header.Set(headerUserAgent, userAgent)
	req.Header.Set(headerID, p.ID)
	req.Header.Set(headerTimestamp, strconv.FormatInt(p.Timestamp.Unix(), 10))
	req.Header.Set(headerSignature, signature)
	return req, nil
}

func explain(err error) error {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && opErr.Timeout() {
		return fmt.Errorf("the receiver did not accept the connection within %s", ConnectTimeout)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("the receiver did not answer within %s", TotalTimeout)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
