package webapp

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

type Handler struct {
	assets http.Handler
	files  fs.FS
}

func New(root fs.FS) Handler {
	return Handler{assets: http.FileServer(http.FS(root)), files: root}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header())
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	requested := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if requested != "." && requested != "" {
		if info, err := fs.Stat(h.files, requested); err == nil && !info.IsDir() {
			if strings.Contains(path.Base(requested), ".") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			h.assets.ServeHTTP(w, r)
			return
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	h.assets.ServeHTTP(w, r)
}

func setSecurityHeaders(header http.Header) {
	header.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}
