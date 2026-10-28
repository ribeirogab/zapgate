package apptest

import (
	"bytes"
	"net/http"
	"testing"
)

const UploadsPath = "/uploads"

func (h *Harness) Upload(t testing.TB, token string, data []byte, contentType string) string {
	t.Helper()
	resp := h.Post(t, UploadsPath, bytes.NewReader(data), contentType, WithBearer(token))
	body := ReadJSON(t, resp)
	id, _ := body["upload_id"].(string)
	if resp.StatusCode != http.StatusCreated || id == "" {
		t.Fatalf("upload: %d %v", resp.StatusCode, body)
	}
	return id
}
