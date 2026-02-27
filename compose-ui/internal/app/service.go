package app

import (
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
)

type Service struct {
	docker         *dockerx.Client
	fileStore      *safe.FileStore
	redeployTimeou time.Duration
	mu             sync.Mutex
}

func NewService(d *dockerx.Client, fs *safe.FileStore, redeployTimeout time.Duration) *Service {
	return &Service{docker: d, fileStore: fs, redeployTimeou: redeployTimeout}
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
	return nil, errors.New("project not found")
}

func (s *Service) ReadComposeFile(ctx context.Context, projectID string) (*model.ComposeFile, error) {
	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !p.Editable || p.ComposeFilePath == "" {
		return nil, errors.New("project compose file is not editable")
	}
	b, fi, err := s.fileStore.Read(p.ComposeFilePath)
	if err != nil {
		return nil, err
	}
	return &model.ComposeFile{Content: string(b), Mtime: fi.ModTime().UnixNano(), Size: fi.Size()}, nil
}

func (s *Service) WriteComposeFile(ctx context.Context, projectID string, expectedMtime int64, content string) (*model.ComposeFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !p.Editable || p.ComposeFilePath == "" {
		return nil, errors.New("project compose file is not editable")
	}
	backup, err := s.fileStore.WriteWithBackup(p.ComposeFilePath, expectedMtime, []byte(content))
	if err != nil {
		return nil, err
	}
	b, fi, err := s.fileStore.Read(p.ComposeFilePath)
	if err != nil {
		return nil, err
	}
	return &model.ComposeFile{Content: string(b), Mtime: fi.ModTime().UnixNano(), Size: fi.Size(), BackupPath: backup}, nil
}

func (s *Service) Redeploy(ctx context.Context, projectID string) (*model.ActionResult, error) {
	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !p.Editable || p.ComposeFilePath == "" {
		return nil, errors.New("project compose file is not editable")
	}
	start := time.Now()
	tctx, cancel := dockerx.WithTimeout(ctx, s.redeployTimeou)
	defer cancel()

	cmd := exec.CommandContext(tctx, "docker", "compose", "-f", p.ComposeFilePath, "up", "-d")
	if p.WorkingDir != "" {
		cmd.Dir = p.WorkingDir
	} else {
		cmd.Dir = filepath.Dir(p.ComposeFilePath)
	}
	out, err := cmd.CombinedOutput()
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
	p, err := s.getProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(p.Services))
	for _, svc := range p.Services {
		ids = append(ids, svc.ContainerID)
	}
	start := time.Now()
	err = s.docker.ProjectAction(ctx, ids, action)
	res := &model.ActionResult{Success: err == nil, DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Message = err.Error()
		return res, err
	}
	res.Message = "project action completed"
	return res, nil
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
			Name:    cleanServiceName(c),
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
