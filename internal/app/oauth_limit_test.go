package app_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const scannerIP = "192.0.2.66"

func failPasswords(t *testing.T, h *apptest.Harness, login *apptest.Login, ip string, count int) {
	t.Helper()
	form := h.OpenAuthorizePage(t, login.Request)
	for i := range count {
		resp := h.SubmitAuthorize(t, form.Submission("guess", "claude"), apptest.FromIP(ip))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: status %d, want 401", i+1, resp.StatusCode)
		}
	}
}

func TestFiveWrongPasswordsBlockTheIP(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	failPasswords(t, h, login, scannerIP, 5)
	form := h.OpenAuthorizePage(t, login.Request)

	blocked := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, "claude"), apptest.FromIP(scannerIP))

	if blocked.StatusCode != http.StatusTooManyRequests || blocked.Header.Get("Location") != "" {
		t.Fatalf("status %d, location %q; want 429 even with the right password", blocked.StatusCode, blocked.Header.Get("Location"))
	}
	if blocked.Header.Get("Retry-After") == "" || !strings.Contains(apptest.ReadBody(t, blocked), "Too many wrong passwords from this address.") {
		t.Fatal("the blocked answer has no Retry-After or no explanation")
	}
	other := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, "claude"), apptest.FromIP("198.51.100.30"))
	apptest.RedirectParams(t, other, apptest.ClaudeCallback)
}

func TestParallelWrongPasswordsFromOneIPGetOnlyFiveTries(t *testing.T) {
	h := apptest.Start(t)
	form := h.OpenAuthorizePage(t, h.NewLogin(t).Request)
	const attempts = 40
	statuses := make(chan int, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			<-start
			statuses <- h.SubmitAuthorize(t, form.Submission("guess", "claude"), apptest.FromIP(scannerIP)).StatusCode
		})
	}
	close(start)
	wg.Wait()
	close(statuses)

	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusUnauthorized] != 5 || counts[http.StatusTooManyRequests] != attempts-5 {
		t.Fatalf("statuses %v; want 5 checked passwords (401) and %d blocked (429)", counts, attempts-5)
	}
}

func TestTheBlockEndsFifteenMinutesAfterTheFailures(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	failPasswords(t, h, login, scannerIP, 5)
	form := h.OpenAuthorizePage(t, login.Request)

	h.Clock.Advance(14*time.Minute + 59*time.Second)
	still := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, "claude"), apptest.FromIP(scannerIP))
	h.Clock.Advance(time.Second)
	freed := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, "claude"), apptest.FromIP(scannerIP))

	if still.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d before 15 minutes, want 429", still.StatusCode)
	}
	apptest.RedirectParams(t, freed, apptest.ClaudeCallback)
}

func TestFailuresSpreadOverMoreThanFifteenMinutesDoNotBlock(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	failPasswords(t, h, login, scannerIP, 4)
	h.Clock.Advance(16 * time.Minute)
	failPasswords(t, h, login, scannerIP, 4)
	form := h.OpenAuthorizePage(t, login.Request)

	resp := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, "claude"), apptest.FromIP(scannerIP))

	apptest.RedirectParams(t, resp, apptest.ClaudeCallback)
}

func TestTheLimitUsesTheTunnelClientIPNotTheProxyAddress(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	failPasswords(t, h, login, scannerIP, 5)
	form := h.OpenAuthorizePage(t, login.Request)

	resp := h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, "claude"), apptest.FromIP("203.0.113.200"))

	apptest.RedirectParams(t, resp, apptest.ClaudeCallback)
}

func TestPasswordFailuresAreAuditedWithoutThePassword(t *testing.T) {
	h := apptest.Start(t)
	login := h.NewLogin(t)
	failPasswords(t, h, login, scannerIP, 5)
	form := h.OpenAuthorizePage(t, login.Request)
	h.SubmitAuthorize(t, form.Submission(apptest.OwnerPassword, "claude"), apptest.FromIP(scannerIP))

	events := linesWithMessage(h.AuditLines(t), "oauth")
	var denied, blocked int
	for _, line := range events {
		if line["event"] != "authorize" {
			continue
		}
		assertFields(t, line, map[string]string{"ip": scannerIP, "client": "Claude", "reason": "wrong_password"})
		switch line["result"] {
		case "denied":
			denied++
		case "blocked":
			blocked++
		}
	}
	if denied != 5 || blocked != 1 {
		t.Fatalf("audit has %d denied and %d blocked authorize lines, want 5 and 1: %s", denied, blocked, h.AuditRaw())
	}
	for _, secret := range []string{"guess", apptest.OwnerPassword} {
		if strings.Contains(h.AuditRaw(), secret) {
			t.Fatalf("the audit log contains the password %q", secret)
		}
	}
}
