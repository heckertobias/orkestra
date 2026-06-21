//go:build !dev

package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// Handler returns an http.Handler that serves the embedded SPA from web/dist.
// Unknown paths fall back to index.html so React Router handles client-side routing.
func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	fileServer := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := sub.Open(r.URL.Path[1:])
		if err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
