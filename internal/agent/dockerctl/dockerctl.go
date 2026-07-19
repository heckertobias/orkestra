// Package dockerctl wraps the Docker Engine SDK for container lifecycle operations.
package dockerctl

import (
	"context"
	"fmt"
	"io"

	"github.com/moby/moby/api/types/container"
	dockerevents "github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// Client wraps the Docker Engine client.
type Client struct {
	dc *client.Client
}

// New creates a dockerctl Client connected to the local Docker daemon.
func New() (*Client, error) {
	// API-version negotiation is enabled by default in moby/moby/client.
	dc, err := client.New(client.FromEnv)
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
	ID           string
	Names        []string
	Image        string
	ImageID      string
	State        string // running | exited | paused | ...
	Status       string // "Up 3 hours"
	RestartCount int
	Labels       map[string]string
}

// ListContainers returns all managed containers (all states).
func (c *Client) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	res, err := c.dc.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]ContainerInfo, 0, len(res.Items))
	for _, ct := range res.Items {
		out = append(out, ContainerInfo{
			ID:      ct.ID,
			Names:   ct.Names,
			Image:   ct.Image,
			ImageID: ct.ImageID,
			State:   string(ct.State),
			Status:  ct.Status,
			Labels:  ct.Labels,
		})
	}
	return out, nil
}

// InspectContainer returns detailed info for a single container.
func (c *Client) InspectContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	res, err := c.dc.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}
	return res.Container, nil
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	_, err := c.dc.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return err
}

// StopContainer stops a running container (10 second grace period).
func (c *Client) StopContainer(ctx context.Context, id string) error {
	timeout := 10
	_, err := c.dc.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &timeout})
	return err
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	timeout := 10
	_, err := c.dc.ContainerRestart(ctx, id, client.ContainerRestartOptions{Timeout: &timeout})
	return err
}

// RemoveContainer forcibly removes a container.
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	_, err := c.dc.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	return err
}

// PullImage pulls an image from a registry and streams the progress to w.
func (c *Client) PullImage(ctx context.Context, ref string, w io.Writer) error {
	reader, err := c.dc.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("image pull: %w", err)
	}
	defer func() { _ = reader.Close() }()
	_, err = io.Copy(w, reader)
	return err
}

// Logs streams container logs. The caller is responsible for closing the returned ReadCloser.
func (c *Client) Logs(ctx context.Context, id string, opts client.ContainerLogsOptions) (io.ReadCloser, error) {
	if !opts.ShowStdout && !opts.ShowStderr {
		opts.ShowStdout = true
		opts.ShowStderr = true
	}
	return c.dc.ContainerLogs(ctx, id, opts)
}

// Events subscribes to Docker events and sends them on the returned channel.
func (c *Client) Events(ctx context.Context) (<-chan dockerevents.Message, <-chan error) {
	f := make(client.Filters).Add("type", "container")
	res := c.dc.Events(ctx, client.EventsListOptions{Filters: f})
	return res.Messages, res.Err
}

// DockerVersion returns the Docker server version string.
func (c *Client) DockerVersion(ctx context.Context) (string, error) {
	v, err := c.dc.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return "", err
	}
	return v.Version, nil
}
