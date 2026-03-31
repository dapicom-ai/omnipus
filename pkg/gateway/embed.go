package gateway

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:spa
var spaFS embed.FS

// newSPAHandler returns an http.Handler that serves the embedded SPA.
// For any path that doesn't match a static file, it serves index.html
// (required for hash routing — the SPA handles routing client-side).
func newSPAHandler() http.Handler {
	sub, err := fs.Sub(spaFS, "spa")
	if err != nil {
		panic("gateway: embedded SPA filesystem not found: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if the file exists in the embedded FS
		cleanPath := strings.TrimPrefix(path, "/")
		if _, err := fs.Stat(sub, cleanPath); err == nil {
			switch {
			case cleanPath == "index.html" || cleanPath == "":
				// index.html must never be cached — it references hashed JS/CSS bundles.
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			case cleanPath == "sw.js" || cleanPath == "manifest.json":
				// M14: service worker and manifest must always be fresh.
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			case strings.HasPrefix(cleanPath, "assets/"):
				// M4: Vite hashes asset filenames (e.g. index-Abc123.js).
				// These can be cached indefinitely by the browser.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — serve index.html for SPA routing (no-cache)
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
