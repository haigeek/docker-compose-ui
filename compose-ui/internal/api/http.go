package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"compose-ui/internal/app"
	"compose-ui/internal/dockerx"
	"compose-ui/internal/model"
	"compose-ui/internal/safe"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	app      *app.Service
	authUser string
	authPass string
}

func NewServer(appSvc *app.Service, authUser, authPass string) *Server {
	return &Server{app: appSvc, authUser: authUser, authPass: authPass}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	frontend := newFrontendHandler()
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.basicAuth)
		r.Get("/projects", s.listProjects)
		r.Get("/containers", s.listContainers)
		r.Get("/projects/{projectId}/compose-file", s.getComposeFile)
		r.Put("/projects/{projectId}/compose-file", s.putComposeFile)
		r.Post("/projects/{projectId}/redeploy", s.redeploy)
		r.Post("/services/{serviceId}/action", s.serviceAction)
		r.Post("/projects/{projectId}/action", s.projectAction)
		r.Get("/images", s.listImages)
		r.Post("/images/delete", s.deleteImages)
		r.Get("/containers/{containerId}/logs", s.logs)
		r.Get("/containers/{containerId}/logs/stream", s.logStream)
	})
	r.NotFound(frontend.ServeHTTP)
	return cors(r)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !secureEqual(user, s.authUser) || !secureEqual(pass, s.authPass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="compose-ui"`)
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "认证失败", "basic auth required", false)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg, detail string, retryable bool) {
	writeJSON(w, status, model.APIError{Code: code, Message: msg, Detail: detail, Retryable: retryable})
}

func bindJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DOCKER_LIST_FAILED", "获取项目失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listContainers(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	items, err := s.app.ListContainers(r.Context(), keyword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DOCKER_LIST_FAILED", "获取容器列表失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getComposeFile(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	file, err := s.app.ReadComposeFile(r.Context(), projectID)
	if err != nil {
		if strings.Contains(err.Error(), "not editable") || strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusBadRequest, "COMPOSE_NOT_EDITABLE", "项目未关联可编辑 compose 文件", err.Error(), false)
			return
		}
		writeError(w, http.StatusInternalServerError, "READ_FILE_FAILED", "读取 compose 文件失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

type putComposeReq struct {
	Content       string `json:"content"`
	ExpectedMtime int64  `json:"expectedMtime"`
}

func (s *Server) putComposeFile(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	var req putComposeReq
	if err := bindJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求参数格式错误", err.Error(), false)
		return
	}
	file, err := s.app.WriteComposeFile(r.Context(), projectID, req.ExpectedMtime, req.Content)
	if err != nil {
		if errors.Is(err, safe.ErrMtimeConflict) {
			writeError(w, http.StatusConflict, "MTIME_CONFLICT", "文件已被其他进程修改", err.Error(), false)
			return
		}
		writeError(w, http.StatusInternalServerError, "WRITE_FILE_FAILED", "保存 compose 文件失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (s *Server) redeploy(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	res, err := s.app.Redeploy(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "REDEPLOY_FAILED", "重部署失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type actionReq struct {
	Action string `json:"action"`
}

func (s *Server) serviceAction(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceId")
	var req actionReq
	if err := bindJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求参数格式错误", err.Error(), false)
		return
	}
	res, err := s.app.ServiceAction(r.Context(), serviceID, req.Action)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SERVICE_ACTION_FAILED", "服务操作失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) projectAction(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	var req actionReq
	if err := bindJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求参数格式错误", err.Error(), false)
		return
	}
	res, err := s.app.ProjectAction(r.Context(), projectID, req.Action)
	if err != nil {
		writeError(w, http.StatusBadRequest, "PROJECT_ACTION_FAILED", "项目操作失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	used := strings.TrimSpace(r.URL.Query().Get("used"))
	items, err := s.app.ListImages(r.Context(), keyword, used)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_LIST_FAILED", "获取镜像列表失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type deleteImagesReq struct {
	ImageIDs []string `json:"imageIds"`
	Force    bool     `json:"force"`
}

func (s *Server) deleteImages(w http.ResponseWriter, r *http.Request) {
	var req deleteImagesReq
	if err := bindJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求参数格式错误", err.Error(), false)
		return
	}
	if len(req.ImageIDs) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "至少提供一个镜像ID", "", false)
		return
	}
	results, err := s.app.DeleteImages(r.Context(), req.ImageIDs, req.Force)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_DELETE_FAILED", "批量删除镜像失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "containerId")
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	follow := r.URL.Query().Get("follow") == "true"
	rc, err := s.app.ReadLogs(r.Context(), containerID, tail, follow)
	if err != nil {
		writeError(w, http.StatusBadRequest, "LOG_READ_FAILED", "读取日志失败", err.Error(), true)
		return
	}
	defer rc.Close()
	b, err := dockerx.ReadLogsDemuxed(rc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LOG_READ_FAILED", "读取日志失败", err.Error(), true)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(b)})
}

func (s *Server) logStream(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "containerId")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "当前环境不支持 SSE", "", false)
		return
	}

	rc, err := s.app.ReadLogs(r.Context(), containerID, 200, true)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", escapeSSE(err.Error()))
		flusher.Flush()
		return
	}
	defer rc.Close()
	demuxed := dockerx.DemuxLogsStream(rc)
	defer demuxed.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	_ = dockerx.StreamLines(ctx, demuxed, func(line string) error {
		fmt.Fprintf(w, "event: log\ndata: %s\n\n", escapeSSE(line))
		flusher.Flush()
		return nil
	})
}

func escapeSSE(v string) string {
	rep := strings.ReplaceAll(v, "\n", "\\n")
	return strings.TrimSpace(rep)
}

func Run(addr string, appSvc *app.Service, authUser, authPass string) error {
	srv := &http.Server{Addr: addr, Handler: NewServer(appSvc, authUser, authPass).Router(), ReadHeaderTimeout: 5 * time.Second}
	return srv.ListenAndServe()
}
