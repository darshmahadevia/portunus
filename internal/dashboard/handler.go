// Package dashboard serves the self-contained operator dashboard.
package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist
var assets embed.FS

// Handler serves the built dashboard without a Node runtime.
func Handler() http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		name := path.Clean(r.URL.Path)
		if name == "/" || name == "/requests" || name == "/playground" {
			w.Header().Set("Cache-Control", "no-store")
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			files.ServeHTTP(w, clone)
			return
		}
		if !strings.HasPrefix(name, "/assets/") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(root, strings.TrimPrefix(name, "/")); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}
