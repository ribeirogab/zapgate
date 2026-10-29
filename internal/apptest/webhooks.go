package apptest

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/ribeirogab/zapgate/internal/clock"
)

const (
	ReceiverPath       = "/hooks/whatsapp"
	timestampTolerance = 5 * time.Minute
	endlessChunkBytes  = 32 << 10
)

var errNoTrustedSecret = errors.New("the receiver trusts no secret yet")

type Answer struct {
	Status  int
	Header  http.Header
	Endless bool
	Hold    <-chan struct{}
}

func Reply(status int) Answer {
	return Answer{Status: status}
}

func Redirect(status int, location string) Answer {
	return Answer{Status: status, Header: http.Header{"Location": {location}}}
}

func EndlessBody(status int) Answer {
	return Answer{Status: status, Endless: true}
}

func Held(release <-chan struct{}, status int) Answer {
	return Answer{Status: status, Hold: release}
}

type Received struct {
	Path           string
	Header         http.Header
	Body           []byte
	SignatureError error
}

type Receiver struct {
	URL string

	clock   clock.Clock
	arrived chan struct{}

	mu       sync.Mutex
	secrets  []string
	answers  []Answer
	received []Received
}

func (h *Harness) StartReceiver(t testing.TB, answers ...Answer) *Receiver {
	t.Helper()
	r := &Receiver{clock: h.Clock, answers: answers, arrived: make(chan struct{}, 1)}
	server := httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(server.Close)
	r.URL = server.URL + ReceiverPath
	return r
}

func (r *Receiver) Trust(secrets ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets = secrets
}

func (r *Receiver) Received() []Received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Received(nil), r.received...)
}

func (r *Receiver) Await(t testing.TB, n int) []Received {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		if got := r.Received(); len(got) >= n {
			requireSigned(t, got)
			return got
		}
		select {
		case <-r.arrived:
		case <-deadline:
			t.Fatalf("the receiver got %d requests, want %d", len(r.Received()), n)
			return nil
		}
	}
}

func requireSigned(t testing.TB, received []Received) {
	t.Helper()
	for i, request := range received {
		if request.SignatureError != nil {
			t.Fatalf("request %d to the receiver has no valid signature: %v", i+1, request.SignatureError)
		}
	}
}

func (r *Receiver) serve(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	r.record(Received{Path: req.URL.Path, Header: req.Header.Clone(), Body: body, SignatureError: r.verify(body, req.Header)})
	r.answer(w, req, r.next())
}

func (r *Receiver) record(request Received) {
	r.mu.Lock()
	r.received = append(r.received, request)
	r.mu.Unlock()
	select {
	case r.arrived <- struct{}{}:
	default:
	}
}

func (r *Receiver) next() Answer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.answers) == 0 {
		return Reply(http.StatusOK)
	}
	answer := r.answers[0]
	if len(r.answers) > 1 {
		r.answers = r.answers[1:]
	}
	return answer
}

func (r *Receiver) answer(w http.ResponseWriter, req *http.Request, answer Answer) {
	if answer.Hold != nil {
		select {
		case <-answer.Hold:
		case <-req.Context().Done():
		}
	}
	for name, values := range answer.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(answer.Status)
	if answer.Endless {
		writeEndlessly(w, req)
	}
}

func writeEndlessly(w http.ResponseWriter, req *http.Request) {
	chunk := make([]byte, endlessChunkBytes)
	flusher, _ := w.(http.Flusher)
	for req.Context().Err() == nil {
		if _, err := w.Write(chunk); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (r *Receiver) verify(body []byte, header http.Header) error {
	if err := r.checkTimestamp(header.Get(standardwebhooks.HeaderWebhookTimestamp)); err != nil {
		return err
	}
	r.mu.Lock()
	secrets := append([]string(nil), r.secrets...)
	r.mu.Unlock()
	err := errNoTrustedSecret
	for _, secret := range secrets {
		var webhook *standardwebhooks.Webhook
		if webhook, err = standardwebhooks.NewWebhook(secret); err != nil {
			continue
		}
		if err = webhook.VerifyIgnoringTimestamp(body, header); err == nil {
			return nil
		}
	}
	return err
}

func (r *Receiver) checkTimestamp(value string) error {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("the webhook-timestamp %q is not Unix seconds", value)
	}
	sent, now := time.Unix(seconds, 0), r.clock.Now()
	if sent.Before(now.Add(-timestampTolerance)) || sent.After(now.Add(timestampTolerance)) {
		return fmt.Errorf("the webhook-timestamp %s is more than %s away from the clock %s", sent.UTC(), timestampTolerance, now.UTC())
	}
	return nil
}
