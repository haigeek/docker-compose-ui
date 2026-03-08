package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"compose-ui/internal/compose"
	"compose-ui/internal/dockerx"
	"compose-ui/internal/model"
	"compose-ui/internal/safe"

	"gopkg.in/yaml.v3"
)

var (
	ErrProjectNotFound       = errors.New("project not found")
	ErrProjectAmbiguous      = errors.New("project ambiguous")
	ErrComposeNotEditable    = errors.New("project compose file is not editable")
	ErrInvalidCompose        = errors.New("invalid compose file")
	ErrServiceNotFound       = errors.New("service not found")
	ErrServiceImageNotFound  = errors.New("service image field not found")
	ErrServiceImageNotString = errors.New("service image field is not a string")
)

type dockerAPI interface {
	ListContainers(ctx context.Context) ([]dockerx.Container, error)
	ServiceAction(ctx context.Context, containerID, action string) error
	ProjectActionWithProgress(ctx context.Context, containerIDs []string, action string, onProgress func(string)) error
	Logs(ctx context.Context, containerID string, tail int, follow bool) (io.ReadCloser, error)
	ListImages(ctx context.Context) ([]dockerx.Image, error)
	RemoveImage(ctx context.Context, imageID string, force bool) error
}

type fileStoreAPI interface {
	Read(path string) ([]byte, os.FileInfo, error)
	WriteWithBackup(path string, expectedMtime int64, content []byte) (backupPath string, err error)
}

type Service struct {
	docker         dockerAPI
	fileStore      fileStoreAPI
	redeployTimeou time.Duration
	composeRunner  func(ctx context.Context, workDir, composeFile string, onLog func(string)) ([]byte, error)
	mu             sync.Mutex
}

func NewService(d *dockerx.Client, fs *safe.FileStore, redeployTimeout time.Duration) *Service {
	return &Service{
		docker:         d,
		fileStore:      fs,
		redeployTimeou: redeployTimeout,
		composeRunner:  defaultComposeRunner,
	}
}

func buildProjectID(name, composePath string) string {
	sum := sha1.Sum([]byte(name + "|" + composePath))
	return hex.EncodeToString(sum[:8])
}

func cleanServiceName(c dockerx.Container) string {
	if c.Service != "" {
		return c.Service
	}
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	if len(c.ID) > 12 {
		return c.ID[:12]
	}
	return c.ID
}

func cleanContainerName(c dockerx.Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	if len(c.ID) > 12 {
		return c.ID[:12]
	}
	return c.ID
}

