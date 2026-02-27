package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed webui/dist
var frontendFS embed.FS

func newFrontendHandler() http.Handler {
	sub, err := fs.Sub(frontendFS, "webui/dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(cleanPath, "api/") || cleanPath == "health" {
			http.NotFound(w, r)
			return
		}
		if cleanPath == "" {
			http.ServeFileFS(w, r, sub, "index.html")
			return
		}

		if _, statErr := fs.Stat(sub, cleanPath); statErr == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFileFS(w, r, sub, "index.html")
	})
}
