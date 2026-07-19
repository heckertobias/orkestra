package compose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"sort"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	specHashLabel = "orkestra.spec-hash"
	stackIDLabel  = "orkestra.stack-id"
	serviceLabel  = "orkestra.service"
	managedLabel  = "orkestra.managed"
)

// Converge reconciles actual Docker state toward the desired Compose project.
func Converge(ctx context.Context, dc *client.Client, stackID string, proj *composetypes.Project) error {
	existing, err := listStackContainers(ctx, dc, stackID)
	if err != nil {
		return fmt.Errorf("list existing containers: %w", err)
	}
	existingByService := make(map[string]containerSummary)
	for _, c := range existing {
		svc := c.Labels[serviceLabel]
		existingByService[svc] = c
	}

	for _, svcName := range sortedServices(proj) {
		svc := proj.Services[svcName]
		desired := specHash(svc)

		if cur, ok := existingByService[svcName]; ok {
			if cur.Labels[specHashLabel] == desired && cur.State == "running" {
				slog.Debug("service up-to-date", "stack", stackID, "service", svcName)
				delete(existingByService, svcName)
				continue
			}
			slog.Info("recreating container", "stack", stackID, "service", svcName)
			_ = removeContainer(ctx, dc, cur.ID)
		}

		slog.Info("creating container", "stack", stackID, "service", svcName)
		if err := createAndStart(ctx, dc, stackID, proj.Name, svcName, svc, desired); err != nil {
			return fmt.Errorf("create %s/%s: %w", stackID, svcName, err)
		}
		delete(existingByService, svcName)
	}

	for svcName, c := range existingByService {
		slog.Info("removing orphan", "stack", stackID, "service", svcName)
		_ = removeContainer(ctx, dc, c.ID)
	}
	return nil
}

// Remove stops and deletes all managed containers for a stack.
func Remove(ctx context.Context, dc *client.Client, stackID string) error {
	list, err := listStackContainers(ctx, dc, stackID)
	if err != nil {
		return err
	}
	for _, c := range list {
		slog.Info("removing container", "stack", stackID, "id", c.ID[:12])
		_ = removeContainer(ctx, dc, c.ID)
	}
	return nil
}

// Stop stops (but does not remove) containers for a stack.
func Stop(ctx context.Context, dc *client.Client, stackID string) error {
	list, err := listStackContainers(ctx, dc, stackID)
	if err != nil {
		return err
	}
	timeout := 10
	for _, c := range list {
		_, _ = dc.ContainerStop(ctx, c.ID, client.ContainerStopOptions{Timeout: &timeout})
	}
	return nil
}

type containerSummary struct {
	ID     string
	Labels map[string]string
	State  string
}

func listStackContainers(ctx context.Context, dc *client.Client, stackID string) ([]containerSummary, error) {
	f := make(client.Filters).
		Add("label", managedLabel+"=true").
		Add("label", stackIDLabel+"="+stackID)
	res, err := dc.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	out := make([]containerSummary, 0, len(res.Items))
	for _, c := range res.Items {
		out = append(out, containerSummary{ID: c.ID, Labels: c.Labels, State: string(c.State)})
	}
	return out, nil
}

func removeContainer(ctx context.Context, dc *client.Client, id string) error {
	timeout := 10
	_, _ = dc.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &timeout})
	_, err := dc.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	return err
}

func createAndStart(ctx context.Context, dc *client.Client, stackID, projectName, svcName string, svc composetypes.ServiceConfig, hash string) error {
	labels := map[string]string{
		managedLabel:                 "true",
		stackIDLabel:                 stackID,
		serviceLabel:                 svcName,
		specHashLabel:                hash,
		"com.docker.compose.project": projectName,
		"com.docker.compose.service": svcName,
	}
	for k, v := range svc.Labels {
		labels[k] = v
	}

	portBindings, exposedPorts, err := buildPorts(svc.Ports)
	if err != nil {
		return err
	}

	env := make([]string, 0, len(svc.Environment))
	for k, v := range svc.Environment {
		if v != nil {
			env = append(env, k+"="+*v)
		}
	}

	image := svc.Image
	if image == "" {
		image = projectName + "_" + svcName
	} else if err := ensureImage(ctx, dc, image, svc.PullPolicy); err != nil {
		return err
	}

	var cmd, entrypoint []string
	if len(svc.Command) > 0 {
		cmd = svc.Command
	}
	if len(svc.Entrypoint) > 0 {
		entrypoint = svc.Entrypoint
	}

	cfg := &container.Config{
		Image:        image,
		Env:          env,
		Labels:       labels,
		ExposedPorts: exposedPorts,
		Cmd:          cmd,
		Entrypoint:   entrypoint,
		WorkingDir:   svc.WorkingDir,
		User:         svc.User,
	}
	hostCfg := &container.HostConfig{
		PortBindings:  portBindings,
		RestartPolicy: toRestartPolicy(svc.Restart),
		Binds:         buildBinds(svc.Volumes),
		Privileged:    svc.Privileged,
		CapAdd:        svc.CapAdd,
		CapDrop:       svc.CapDrop,
	}
	netCfg := &network.NetworkingConfig{}

	name := projectName + "-" + svcName + "-" + uuid.NewString()[:8]
	resp, err := dc.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           cfg,
		HostConfig:       hostCfg,
		NetworkingConfig: netCfg,
		Name:             name,
	})
	if err != nil {
		return fmt.Errorf("ContainerCreate: %w", err)
	}
	_, err = dc.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	return err
}

