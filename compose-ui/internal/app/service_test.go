package app

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compose-ui/internal/compose"
	"compose-ui/internal/dockerx"
	"compose-ui/internal/model"
)

func TestShouldFallbackToDockerComposePlugin(t *testing.T) {
	tests := []struct {
		name string
		err  error
		out  string
		want bool
	}{
		{name: "nil error", err: nil, out: "", want: false},
		{name: "docker-compose not found", err: exec.ErrNotFound, out: "", want: true},
		{name: "docker-compose shell missing", err: errors.New("exit status 127"), out: "docker-compose: command not found", want: true},
		{name: "docker-compose executable missing", err: errors.New("exit status 1"), out: "executable file not found in $PATH", want: true},
		{name: "other error", err: errors.New("exit status 1"), out: "permission denied", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFallbackToDockerComposePlugin(tt.err, []byte(tt.out))
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetProjectByName(t *testing.T) {
	projects := []model.Project{
		{Name: "alpha"},
		{Name: "beta"},
	}

	got, err := getProjectByName(projects, "beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "beta" {
		t.Fatalf("unexpected project: %+v", got)
	}
}

func TestGetProjectByNameNotFound(t *testing.T) {
	_, err := getProjectByName([]model.Project{{Name: "alpha"}}, "beta")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestGetProjectByNameAmbiguous(t *testing.T) {
	_, err := getProjectByName([]model.Project{{Name: "alpha"}, {Name: "alpha"}}, "alpha")
	if !errors.Is(err, ErrProjectAmbiguous) {
		t.Fatalf("expected ErrProjectAmbiguous, got %v", err)
	}
}

func TestUpdateComposeServiceImage(t *testing.T) {
	input := "services:\n  web:\n    image: nginx:1.0\n"

	out, err := updateComposeServiceImage(input, "web", "nginx:2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "image: nginx:2.0") {
		t.Fatalf("expected image to be updated, got:\n%s", string(out))
	}
}

func TestUpdateComposeServiceImageErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr error
	}{
		{name: "invalid yaml", content: "services:\n  web:\n    image: [", wantErr: ErrInvalidCompose},
		{name: "missing services", content: "name: demo\n", wantErr: ErrServiceNotFound},
		{name: "missing service", content: "services:\n  api:\n    image: nginx\n", wantErr: ErrServiceNotFound},
		{name: "missing image", content: "services:\n  web:\n    command: sleep infinity\n", wantErr: ErrServiceImageNotFound},
		{name: "image not string", content: "services:\n  web:\n    image:\n      name: nginx\n", wantErr: ErrServiceImageNotString},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := updateComposeServiceImage(tt.content, "web", "nginx:2.0")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestUpdateServiceImageAndRedeploy(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  web:\n    image: nginx:1.0\n"), 0o644); err != nil {
		t.Fatalf("write temp compose: %v", err)
	}
	store := &fakeFileStore{
		content: []byte("services:\n  web:\n    image: nginx:1.0\n"),
		info:    fakeFileInfo{mtime: time.UnixMilli(1000)},
	}
	svc := &Service{
		docker: &fakeDocker{
			containers: []dockerx.Container{composeContainer("demo", composePath, "web", "nginx:1.0")},
		},
		fileStore:      store,
		redeployTimeou: time.Second,
		composeRunner: func(ctx context.Context, workDir, composeFile string, onLog func(string)) ([]byte, error) {
			if composeFile != composePath {
				t.Fatalf("unexpected compose file: %s", composeFile)
			}
			return []byte("redeployed"), nil
		},
	}

	res, err := svc.UpdateServiceImageAndRedeploy(context.Background(), "demo", "web", "nginx:2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(store.written), "image: nginx:2.0") {
		t.Fatalf("expected updated compose content, got:\n%s", string(store.written))
	}
	if res.Message != "service web image updated to nginx:2.0 and redeploy completed" {
		t.Fatalf("unexpected message: %s", res.Message)
	}
}

func TestUpdateServiceImageAndRedeployWriteFailure(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  web:\n    image: nginx:1.0\n"), 0o644); err != nil {
		t.Fatalf("write temp compose: %v", err)
	}
	store := &fakeFileStore{
		content:  []byte("services:\n  web:\n    image: nginx:1.0\n"),
		info:     fakeFileInfo{mtime: time.UnixMilli(1000)},
		writeErr: errors.New("disk full"),
	}
	runnerCalled := false
	svc := &Service{
		docker: &fakeDocker{
			containers: []dockerx.Container{composeContainer("demo", composePath, "web", "nginx:1.0")},
		},
		fileStore:      store,
		redeployTimeou: time.Second,
		composeRunner: func(ctx context.Context, workDir, composeFile string, onLog func(string)) ([]byte, error) {
			runnerCalled = true
			return nil, nil
		},
	}

	_, err := svc.UpdateServiceImageAndRedeploy(context.Background(), "demo", "web", "nginx:2.0")
	if err == nil {
		t.Fatal("expected error")
	}
	if runnerCalled {
		t.Fatal("compose runner should not be called on write failure")
	}
}

func composeContainer(projectName, composePath, serviceName, image string) dockerx.Container {
	workDir := filepath.Dir(composePath)
	return dockerx.Container{
		ID:    "container-id-123456",
		Image: image,
		Labels: map[string]string{
			compose.LabelProjectName: projectName,
			compose.LabelWorkingDir:  workDir,
			compose.LabelConfigFiles: filepath.Base(composePath),
			compose.LabelServiceName: serviceName,
		},
		Mounts: []dockerx.Mount{{Source: workDir}},
	}
}

type fakeDocker struct {
	containers []dockerx.Container
}

func (f *fakeDocker) ListContainers(ctx context.Context) ([]dockerx.Container, error) {
	return f.containers, nil
}

func (f *fakeDocker) ServiceAction(ctx context.Context, containerID, action string) error {
	return nil
}

func (f *fakeDocker) ProjectActionWithProgress(ctx context.Context, containerIDs []string, action string, onProgress func(string)) error {
	return nil
}

func (f *fakeDocker) Logs(ctx context.Context, containerID string, tail int, follow bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeDocker) ListImages(ctx context.Context) ([]dockerx.Image, error) {
	return nil, nil
}

func (f *fakeDocker) RemoveImage(ctx context.Context, imageID string, force bool) error {
	return nil
}

type fakeFileStore struct {
	content  []byte
	info     os.FileInfo
	written  []byte
	writeErr error
}

func (f *fakeFileStore) Read(path string) ([]byte, os.FileInfo, error) {
	return append([]byte(nil), f.content...), f.info, nil
}

func (f *fakeFileStore) WriteWithBackup(path string, expectedMtime int64, content []byte) (string, error) {
	if f.writeErr != nil {
		return "", f.writeErr
	}
	f.written = append([]byte(nil), content...)
	f.content = append([]byte(nil), content...)
	return path + ".bak", nil
}

type fakeFileInfo struct {
	mtime time.Time
}

func (f fakeFileInfo) Name() string       { return "docker-compose.yml" }
func (f fakeFileInfo) Size() int64        { return int64(len("docker-compose")) }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o644 }
func (f fakeFileInfo) ModTime() time.Time { return f.mtime }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }
