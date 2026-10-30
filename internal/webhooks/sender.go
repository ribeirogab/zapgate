package webhooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"sync/atomic"
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
	headerRetryAfter  = "Retry-After"
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
	Status     int
	RetryAfter string
	Err        error
	Sent       bool
}

func (r Result) Accepted() bool {
	return r.Err == nil && r.Status >= 200 && r.Status <= 299
}

func (r Result) Retryable() bool {
	switch {
	case r.Err != nil:
		return true
	case r.Status == http.StatusRequestTimeout, r.Status == http.StatusTooManyRequests:
		return true
	case r.Status >= 300 && r.Status <= 399:
		return true
	}
	return r.Status >= 500
}

func (r Result) AsksToWait() bool {
	return r.RetryAfter != "" && (r.Status == http.StatusTooManyRequests || r.Status == http.StatusServiceUnavailable)
}

func (r Result) Unanswered() bool {
	return r.Sent && r.Err != nil
}

func (r Result) Gone() bool {
	return r.Err == nil && r.Status == http.StatusGone
}

func (r Result) Blocked() bool {
	var blocked *BlockedAddressError
	return errors.As(r.Err, &blocked)
}

type Sender struct {
	plain   *http.Client
	secure  *http.Client
	timeout time.Duration
}

func NewSender(resolver Resolver, timeout time.Duration) *Sender {
	dialer := &net.Dialer{Timeout: ConnectTimeout}
	plain := newTransport(localDialer{resolver: resolver, dialer: dialer}.DialContext)
	plain.DisableKeepAlives = true
	return &Sender{
		plain:   newClient(plain, timeout),
		secure:  newClient(newTransport(dialer.DialContext), timeout),
		timeout: timeout,
	}
}

func newTransport(dial func(ctx context.Context, network, address string) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		Proxy:               nil,
		DialContext:         dial,
		TLSHandshakeTimeout: ConnectTimeout,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        MaxConcurrent,
		IdleConnTimeout:     idleConnExpiry,
	}
}

func newClient(transport *http.Transport, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (s *Sender) clientFor(req *http.Request) *http.Client {
	if req.URL.Scheme == httpsScheme {
		return s.secure
	}
	return s.plain
}

func (s *Sender) Send(ctx context.Context, p Post) Result {
	var sent atomic.Bool
	req, err := newRequest(httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) { sent.Store(info.Err == nil) },
	}), p)
	if err != nil {
		return Result{Err: err}
	}
	resp, err := s.clientFor(req).Do(req)
	if err != nil {
		return Result{Err: s.explain(err), Sent: sent.Load()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseBytes))
	return Result{Status: resp.StatusCode, RetryAfter: resp.Header.Get(headerRetryAfter)}
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

func (s *Sender) explain(err error) error {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && opErr.Timeout() {
		return fmt.Errorf("the receiver did not accept the connection within %s", ConnectTimeout)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("the receiver did not answer within %s", s.timeout)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