// ensureImage makes the image available locally before a container is created, honoring the
// service's Compose pull_policy. Without this, ContainerCreate fails with "No such image" for
// any image not already present in the daemon.
//
// Pulls are anonymous (no registry credentials) — private registries are not yet supported.
func ensureImage(ctx context.Context, dc *client.Client, ref, pullPolicy string) error {
	present := true
	if pullPolicy != composetypes.PullPolicyAlways {
		// Only the presence check drives the "missing" policies; "always" pulls regardless.
		if _, err := dc.ImageInspect(ctx, ref); err != nil {
			if !cerrdefs.IsNotFound(err) {
				return fmt.Errorf("inspect image %s: %w", ref, err)
			}
			present = false
		}
	}

	if !shouldPull(pullPolicy, present) {
		return nil
	}

	slog.Info("pulling image", "image", ref, "pull_policy", pullPolicy)
	rc, err := dc.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}
	defer func() { _ = rc.Close() }()
	// The pull only completes once the progress stream has been fully consumed.
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}
	return nil
}

// shouldPull decides whether to pull an image given the Compose pull_policy and whether the
// image is already present locally. An empty policy defaults to "missing" (Compose default).
// "never" and "build" never pull — for "never" a missing image lets ContainerCreate fail loudly,
// and "build" images are not fetched from a registry.
func shouldPull(policy string, present bool) bool {
	switch policy {
	case composetypes.PullPolicyAlways:
		return true
	case composetypes.PullPolicyNever, composetypes.PullPolicyBuild:
		return false
	default: // "missing", "if_not_present", or unset
		return !present
	}
}

func specHash(svc composetypes.ServiceConfig) string {
	key := struct {
		Image      string
		Cmd        []string
		Entrypoint []string
		Env        composetypes.MappingWithEquals
		Ports      []composetypes.ServicePortConfig
		WorkingDir string
		User       string
		Privileged bool
		Restart    string
	}{
		svc.Image, svc.Command, svc.Entrypoint, svc.Environment,
		svc.Ports, svc.WorkingDir, svc.User, svc.Privileged, svc.Restart,
	}
	b, _ := json.Marshal(key)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:8])
}

func sortedServices(proj *composetypes.Project) []string {
	names := make([]string, 0, len(proj.Services))
	for name := range proj.Services {
		names = append(names, name)
	}
	// Stable alphabetical order as a baseline; depends_on ordering handled by compose-go loader.
	sort.Strings(names)
	return names
}

func buildPorts(ports []composetypes.ServicePortConfig) (network.PortMap, network.PortSet, error) {
	portMap := make(network.PortMap)
	portSet := make(network.PortSet)
	for _, p := range ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		port, err := network.ParsePort(fmt.Sprintf("%d/%s", p.Target, proto))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid port %d/%s: %w", p.Target, proto, err)
		}
		portSet[port] = struct{}{}
		if p.Published != "" && p.Published != "0" {
			binding := network.PortBinding{HostPort: p.Published}
			if p.HostIP != "" {
				addr, err := netip.ParseAddr(p.HostIP)
				if err != nil {
					return nil, nil, fmt.Errorf("invalid host_ip %q: %w", p.HostIP, err)
				}
				binding.HostIP = addr
			}
			portMap[port] = append(portMap[port], binding)
		}
	}
	return portMap, portSet, nil
}

func toRestartPolicy(policy string) container.RestartPolicy {
	switch policy {
	case "always":
		return container.RestartPolicy{Name: "always"}
	case "unless-stopped":
		return container.RestartPolicy{Name: "unless-stopped"}
	case "on-failure":
		return container.RestartPolicy{Name: "on-failure"}
	default:
		return container.RestartPolicy{Name: "no"}
	}
}

func buildBinds(vols []composetypes.ServiceVolumeConfig) []string {
	var binds []string
	for _, v := range vols {
		if v.Type == "bind" && v.Source != "" {
			bind := v.Source + ":" + v.Target
			if v.ReadOnly {
				bind += ":ro"
			}
			binds = append(binds, bind)
		}
	}
	return binds
}
