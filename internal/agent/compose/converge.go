package compose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"

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

	var svcErrs []error
	for _, svcName := range sortedServices(proj) {
		svc := proj.Services[svcName]

		cur, exists := existingByService[svcName]
		// This service is part of the desired project, so it is never an orphan — mark it handled
		// regardless of the outcome below, so the orphan-removal loop leaves it alone.
		delete(existingByService, svcName)

		// Phase 1: resolve and pull the image per pull_policy BEFORE touching any container. This
		// keeps a running container alive across a slow or failing pull, and lets pull_policy:always
		// pick up a repointed tag instead of pulling only after the old container is already gone.
		image := svc.Image
		if image == "" {
			image = proj.Name + "_" + svcName
		} else if err := ensureImage(ctx, dc, image, svc.PullPolicy); err != nil {
			// Leave any existing container running and keep reconciling the rest of the stack.
			slog.Error("resolve image failed", "stack", stackID, "service", svcName, "err", err)
			svcErrs = append(svcErrs, fmt.Errorf("%s: %w", svcName, err))
			continue
		}

		// Phase 2: hash over the resolved image ID, so a moved tag (same string, new content)
		// changes identity and forces a recreate — a plain tag string never would.
		desired := specHash(svc, imageID(ctx, dc, image))

		// Phase 3: compare, then remove + recreate only if drifted.
		if exists {
			if cur.Labels[specHashLabel] == desired && cur.State == "running" {
				slog.Debug("service up-to-date", "stack", stackID, "service", svcName)
				continue
			}
			slog.Info("recreating container", "stack", stackID, "service", svcName)
			_ = removeContainer(ctx, dc, cur.ID)
		}

		slog.Info("creating container", "stack", stackID, "service", svcName)
		if err := createAndStart(ctx, dc, stackID, proj.Name, svcName, svc, image, desired); err != nil {
			// Partial failure: record and keep reconciling the rest of the stack. Aborting here
			// would leave every alphabetically-later service and the orphan cleanup untouched.
			slog.Error("create container failed", "stack", stackID, "service", svcName, "err", err)
			svcErrs = append(svcErrs, fmt.Errorf("%s: %w", svcName, err))
			continue
		}
	}

	// Orphan removal always runs, even after a service failure above.
	for svcName, c := range existingByService {
		slog.Info("removing orphan", "stack", stackID, "service", svcName)
		_ = removeContainer(ctx, dc, c.ID)
	}

	if len(svcErrs) > 0 {
		return fmt.Errorf("converge %s: %d service(s) failed: %w", stackID, len(svcErrs), errors.Join(svcErrs...))
	}
	return nil
}

// ListManagedStackIDs returns the distinct stack IDs of every orkestra-managed container on the
// host. It is used to detect stacks that have disappeared from the desired state and must be
// removed, since the Master always pushes the full desired state rather than a diff.
func ListManagedStackIDs(ctx context.Context, dc *client.Client) ([]string, error) {
	f := make(client.Filters).Add("label", managedLabel+"=true")
	res, err := dc.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	ids := make([]string, 0, len(res.Items))
	for _, c := range res.Items {
		id := c.Labels[stackIDLabel]
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
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

// createAndStart creates and starts the container for a service. The image must already have been
// resolved and pulled by the caller (Converge phase 1); createAndStart does not pull.
func createAndStart(ctx context.Context, dc *client.Client, stackID, projectName, svcName string, svc composetypes.ServiceConfig, image, hash string) error {
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
	restartPolicy, err := toRestartPolicy(svc.Restart)
	if err != nil {
		return err
	}
	hostCfg := &container.HostConfig{
		PortBindings:  portBindings,
		RestartPolicy: restartPolicy,
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

	pull, err := shouldPull(pullPolicy, present)
	if err != nil {
		return err
	}
	if !pull {
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

// imageID returns the local content-addressable ID of ref, or "" if it can't be determined (e.g. a
// build image that isn't present, or ref unset). Best-effort: when unknown, identity falls back to
// the tag string carried in the spec-hash. Feeding the ID into the hash is what lets a moved tag —
// same string, new content — force a recreate.
func imageID(ctx context.Context, dc *client.Client, ref string) string {
	if ref == "" {
		return ""
	}
	ins, err := dc.ImageInspect(ctx, ref)
	if err != nil {
		return ""
	}
	return ins.ID
}

// shouldPull decides whether to pull an image given the Compose pull_policy and whether the
// image is already present locally. An empty policy defaults to "missing" (Compose default).
// "never" and "build" never pull — for "never" a missing image lets ContainerCreate fail loudly,
// and "build" images are not fetched from a registry. An unknown value is a loud error, never a
// silent default: the time-based policies (refresh/daily/weekly/every_*) used to fall through and
// behave like "missing", the opposite of what they request.
func shouldPull(policy string, present bool) (bool, error) {
	switch policy {
	case composetypes.PullPolicyAlways:
		return true, nil
	case composetypes.PullPolicyNever, composetypes.PullPolicyBuild:
		return false, nil
	case "", composetypes.PullPolicyMissing, composetypes.PullPolicyIfNotPresent:
		return !present, nil
	default:
		return false, fmt.Errorf("unsupported pull_policy %q (time-based policies are not yet supported)", policy)
	}
}

// specHash is the container-identity fingerprint: a change recreates the container. imageID is the
// resolved content ID of svc.Image (see imageID); folding it in means a repointed tag recreates,
// where the tag string alone would not.
func specHash(svc composetypes.ServiceConfig, imageID string) string {
	key := struct {
		Image      string
		ImageID    string
		Cmd        []string
		Entrypoint []string
		Env        composetypes.MappingWithEquals
		Ports      []composetypes.ServicePortConfig
		WorkingDir string
		User       string
		Privileged bool
		Restart    string
	}{
		svc.Image, imageID, svc.Command, svc.Entrypoint, svc.Environment,
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

// toRestartPolicy maps a Compose `restart:` string onto a Docker restart policy. An unknown value
// is a loud error, never a silent default: previously `on-failure:5` (and any typo) fell through to
// "no", so the container never restarted despite asking it to.
func toRestartPolicy(policy string) (container.RestartPolicy, error) {
	switch {
	case policy == "", policy == "no":
		return container.RestartPolicy{Name: container.RestartPolicyDisabled}, nil
	case policy == "always":
		return container.RestartPolicy{Name: container.RestartPolicyAlways}, nil
	case policy == "unless-stopped":
		return container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}, nil
	case policy == "on-failure":
		return container.RestartPolicy{Name: container.RestartPolicyOnFailure}, nil
	case strings.HasPrefix(policy, "on-failure:"):
		// Docker only accepts a maximum retry count together with the on-failure policy.
		n, err := strconv.Atoi(strings.TrimPrefix(policy, "on-failure:"))
		if err != nil || n < 0 {
			return container.RestartPolicy{}, fmt.Errorf("invalid restart policy %q: on-failure count must be a non-negative integer", policy)
		}
		return container.RestartPolicy{Name: container.RestartPolicyOnFailure, MaximumRetryCount: n}, nil
	default:
		return container.RestartPolicy{}, fmt.Errorf("unsupported restart policy %q", policy)
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
