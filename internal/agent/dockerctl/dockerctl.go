// Package dockerctl wraps the Docker Engine SDK for container lifecycle operations.
package dockerctl

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	dockerevents "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// Client wraps the Docker Engine client.
type Client struct {
	dc *client.Client
}

// New creates a dockerctl Client connected to the local Docker daemon.
func New() (*Client, error) {
	dc, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return &Client{dc: dc}, nil
}

// Close releases the underlying Docker client.
func (c *Client) Close() error { return c.dc.Close() }

// RawClient returns the underlying *client.Client for use by packages that need it directly.
func (c *Client) RawClient() *client.Client { return c.dc }

// ContainerInfo is a summary of a single container.
type ContainerInfo struct {
	ID          string
	Names       []string
	Image       string
	ImageID     string
	State       string // running | exited | paused | ...
	Status      string // "Up 3 hours"
	RestartCount int
	Labels      map[string]string
}

// ListContainers returns all managed containers (all states).
func (c *Client) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	containers, err := c.dc.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]ContainerInfo, 0, len(containers))
	for _, ct := range containers {
		out = append(out, ContainerInfo{
			ID:      ct.ID,
			Names:   ct.Names,
			Image:   ct.Image,
			ImageID: ct.ImageID,
			State:   ct.State,
			Status:  ct.Status,
			Labels:  ct.Labels,
		})
	}
	return out, nil
}

// InspectContainer returns detailed info for a single container.
func (c *Client) InspectContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	return c.dc.ContainerInspect(ctx, id)
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.dc.ContainerStart(ctx, id, container.StartOptions{})
}

// StopContainer stops a running container (10 second grace period).
func (c *Client) StopContainer(ctx context.Context, id string) error {
	timeout := 10
	return c.dc.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	timeout := 10
	return c.dc.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout})
}

// RemoveContainer forcibly removes a container.
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	return c.dc.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
}

// PullImage pulls an image from a registry and streams the progress to w.
func (c *Client) PullImage(ctx context.Context, ref string, w io.Writer) error {
	reader, err := c.dc.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("image pull: %w", err)
	}
	defer func() { _ = reader.Close() }()
	_, err = io.Copy(w, reader)
	return err
}

// Logs streams container logs. The caller is responsible for closing the returned ReadCloser.
func (c *Client) Logs(ctx context.Context, id string, opts container.LogsOptions) (io.ReadCloser, error) {
	if !opts.ShowStdout && !opts.ShowStderr {
		opts.ShowStdout = true
		opts.ShowStderr = true
	}
	return c.dc.ContainerLogs(ctx, id, opts)
}

// Events subscribes to Docker events and sends them on the returned channel.
func (c *Client) Events(ctx context.Context) (<-chan dockerevents.Message, <-chan error) {
	f := filters.NewArgs()
	f.Add("type", "container")
	return c.dc.Events(ctx, dockerevents.ListOptions{Filters: f})
}

// DockerVersion returns the Docker server version string.
func (c *Client) DockerVersion(ctx context.Context) (string, error) {
	v, err := c.dc.ServerVersion(ctx)
	if err != nil {
		return "", err
	}
	return v.Version, nil
}