func (s *Service) ListProjects(ctx context.Context) ([]model.Project, error) {
	containers, err := s.docker.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	projectMap := make(map[string]*model.Project)
	for _, c := range containers {
		mounts := make([]compose.Mount, 0, len(c.Mounts))
		for _, m := range c.Mounts {
			mounts = append(mounts, compose.Mount{Source: m.Source})
		}
		resolved := compose.Resolve(c.Labels, mounts)
		if resolved.Editable {
			if _, err := os.Stat(resolved.ComposeFile); err != nil {
				resolved.Editable = false
				resolved.ComposeFile = ""
			}
		}

		pid := buildProjectID(resolved.ProjectName, resolved.ComposeFile)
		p := projectMap[pid]
		if p == nil {
			projectMap[pid] = &model.Project{
				ID:              pid,
				Name:            resolved.ProjectName,
				ComposeFilePath: resolved.ComposeFile,
				WorkingDir:      resolved.WorkingDir,
				Editable:        resolved.Editable,
				Services:        []model.Service{},
			}
			p = projectMap[pid]
		}
		p.Services = append(p.Services, model.Service{
			ID:          c.ID,
			Name:        cleanServiceName(c),
			ContainerID: c.ID,
			Status:      c.Status,
			Image:       c.Image,
		})
	}

	projects := make([]model.Project, 0, len(projectMap))
	for _, p := range projectMap {
		sort.Slice(p.Services, func(i, j int) bool { return p.Services[i].Name < p.Services[j].Name })
		projects = append(projects, *p)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

func (s *Service) getProjectByID(ctx context.Context, projectID string) (*model.Project, error) {
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		if projects[i].ID == projectID {
			return &projects[i], nil
		}
	}
	return nil, ErrProjectNotFound
}

func getProjectByName(projects []model.Project, projectName string) (*model.Project, error) {
	name := strings.TrimSpace(projectName)
	if name == "" {
		return nil, ErrProjectNotFound
	}

	var matched *model.Project
	for i := range projects {
		if projects[i].Name != name {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("%w: %s", ErrProjectAmbiguous, name)
		}
		matched = &projects[i]
	}
	if matched == nil {
		return nil, fmt.Errorf("%w: %s", ErrProjectNotFound, name)
	}
	return matched, nil
}

func (s *Service) getProjectByName(ctx context.Context, projectName string) (*model.Project, error) {
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	return getProjectByName(projects, projectName)
}

func (s *Service) ReadComposeFile(ctx context.Context, projectID string) (*model.ComposeFile, error) {
	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !p.Editable || p.ComposeFilePath == "" {
		return nil, ErrComposeNotEditable
	}
	b, fi, err := s.fileStore.Read(p.ComposeFilePath)
	if err != nil {
		return nil, err
	}
	return &model.ComposeFile{Content: string(b), Mtime: fi.ModTime().UnixMilli(), Size: fi.Size()}, nil
}

func (s *Service) WriteComposeFile(ctx context.Context, projectID string, expectedMtime int64, content string) (*model.ComposeFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !p.Editable || p.ComposeFilePath == "" {
		return nil, ErrComposeNotEditable
	}
	backup, err := s.fileStore.WriteWithBackup(p.ComposeFilePath, expectedMtime, []byte(content))
	if err != nil {
		return nil, err
	}
	b, fi, err := s.fileStore.Read(p.ComposeFilePath)
	if err != nil {
		return nil, err
	}
	return &model.ComposeFile{Content: string(b), Mtime: fi.ModTime().UnixMilli(), Size: fi.Size(), BackupPath: backup}, nil
}

func (s *Service) Redeploy(ctx context.Context, projectID string) (*model.ActionResult, error) {
	return s.redeployWithStream(ctx, projectID, nil)
}

func (s *Service) RedeployWithStream(ctx context.Context, projectID string, onLog func(string)) (*model.ActionResult, error) {
	return s.redeployWithStream(ctx, projectID, onLog)
}

func (s *Service) redeployWithStream(ctx context.Context, projectID string, onLog func(string)) (*model.ActionResult, error) {
	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !p.Editable || p.ComposeFilePath == "" {
		return nil, ErrComposeNotEditable
	}
	start := time.Now()
	tctx, cancel := dockerx.WithTimeout(ctx, s.redeployTimeou)
	defer cancel()

	workDir := p.WorkingDir
	if workDir == "" {
		workDir = filepath.Dir(p.ComposeFilePath)
	}
	out, err := s.composeRunner(tctx, workDir, p.ComposeFilePath, onLog)
	dur := time.Since(start)

	res := &model.ActionResult{
		Success:    err == nil,
		Message:    "redeploy completed",
		Stdout:     string(out),
		DurationMS: dur.Milliseconds(),
	}
	if err != nil {
		res.Message = fmt.Sprintf("redeploy failed: %v", err)
		res.Stderr = string(out)
	}
	return res, nil
}

func defaultComposeRunner(ctx context.Context, workDir, composeFile string, onLog func(string)) ([]byte, error) {
	emit := func(v string) {
		if onLog != nil {
			onLog(v)
		}
	}
	emit(fmt.Sprintf("working dir: %s", workDir))
	emit(fmt.Sprintf("compose file: %s", composeFile))
	emit(fmt.Sprintf("$ docker-compose -f %s up -d", composeFile))

	cmd := exec.CommandContext(ctx, "docker-compose", "-f", composeFile, "up", "-d")
	cmd.Dir = workDir
	out, err := runCommandWithStream(cmd, onLog)
	if err != nil && shouldFallbackToDockerComposePlugin(err, out) {
		emit("docker-compose 不可用，回退到 docker compose")
		emit(fmt.Sprintf("$ docker compose -f %s up -d", composeFile))
		pluginCmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d")
		pluginCmd.Dir = workDir
		pluginOut, pluginErr := runCommandWithStream(pluginCmd, onLog)
		if pluginErr == nil {
			return pluginOut, nil
		}
		out = append(out, []byte("\n--- fallback docker compose ---\n")...)
		out = append(out, pluginOut...)
		return out, pluginErr
	}
	return out, err
}

func updateComposeServiceImage(content, serviceName, image string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCompose, err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("%w: empty document", ErrInvalidCompose)
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: root must be a mapping", ErrInvalidCompose)
	}

	servicesNode := mappingValue(doc, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: services", ErrServiceNotFound)
	}
	serviceNode := mappingValue(servicesNode, serviceName)
	if serviceNode == nil || serviceNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: %s", ErrServiceNotFound, serviceName)
	}
	imageNode := mappingValue(serviceNode, "image")
	if imageNode == nil {
		return nil, fmt.Errorf("%w: %s", ErrServiceImageNotFound, serviceName)
	}
	if imageNode.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("%w: %s", ErrServiceImageNotString, serviceName)
	}

	imageNode.Tag = "!!str"
	imageNode.Style = 0
	imageNode.Value = image

	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCompose, err)
	}
	return out, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func (s *Service) UpdateServiceImageAndRedeploy(ctx context.Context, projectName, serviceName, image string) (*model.ActionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	projectName = strings.TrimSpace(projectName)
	serviceName = strings.TrimSpace(serviceName)
	image = strings.TrimSpace(image)

	p, err := s.getProjectByName(ctx, projectName)
	if err != nil {
		return nil, err
	}
	if !p.Editable || p.ComposeFilePath == "" {
		return nil, ErrComposeNotEditable
	}

	b, fi, err := s.fileStore.Read(p.ComposeFilePath)
	if err != nil {
		return nil, err
	}

	updated, err := updateComposeServiceImage(string(b), serviceName, image)
	if err != nil {
		return nil, err
	}

	if _, err := s.fileStore.WriteWithBackup(p.ComposeFilePath, fi.ModTime().UnixMilli(), updated); err != nil {
		return nil, err
	}

	res, err := s.redeployWithStream(ctx, p.ID, nil)
	if res != nil && err == nil {
		res.Message = fmt.Sprintf("service %s image updated to %s and redeploy completed", serviceName, image)
	}
	return res, err
}

