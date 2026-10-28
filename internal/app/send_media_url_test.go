package app_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/config"
)

const privateAddress = "the media_url leads to a private, loopback or link-local address, so it was not fetched"

type origin struct {
	*httptest.Server
	hits atomic.Int32
}

func startOrigin(t *testing.T, handler http.HandlerFunc) *origin {
	t.Helper()
	o := &origin{}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(o.Close)
	return o
}

func trustingLoopback(timeout time.Duration) apptest.Option {
	return apptest.WithConfig(func(cfg *config.Config) {
		cfg.MediaFetchAllowed = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
		if timeout > 0 {
			cfg.MediaFetchTimeout = timeout
		}
	})
}

func TestAMediaURLIsFetchedFollowingRedirectsAndSent(t *testing.T) {
	photo := pngOf(t, 5, 5)
	site := startOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/curto" {
			http.Redirect(w, r, "/fotos/praia.png?v=2", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(photo)
	})
	h := apptest.Start(t, trustingLoopback(0))
	o := &owner{answer: "accept"}
	session, _ := mediaSession(t, h, o.elicitation())

	sendMedia(t, session, map[string]any{"to": mariaJID.String(), "media_url": site.URL + "/curto"})

	if asked := o.questions(); len(asked) != 1 || !strings.Contains(asked[0], "Kind: image (image/png)\nFile: praia.png\n") {
		t.Fatalf("the owner was asked %q", asked)
	}
	if !bytes.Equal(onlyUpload(t, h), photo) || h.WhatsApp.Sent(workPhone)[0].Message.GetImageMessage().GetWidth() != 5 {
		t.Fatalf("WhatsApp got %v", h.WhatsApp.Sent(workPhone))
	}
}

func TestAMediaURLMustBeAPublicHTTPAddress(t *testing.T) {
	site := startOrigin(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "secret") })
	h := apptest.Start(t)
	session, _ := mediaSession(t, h)
	port := site.URL[strings.LastIndex(site.URL, ":")+1:]
	cases := map[string]string{
		site.URL + "/x":                            privateAddress,
		"http://localhost:" + port + "/x":          privateAddress,
		"http://[::1]:" + port + "/x":              privateAddress,
		"http://169.254.169.254/latest/meta-data":  privateAddress,
		"http://10.0.0.1/x":                        privateAddress,
		"http://192.168.1.1/x":                     privateAddress,
		"http://100.100.100.100/x":                 privateAddress,
		"http://[fd7a:115c:a1e0::1]/x":             privateAddress,
		"http://[::ffff:127.0.0.1]:" + port + "/x": privateAddress,
		"http://0.0.0.0:" + port + "/x":            privateAddress,
		"file:///etc/passwd":                       "media_url must be an http or https URL",
		"ftp://example.com/a.pdf":                  "media_url must be an http or https URL",
		"https:///no-host":                         "media_url must be an http or https URL",
	}

	for mediaURL, want := range cases {
		result := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "media_url": mediaURL}))
		if !result.IsError || errorText(result) != want {
			t.Errorf("%s: error %v %q, want %q", mediaURL, result.IsError, errorText(result), want)
		}
	}
	if site.hits.Load() != 0 {
		t.Fatalf("the private origin got %d requests", site.hits.Load())
	}
}

func TestARedirectToAPrivateAddressIsRefused(t *testing.T) {
	inside := startOrigin(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "secret") })
	port := inside.URL[strings.LastIndex(inside.URL, ":")+1:]
	targets := map[string]string{
		"/loopback": "http://127.0.0.2:" + port + "/x",
		"/ipv6":     "http://[::1]:" + port + "/x",
		"/metadata": "http://169.254.169.254/latest/meta-data",
		"/file":     "file:///etc/passwd",
	}
	outside := startOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targets[r.URL.Path], http.StatusFound)
	})
	h := apptest.Start(t, trustingLoopback(0))
	session, _ := mediaSession(t, h)
	want := map[string]string{"/loopback": privateAddress, "/ipv6": privateAddress, "/metadata": privateAddress, "/file": "media_url must be an http or https URL"}

	for path, message := range want {
		result := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "media_url": outside.URL + path}))
		if !result.IsError || errorText(result) != message {
			t.Errorf("%s: error %v %q, want %q", path, result.IsError, errorText(result), message)
		}
	}
	if inside.hits.Load() != 0 || len(h.WhatsApp.Uploads(workPhone)) != 0 {
		t.Fatalf("the inside origin got %d requests, WhatsApp got %d uploads", inside.hits.Load(), len(h.WhatsApp.Uploads(workPhone)))
	}
}

func TestAMediaURLHasASizeCapATimeoutAndMustAnswer200(t *testing.T) {
	release := make(chan struct{})
	site := startOrigin(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/declared":
			w.Header().Set("Content-Length", fmt.Sprint(maxUploadBytes+1))
			io.Copy(w, zeroBytes(maxUploadBytes+1))
		case "/streamed":
			w.(http.Flusher).Flush()
			io.Copy(w, zeroBytes(maxUploadBytes+1))
		case "/slow":
			<-release
		default:
			http.NotFound(w, r)
		}
	})
	defer close(release)
	h := apptest.Start(t, trustingLoopback(300*time.Millisecond))
	session, _ := mediaSession(t, h)
	cases := map[string]string{
		"/declared": "the file at media_url is larger than 100 MB",
		"/streamed": "the file at media_url is larger than 100 MB",
		"/slow":     "the media_url did not answer within 300ms",
		"/missing":  "the media_url answered 404 Not Found",
	}

	for path, want := range cases {
		result := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "media_url": site.URL + path}))
		if !result.IsError || errorText(result) != want {
			t.Errorf("%s: error %v %q, want %q", path, result.IsError, errorText(result), want)
		}
	}
}
