package media

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/ribeirogab/zapgate/internal/store"
)

const LinkRoute = "GET " + LinkPath + "{token}"

type HandlerDeps struct {
	Store *store.Store
	Files *Files
	Links *Links
	Log   *slog.Logger
}

func Handler(deps HandlerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safeHeaders(w.Header())
		path, err := deps.Links.verify(r.PathValue("token"))
		if errors.Is(err, errLinkExpired) {
			http.Error(w, "this media link expired; call download_media again for a new link", http.StatusGone)
			return
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		file, err := deps.Store.MediaFileByPath(r.Context(), path)
		if err != nil {
			notFound(w, r, deps.Log, err)
			return
		}
		content, err := deps.Files.Open(path)
		if err != nil {
			notFound(w, r, deps.Log, err)
			return
		}
		defer content.Close()
		w.Header().Set("Content-Type", ContentType(file))
		w.Header().Set("Content-Disposition", contentDisposition(file))
		http.ServeContent(w, r, "", time.Time{}, content)
	})
}

func safeHeaders(h http.Header) {
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Referrer-Policy", "no-referrer")
}

func notFound(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("cannot serve a media link", "error", err)
	}
	http.NotFound(w, r)
}