func shouldFallbackToDockerComposePlugin(err error, out []byte) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	msg := strings.ToLower(string(out))
	patterns := []string{
		"docker-compose: command not found",
		"executable file not found",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func (s *Service) ServiceAction(ctx context.Context, serviceID, action string) (*model.ActionResult, error) {
	start := time.Now()
	err := s.docker.ServiceAction(ctx, serviceID, action)
	res := &model.ActionResult{Success: err == nil, DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Message = err.Error()
		return res, err
	}
	res.Message = "service action completed"
	return res, nil
}

func (s *Service) ProjectAction(ctx context.Context, projectID, action string) (*model.ActionResult, error) {
	return s.ProjectActionWithStream(ctx, projectID, action, nil)
}

func (s *Service) ProjectActionWithStream(ctx context.Context, projectID, action string, onLog func(string)) (*model.ActionResult, error) {
	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(p.Services))
	for _, svc := range p.Services {
		ids = append(ids, svc.ContainerID)
	}
	start := time.Now()
	err = s.docker.ProjectActionWithProgress(ctx, ids, action, onLog)
	res := &model.ActionResult{Success: err == nil, DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Message = err.Error()
		return res, err
	}
	res.Message = "project action completed"
	return res, nil
}

type lineEmitter struct {
	onLine  func(string)
	pending string
	mu      sync.Mutex
}

