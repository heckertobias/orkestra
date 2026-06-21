//go:build dev

package webui

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Handler returns a reverse proxy to the Vite dev server at :5173.
func Handler() http.Handler {
	target, _ := url.Parse("http://localhost:5173")
	return httputil.NewSingleHostReverseProxy(target)
}
