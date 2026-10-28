package app_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const maxUploadBytes = 100 << 20

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func zeroBytes(n int64) io.Reader {
	return io.LimitReader(zeros{}, n)
}

func TestAnUploadWithTheTokenIsValidForOneHour(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)

	resp := h.Post(t, apptest.UploadsPath, strings.NewReader("%PDF-1.7 contrato"), "application/pdf", apptest.WithBearer(token))

	body := apptest.ReadJSON(t, resp)
	if resp.StatusCode != http.StatusCreated || body["expires_at"] != "2026-10-13T13:00:00Z" || len(body["upload_id"].(string)) < 20 {
		t.Fatalf("upload: %d %v", resp.StatusCode, body)
	}
	assertFields(t, linesWithMessage(h.AuditLines(t), "upload")[0], map[string]string{"grant": "bot", "result": "ok", "ip": apptest.DefaultIP})
}

func TestAnUploadWithoutAValidTokenIsUnauthorized(t *testing.T) {
	h := apptest.Start(t)

	for _, opts := range [][]apptest.RequestOption{nil, {apptest.WithBearer("wrong")}} {
		resp := h.Post(t, apptest.UploadsPath, strings.NewReader("data"), "image/png", opts...)

		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), resourceMetadata) {
			t.Fatalf("status %d, WWW-Authenticate %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
		}
	}
	if denied := linesWithMessage(h.AuditLines(t), "auth"); len(denied) != 2 || denied[0]["path"] != "/uploads" {
		t.Fatalf("audit %v", denied)
	}
}

func TestAReadOnlyAccessCannotUpload(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "reader", nil, true)

	resp := h.Post(t, apptest.UploadsPath, strings.NewReader("data"), "image/png", apptest.WithBearer(token))

	if body := apptest.ReadJSON(t, resp); resp.StatusCode != http.StatusForbidden || body["error"] != "the access reader is read-only and cannot send media" {
		t.Fatalf("upload: %d %v", resp.StatusCode, body)
	}
}

func TestAnUploadTakesUpTo100MB(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)

	accepted := h.Post(t, apptest.UploadsPath, zeroBytes(maxUploadBytes), "video/mp4", apptest.WithBearer(token))
	refused := h.Post(t, apptest.UploadsPath, zeroBytes(maxUploadBytes+1), "video/mp4", apptest.WithBearer(token))

	if accepted.StatusCode != http.StatusCreated {
		t.Fatalf("100 MB: %d %s", accepted.StatusCode, apptest.ReadBody(t, accepted))
	}
	if body := apptest.ReadJSON(t, refused); refused.StatusCode != http.StatusRequestEntityTooLarge || body["error"] != "the file is larger than 100 MB" {
		t.Fatalf("100 MB + 1: %d %v", refused.StatusCode, body)
	}
}

func TestAnUploadMustBeTheRawFile(t *testing.T) {
	h := apptest.Start(t)
	token := h.StaticToken(t, "bot", nil, false)

	empty := h.Post(t, apptest.UploadsPath, bytes.NewReader(nil), "image/png", apptest.WithBearer(token))
	form := h.Post(t, apptest.UploadsPath, strings.NewReader("--x\r\n"), "multipart/form-data; boundary=x", apptest.WithBearer(token))

	if body := apptest.ReadJSON(t, empty); empty.StatusCode != http.StatusBadRequest || body["error"] != "the body is empty; send the file as the request body" {
		t.Fatalf("empty: %d %v", empty.StatusCode, body)
	}
	if body := apptest.ReadJSON(t, form); form.StatusCode != http.StatusBadRequest || !strings.Contains(body["error"].(string), "--data-binary") {
		t.Fatalf("multipart: %d %v", form.StatusCode, body)
	}
}

func uploadFiles(t *testing.T, h *apptest.Harness) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(h.DataDir, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestAnUploadBelongsToTheAccessThatSentIt(t *testing.T) {
	h := apptest.Start(t)
	h.AddAccount(t, "work", workPhone, "Alex")
	id := h.Upload(t, h.StaticToken(t, "first", []string{"work"}, false), []byte(contract), "application/pdf")
	other := h.Connect(t, h.StaticToken(t, "second", []string{"work"}, false))

	result := callTool(t, other, "send_media", withWork(map[string]any{"to": mariaJID.String(), "upload_id": id}))

	if want := `the upload_id "` + id + `" is unknown for this access; POST the file to /uploads with the same token and pass the new upload_id`; !result.IsError || errorText(result) != want {
		t.Fatalf("error %v %q", result.IsError, errorText(result))
	}
}

func TestAnExpiredUploadIsRefusedAndDeleted(t *testing.T) {
	h := apptest.Start(t)
	session, token := mediaSession(t, h)
	expired := h.Upload(t, token, []byte(contract), "application/pdf")
	h.Clock.Advance(30 * time.Minute)
	fresh := h.Upload(t, token, []byte(contract), "application/pdf")
	h.Clock.Advance(30 * time.Minute)

	result := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "upload_id": expired}))

	if want := `the upload_id "` + expired + `" expired at 2026-10-13T13:00:00Z; POST the file to /uploads again`; !result.IsError || errorText(result) != want {
		t.Fatalf("error %v %q", result.IsError, errorText(result))
	}
	h.Restart(t)
	if files := uploadFiles(t, h); !slices.Equal(files, []string{fresh + ".data", fresh + ".json"}) {
		t.Fatalf("data/uploads holds %v", files)
	}
	session = h.Connect(t, token)
	sendMedia(t, session, map[string]any{"to": mariaJID.String(), "upload_id": fresh})
}

func TestAnUploadIDIsNeverAPath(t *testing.T) {
	h := apptest.Start(t)
	session, _ := mediaSession(t, h)

	result := callTool(t, session, "send_media", withWork(map[string]any{"to": mariaJID.String(), "upload_id": "../app"}))

	if !result.IsError || !strings.Contains(errorText(result), "is unknown for this access") {
		t.Fatalf("error %v %q", result.IsError, errorText(result))
	}
}
