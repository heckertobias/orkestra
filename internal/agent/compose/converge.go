package compose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
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
		_ = dc.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout})
	}
	return nil
}

type containerSummary struct {
	ID     string
	Labels map[string]string
	State  string
}

func listStackContainers(ctx context.Context, dc *client.Client, stackID string) ([]containerSummary, error) {
	f := filters.NewArgs(
		filters.Arg("label", managedLabel+"=true"),
		filters.Arg("label", stackIDLabel+"="+stackID),
	)
	list, err := dc.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	out := make([]containerSummary, 0, len(list))
	for _, c := range list {
		out = append(out, containerSummary{ID: c.ID, Labels: c.Labels, State: c.State})
	}
	return out, nil
}

func removeContainer(ctx context.Context, dc *client.Client, id string) error {
	timeout := 10
	_ = dc.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
	return dc.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
}

func createAndStart(ctx context.Context, dc *client.Client, stackID, projectName, svcName string, svc composetypes.ServiceConfig, hash string) error {
	labels := map[string]string{
		managedLabel:  "true",
		stackIDLabel:  stackID,
		serviceLabel:  svcName,
		specHashLabel: hash,
		"com.docker.compose.project": projectName,
		"com.docker.compose.service": svcName,
	}
	for k, v := range svc.Labels {
		labels[k] = v
	}

	portBindings, exposedPorts := buildPorts(svc.Ports)

	env := make([]string, 0, len(svc.Environment))
	for k, v := range svc.Environment {
		if v != nil {
			env = append(env, k+"="+*v)
		}
	}

	image := svc.Image
	if image == "" {
		image = projectName + "_" + svcName
	}

	var cmd, entrypoint strslice.StrSlice
	if len(svc.Command) > 0 {
		cmd = strslice.StrSlice(svc.Command)
	}
	if len(svc.Entrypoint) > 0 {
		entrypoint = strslice.StrSlice(svc.Entrypoint)
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
		CapAdd:        strslice.StrSlice(svc.CapAdd),
		CapDrop:       strslice.StrSlice(svc.CapDrop),
	}
	netCfg := &network.NetworkingConfig{}

	name := projectName + "-" + svcName + "-" + uuid.NewString()[:8]
	resp, err := dc.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, name)
	if err != nil {
		return fmt.Errorf("ContainerCreate: %w", err)
	}
	return dc.ContainerStart(ctx, resp.ID, container.StartOptions{})
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

func buildPorts(ports []composetypes.ServicePortConfig) (nat.PortMap, nat.PortSet) {
	portMap := make(nat.PortMap)
	portSet := make(nat.PortSet)
	for _, p := range ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		port := nat.Port(fmt.Sprintf("%d/%s", p.Target, proto))
		portSet[port] = struct{}{}
		if p.Published != "" && p.Published != "0" {
			portMap[port] = append(portMap[port], nat.PortBinding{
				HostIP:   p.HostIP,
				HostPort: p.Published,
			})
		}
	}
	return portMap, portSet
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
