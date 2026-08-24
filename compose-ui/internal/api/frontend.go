package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed webui/dist
var frontendFS embed.FS

// frontendConfig 是运行时注入到前端页面的配置，部署时可无需重新构建前端。
type frontendConfig struct {
	EnableProjectManagement bool `json:"enableProjectManagement"`
}

func newFrontendHandler(cfg frontendConfig) http.Handler {
	sub, err := fs.Sub(frontendFS, "webui/dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	indexHTML := buildIndexHTML(sub, cfg)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(cleanPath, "api/") || cleanPath == "health" {
			http.NotFound(w, r)
			return
		}
		if cleanPath == "" {
			serveIndexHTML(w, indexHTML)
			return
		}

		// 构建产物文件名带内容 hash，不可变，可长期缓存
		if strings.HasPrefix(cleanPath, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		if _, statErr := fs.Stat(sub, cleanPath); statErr == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		serveIndexHTML(w, indexHTML)
	})
}

// buildIndexHTML 读取嵌入的 index.html，并注入 window.__COMPOSE_UI_CONFIG__。
func buildIndexHTML(sub fs.FS, cfg frontendConfig) []byte {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		raw = []byte("{}")
	}
	script := fmt.Sprintf("<script>window.__COMPOSE_UI_CONFIG__=%s;</script>", raw)
	html := string(data)
	if idx := strings.Index(html, "</head>"); idx >= 0 {
		html = html[:idx] + script + html[idx:]
	} else {
		html += script
	}
	return []byte(html)
}

func serveIndexHTML(w http.ResponseWriter, content []byte) {
	if content == nil {
		http.Error(w, "index.html not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// index.html 是入口且动态注入运行时配置，禁用强缓存，始终校验最新版本
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(content)
}
