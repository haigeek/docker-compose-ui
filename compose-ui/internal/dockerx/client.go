package dockerx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type Mount struct {
	Source string
}

type Container struct {
	ID      string
	Image   string
	ImageID string
	Status  string
	Names   []string
	Labels  map[string]string
	Mounts  []Mount
	Service string
}

type Image struct {
	ID       string
	RepoTags []string
	Size     int64
	Created  int64
}

type Client struct {
	raw *client.Client
}

func New() (*Client, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{raw: c}, nil
}

func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	list, err := c.raw.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(list))
	for _, item := range list {
		mounts := make([]Mount, 0, len(item.Mounts))
		for _, m := range item.Mounts {
			mounts = append(mounts, Mount{Source: m.Source})
		}
		out = append(out, Container{
			ID:      item.ID,
			Image:   item.Image,
			ImageID: item.ImageID,
			Status:  item.Status,
			Names:   item.Names,
			Labels:  item.Labels,
			Mounts:  mounts,
			Service: strings.TrimSpace(item.Labels["com.docker.compose.service"]),
		})
	}
	return out, nil
}

func (c *Client) ServiceAction(ctx context.Context, containerID, action string) error {
	switch action {
	case "stop":
		t := 10
		return c.raw.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &t})
	case "restart":
		t := 10
		return c.raw.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &t})
	case "delete":
		return c.raw.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true, RemoveVolumes: false})
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
}

func (c *Client) ProjectAction(ctx context.Context, containerIDs []string, action string) error {
	if len(containerIDs) == 0 {
		return errors.New("no containers in project")
	}
	for _, id := range containerIDs {
		if action == "start" {
			if err := c.raw.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
				return err
			}
			continue
		}
		if action == "stop" {
			t := 10
			if err := c.raw.ContainerStop(ctx, id, container.StopOptions{Timeout: &t}); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("unsupported project action: %s", action)
	}
	return nil
}

func (c *Client) Logs(ctx context.Context, containerID string, tail int, follow bool) (io.ReadCloser, error) {
	return c.raw.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Tail:       fmt.Sprintf("%d", tail),
		Follow:     follow,
	})
}

func (c *Client) ListImages(ctx context.Context) ([]Image, error) {
	list, err := c.raw.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]Image, 0, len(list))
	for _, item := range list {
		out = append(out, Image{
			ID:       item.ID,
			RepoTags: item.RepoTags,
			Size:     item.Size,
			Created:  item.Created,
		})
	}
	return out, nil
}

func (c *Client) RemoveImage(ctx context.Context, imageID string, force bool) error {
	_, err := c.raw.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
	return err
}

func ReadLogsDemuxed(raw io.Reader) ([]byte, error) {
	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &out, raw); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func DemuxLogsStream(raw io.Reader) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		_, err := stdcopy.StdCopy(pw, pw, raw)
		_ = pw.CloseWithError(err)
	}()
	return pr
}

func StreamLines(ctx context.Context, r io.Reader, onLine func(string) error) error {
	s := bufio.NewScanner(r)
	for s.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := onLine(s.Text()); err != nil {
				return err
			}
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	return nil
}

func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		d = 120 * time.Second
	}
	return context.WithTimeout(ctx, d)
}

func (c *Client) Close() error {
	return c.raw.Close()
}

var _ = types.Container{} // keep imported types package for API compatibility pin
