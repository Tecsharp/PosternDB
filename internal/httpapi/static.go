package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const indexFile = "index.html"

// spaHandler serves the embedded React build. Unknown paths fall back to
// index.html so client-side routes survive a page reload; /api paths never
// fall back, so API typos return a real 404.
func spaHandler(static fs.FS) http.HandlerFunc {
	files := http.FileServer(http.FS(static))
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && fileExists(static, name) {
			if strings.HasPrefix(name, "assets/") {
				// Vite fingerprints asset names, so they can be cached forever.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		serveIndex(w, static)
	}
}

func serveIndex(w http.ResponseWriter, static fs.FS) {
	body, err := fs.ReadFile(static, indexFile)
	if err != nil {
		http.Error(w, "frontend not built: run `npm run build` in web/", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

func fileExists(static fs.FS, name string) bool {
	info, err := fs.Stat(static, name)
	return err == nil && !info.IsDir()
}
