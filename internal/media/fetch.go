package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ribeirogab/zapgate/internal/netguard"
)

const (
	maxRedirects    = 5
	dialTimeout     = 10 * time.Second
	tlsTimeout      = 10 * time.Second
	fetchUserAgent  = "zapgate"
	httpScheme      = "http"
	httpsScheme     = "https"
	fetchIdleConns  = 4
	fetchIdleExpiry = 30 * time.Second
)

var errScheme = &FetchError{Message: "media_url must be an http or https URL"}

type FetchError struct {
	Message string
}

func (e *FetchError) Error() string { return e.Message }

type FetchConfig struct {
	Timeout time.Duration
	Allowed []netip.Prefix
}

type fetcher struct {
	client  *http.Client
	timeout time.Duration
}

func newFetcher(cfg FetchConfig) *fetcher {
	guard := netguard.PublicGuard{Allowed: cfg.Allowed}
	dialer := &net.Dialer{Timeout: dialTimeout, Control: guard.Control}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: tlsTimeout,
		MaxIdleConns:        fetchIdleConns,
		IdleConnTimeout:     fetchIdleExpiry,
	}
	return &fetcher{
		client:  &http.Client{Transport: transport, Timeout: cfg.Timeout, CheckRedirect: checkRedirect},
		timeout: cfg.Timeout,
	}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return &FetchError{Message: fmt.Sprintf("the media_url redirected more than %d times", maxRedirects)}
	}
	return checkScheme(req.URL)
}

func checkScheme(u *url.URL) error {
	if (u.Scheme != httpScheme && u.Scheme != httpsScheme) || u.Host == "" {
		return errScheme
	}
	return nil
}

func (o *Outbox) fetch(ctx context.Context, rawURL string) (File, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return File{}, errScheme
	}
	if err := checkScheme(u); err != nil {
		return File{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return File{}, errScheme
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := o.fetcher.client.Do(req)
	if err != nil {
		return File{}, o.fetcher.explain(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return File{}, &FetchError{Message: "the media_url answered " + resp.Status}
	}
	if resp.ContentLength > MaxFileBytes {
		return File{}, tooLargeAtURL()
	}
	file, err := o.store(resp.Body)
	if err != nil {
		return File{}, o.fetcher.explain(err)
	}
	file.declared = declaredMime(resp.Header.Get("Content-Type"))
	file.suggested = nameIn(resp.Request.URL)
	return file, nil
}

func (o *Outbox) store(body io.Reader) (File, error) {
	temp, err := o.tempFile()
	if err != nil {
		return File{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(body, MaxFileBytes+1))
	err = errors.Join(err, temp.Close())
	file := File{Path: temp.Name(), Size: size, SHA256: hash.Sum(nil), temporary: true}
	switch {
	case err != nil:
		file.Discard()
		return File{}, err
	case size > MaxFileBytes:
		file.Discard()
		return File{}, tooLargeAtURL()
	case size == 0:
		file.Discard()
		return File{}, ErrEmpty
	}
	return file, nil
}

func (f *fetcher) explain(err error) error {
	var fetchErr *FetchError
	var netErr net.Error
	switch {
	case errors.As(err, &fetchErr):
		return fetchErr
	case errors.Is(err, netguard.ErrBlocked):
		return &FetchError{Message: "the media_url leads to a private, loopback or link-local address, so it was not fetched"}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return &FetchError{Message: fmt.Sprintf("the media_url did not answer within %s", f.timeout)}
	case errors.Is(err, ErrEmpty):
		return err
	}
	return &FetchError{Message: fmt.Sprintf("the media_url could not be fetched: %v", errors.Unwrap(err))}
}

func tooLargeAtURL() error {
	return &FetchError{Message: fmt.Sprintf("the file at media_url is larger than %d MB", MaxFileBytes>>20)}
}

func nameIn(u *url.URL) string {
	name := path.Base(u.Path)
	if path.Ext(name) == "" {
		return ""
	}
	return name
}