func (w *lineEmitter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.onLine == nil {
		return len(p), nil
	}
	w.pending += string(p)
	for {
		i := strings.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSuffix(w.pending[:i], "\r")
		w.onLine(line)
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}

func (w *lineEmitter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.onLine == nil || w.pending == "" {
		return
	}
	w.onLine(strings.TrimSuffix(w.pending, "\r"))
	w.pending = ""
}

func runCommandWithStream(cmd *exec.Cmd, onLog func(string)) ([]byte, error) {
	var out bytes.Buffer
	stdoutEmitter := &lineEmitter{onLine: onLog}
	stderrEmitter := &lineEmitter{onLine: onLog}
	cmd.Stdout = io.MultiWriter(&out, stdoutEmitter)
	cmd.Stderr = io.MultiWriter(&out, stderrEmitter)
	err := cmd.Run()
	stdoutEmitter.Flush()
	stderrEmitter.Flush()
	return out.Bytes(), err
}

func (s *Service) ReadLogs(ctx context.Context, containerID string, tail int, follow bool) (io.ReadCloser, error) {
	if tail <= 0 {
		tail = 200
	}
	return s.docker.Logs(ctx, containerID, tail, follow)
}

func (s *Service) ListContainers(ctx context.Context, keyword string) ([]model.Container, error) {
	containers, err := s.docker.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	kw := strings.ToLower(strings.TrimSpace(keyword))
	items := make([]model.Container, 0, len(containers))
	for _, c := range containers {
		mounts := make([]compose.Mount, 0, len(c.Mounts))
		for _, m := range c.Mounts {
			mounts = append(mounts, compose.Mount{Source: m.Source})
		}
		resolved := compose.Resolve(c.Labels, mounts)
		item := model.Container{
			ID:      c.ID,
			Name:    cleanContainerName(c),
			Image:   c.Image,
			Status:  c.Status,
			Project: resolved.ProjectName,
		}
		if kw != "" {
			haystack := strings.ToLower(strings.Join([]string{item.ID, item.Name, item.Image, item.Status, item.Project}, " "))
			if !strings.Contains(haystack, kw) {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *Service) ListImages(ctx context.Context, keyword, usedFilter string) ([]model.Image, error) {
	images, err := s.docker.ListImages(ctx)
	if err != nil {
		return nil, err
	}
	containers, err := s.docker.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	usedByID := make(map[string]struct{})
	usedByName := make(map[string]struct{})
	for _, c := range containers {
		if c.ImageID != "" {
			usedByID[c.ImageID] = struct{}{}
		}
		if c.Image != "" {
			usedByName[c.Image] = struct{}{}
		}
	}

	kw := strings.ToLower(strings.TrimSpace(keyword))
	filterUsed := strings.ToLower(strings.TrimSpace(usedFilter))
	items := make([]model.Image, 0, len(images))
	for _, img := range images {
		_, idUsed := usedByID[img.ID]
		used := idUsed
		if !used {
			for _, tag := range img.RepoTags {
				if _, ok := usedByName[tag]; ok {
					used = true
					break
				}
			}
		}

		if filterUsed == "used" && !used {
			continue
		}
		if filterUsed == "unused" && used {
			continue
		}

		tags := make([]string, 0, len(img.RepoTags))
		for _, tag := range img.RepoTags {
			if tag != "" && tag != "<none>:<none>" {
				tags = append(tags, tag)
			}
		}
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}

		item := model.Image{
			ID:       img.ID,
			RepoTags: tags,
			Size:     img.Size,
			Created:  img.Created,
			Used:     used,
		}

		if kw != "" {
			haystack := strings.ToLower(item.ID + " " + strings.Join(item.RepoTags, " "))
			if !strings.Contains(haystack, kw) {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Created > items[j].Created })
	return items, nil
}

func (s *Service) DeleteImages(ctx context.Context, imageIDs []string, force bool) ([]model.ImageDeleteResult, error) {
	results := make([]model.ImageDeleteResult, 0, len(imageIDs))
	for _, imageID := range imageIDs {
		id := strings.TrimSpace(imageID)
		if id == "" {
			continue
		}
		err := s.docker.RemoveImage(ctx, id, force)
		if err != nil {
			results = append(results, model.ImageDeleteResult{
				ImageID: id,
				Success: false,
				Message: err.Error(),
			})
			continue
		}
		results = append(results, model.ImageDeleteResult{
			ImageID: id,
			Success: true,
			Message: "deleted",
		})
	}
	return results, nil
}
