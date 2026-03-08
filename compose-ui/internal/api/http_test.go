package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"compose-ui/internal/app"
	"compose-ui/internal/model"
)

func TestRedeployByImageInvalidBody(t *testing.T) {
	srv := NewServer(&fakeAppService{}, "admin", "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/redeploy-by-image", strings.NewReader(`{"projectName":"demo","serviceName":"","image":"nginx:2.0"}`))
	req.Header.Set("Authorization", basicAuth("admin", "admin"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertJSONField(t, rec.Body.Bytes(), "code", "INVALID_BODY")
}

func TestRedeployByImageErrorMapping(t *testing.T) {
	srv := NewServer(&fakeAppService{
		redeployByImageFn: func(ctx context.Context, projectName, serviceName, image string) (*model.ActionResult, error) {
			return nil, app.ErrProjectAmbiguous
		},
	}, "admin", "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/redeploy-by-image", strings.NewReader(`{"projectName":"demo","serviceName":"web","image":"nginx:2.0"}`))
	req.Header.Set("Authorization", basicAuth("admin", "admin"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want %d", rec.Code, http.StatusConflict)
	}
	assertJSONField(t, rec.Body.Bytes(), "code", "PROJECT_AMBIGUOUS")
}

func TestRedeployByImageSuccess(t *testing.T) {
	srv := NewServer(&fakeAppService{
		redeployByImageFn: func(ctx context.Context, projectName, serviceName, image string) (*model.ActionResult, error) {
			return &model.ActionResult{Success: true, Message: "ok", DurationMS: 1}, nil
		},
	}, "admin", "admin")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/redeploy-by-image", strings.NewReader(`{"projectName":"demo","serviceName":"web","image":"nginx:2.0"}`))
	req.Header.Set("Authorization", basicAuth("admin", "admin"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", rec.Code, http.StatusOK)
	}
	assertJSONField(t, rec.Body.Bytes(), "message", "ok")
}

func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func assertJSONField(t *testing.T, body []byte, key, want string) {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got, _ := data[key].(string); got != want {
		t.Fatalf("field %s = %q, want %q", key, got, want)
	}
}

type fakeAppService struct {
	redeployByImageFn func(ctx context.Context, projectName, serviceName, image string) (*model.ActionResult, error)
}

func (f *fakeAppService) ListProjects(ctx context.Context) ([]model.Project, error) {
	return nil, nil
}

func (f *fakeAppService) ListContainers(ctx context.Context, keyword string) ([]model.Container, error) {
	return nil, nil
}

func (f *fakeAppService) ReadComposeFile(ctx context.Context, projectID string) (*model.ComposeFile, error) {
	return nil, nil
}

func (f *fakeAppService) WriteComposeFile(ctx context.Context, projectID string, expectedMtime int64, content string) (*model.ComposeFile, error) {
	return nil, nil
}

func (f *fakeAppService) Redeploy(ctx context.Context, projectID string) (*model.ActionResult, error) {
	return nil, nil
}

func (f *fakeAppService) UpdateServiceImageAndRedeploy(ctx context.Context, projectName, serviceName, image string) (*model.ActionResult, error) {
	if f.redeployByImageFn != nil {
		return f.redeployByImageFn(ctx, projectName, serviceName, image)
	}
	return nil, nil
}

func (f *fakeAppService) ServiceAction(ctx context.Context, serviceID, action string) (*model.ActionResult, error) {
	return nil, nil
}

func (f *fakeAppService) ProjectAction(ctx context.Context, projectID, action string) (*model.ActionResult, error) {
	return nil, nil
}

func (f *fakeAppService) RedeployWithStream(ctx context.Context, projectID string, onLog func(string)) (*model.ActionResult, error) {
	return nil, nil
}

func (f *fakeAppService) ProjectActionWithStream(ctx context.Context, projectID, action string, onLog func(string)) (*model.ActionResult, error) {
	return nil, nil
}

func (f *fakeAppService) ListImages(ctx context.Context, keyword, usedFilter string) ([]model.Image, error) {
	return nil, nil
}

func (f *fakeAppService) DeleteImages(ctx context.Context, imageIDs []string, force bool) ([]model.ImageDeleteResult, error) {
	return nil, nil
}

func (f *fakeAppService) ReadLogs(ctx context.Context, containerID string, tail int, follow bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
