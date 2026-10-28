package media

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/oauth"
	"github.com/ribeirogab/zapgate/internal/store"
)

const UploadsPath = "/uploads"

var errMultipart = errors.New("send the raw file as the request body, for example curl --data-binary @photo.jpg -H 'Content-Type: image/jpeg'; multipart forms are not accepted")

type UploadHandlerDeps struct {
	Outbox *Outbox
	Store  *store.Store
	Audit  *audit.Logger
}

type uploadResponse struct {
	UploadID  string `json:"upload_id"`
	ExpiresAt string `json:"expires_at"`
}

func UploadHandler(deps UploadHandlerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := auth.TokenInfoFromContext(r.Context())
		event := audit.Upload{Grant: oauth.GrantName(info), IP: audit.ClientIP(r), Result: audit.ResultError}
		defer func() { deps.Audit.Upload(r.Context(), event) }()
		grantID, _ := oauth.GrantID(info)
		event.GrantID = grantID
		grant, err := deps.Store.ActiveGrant(r.Context(), grantID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "the access was revoked")
			return
		}
		if grant.ReadOnly {
			event.Result = audit.ResultDenied
			writeError(w, http.StatusForbidden, "the access "+grant.Name+" is read-only and cannot send media")
			return
		}
		status, upload, err := save(deps.Outbox, grantID, r)
		if err != nil {
			writeError(w, status, err.Error())
			return
		}
		event.Result, event.Size = audit.ResultOK, upload.Size
		writeJSON(w, http.StatusCreated, uploadResponse{UploadID: upload.ID, ExpiresAt: upload.ExpiresAt.UTC().Format(time.RFC3339)})
	})
}

func save(outbox *Outbox, grantID int64, r *http.Request) (int, Upload, error) {
	contentType := r.Header.Get("Content-Type")
	if mediaType, _, _ := mime.ParseMediaType(contentType); strings.HasPrefix(mediaType, "multipart/") {
		return http.StatusBadRequest, Upload{}, errMultipart
	}
	if r.ContentLength > MaxFileBytes {
		return http.StatusRequestEntityTooLarge, Upload{}, ErrTooLarge
	}
	upload, err := outbox.SaveUpload(grantID, contentType, r.Body)
	switch {
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge, Upload{}, err
	case errors.Is(err, ErrEmpty):
		return http.StatusBadRequest, Upload{}, errors.New("the body is empty; send the file as the request body")
	case err != nil:
		return http.StatusInternalServerError, Upload{}, errors.New("the server could not store the file")
	}
	return http.StatusCreated, upload, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
