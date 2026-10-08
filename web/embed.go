// Package web serves the Orxest single page application from the binary.
//
// The frontend is built with Vite into web/dist and embedded at compile time,
// which is what makes "one Orxest process plus a SQLite database" a single
// deployable artifact (spec §37, §59.20).
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the built frontend. When the frontend has not been built, a
// placeholder page explains how to build it.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return placeholder()
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return placeholder()
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if clean != "/" {
			if f, err := sub.Open(strings.TrimPrefix(clean, "/")); err == nil {
				_ = f.Close()
				if strings.HasPrefix(clean, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// Unknown path: this is a client side route, so serve the shell.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(string(index)))
	})
}

func placeholder() http.Handler {
	const page = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Orxest</title>
<style>body{font-family:ui-sans-serif,system-ui,sans-serif;margin:6rem auto;max-width:42rem;line-height:1.6;color:#111}
code{background:#f4f4f5;padding:.15rem .35rem;border-radius:.25rem}</style></head>
<body>
<h1>Orxest API is running</h1>
<p>The web interface was not built into this binary. Build it and restart, or use the REST API directly.</p>
<pre><code>cd web &amp;&amp; npm install &amp;&amp; npm run build
# then rebuild the binary
go build ./cmd/orxest</code></pre>
<p>API documentation: <a href="/api/openapi.json">/api/openapi.json</a> &middot; health: <a href="/api/health">/api/health</a></p>
</body></html>`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(page))
	})
}
