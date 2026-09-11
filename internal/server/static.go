package server

import (
	"net/http"
	"path"
	"strings"
)

// staticHandler serves the embedded SPA (spec §1). Unknown extensionless
// paths fall back to index.html so client-side routes work; unknown paths
// with an extension (missing assets) get a 404.
func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServer(s.staticFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := s.staticFS.Open(p); err == nil {
			f.Close()
		} else if path.Ext(p) == "" {
			// SPA route: fall back to the app shell.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		} else {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", cacheControlFor(p))
		fileServer.ServeHTTP(w, r)
	})
}

// cacheControlFor sets the Cache-Control header for a static path (spec §6
// PWA). The app shell, the service worker and the manifest are always
// revalidated, so a new build is picked up on the next page load instead
// of waiting out heuristic caching; content-hashed build assets are
// immutable; everything else gets a short cache.
func cacheControlFor(p string) string {
	switch {
	case p == "index.html" || p == "sw.js" || strings.HasSuffix(p, ".webmanifest"):
		return "no-cache"
	case strings.HasPrefix(p, "assets/"):
		return "public, max-age=31536000, immutable"
	default:
		return "public, max-age=300"
	}
}
