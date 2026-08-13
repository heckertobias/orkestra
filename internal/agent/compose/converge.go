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

// Labels stamped onto every container orkestra creates. They are the only link between a
// running container and the desired state that produced it — container identity, stack
// pruning and the reported inventory all hinge on them.
const (
	LabelSpecHash = "orkestra.spec-hash"
	LabelStackID  = "orkestra.stack-id"
	LabelService  = "orkestra.service"
	LabelManaged  = "orkestra.managed"
	// LabelStackVersion records the stack_version_id the container was created from, so the
	// agent can report which version is actually running rather than which one was pushed.
	LabelStackVersion = "orkestra.stack-version"
)

// Converge reconciles actual Docker state toward the desired Compose project. version is the
// stack_version_id being applied; it is stamped onto every container this call creates.
func Converge(ctx context.Context, dc *client.Client, stackID, version string, proj *composetypes.Project) error {
	existing, err := listStackContainers(ctx, dc, stackID)
	if err != nil {
		return fmt.Errorf("list existing containers: %w", err)
	}
	existingByService := make(map[string]containerSummary)
	for _, c := range existing {
		svc := c.Labels[LabelService]
		existingByService[svc] = c
	}

	var svcErrs []ServiceFailure
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
			svcErrs = append(svcErrs, ServiceFailure{Service: svcName, Err: err})
			continue
		}

		// Phase 2: hash over the resolved image ID, so a moved tag (same string, new content)
		// changes identity and forces a recreate — a plain tag string never would.
		desired := specHash(svc, imageID(ctx, dc, image))

		// Phase 3: compare, then remove + recreate only if drifted.
		if exists {
			if cur.Labels[LabelSpecHash] == desired && cur.State == "running" {
				slog.Debug("service up-to-date", "stack", stackID, "service", svcName)
				continue
			}
			slog.Info("recreating container", "stack", stackID, "service", svcName)
			_ = removeContainer(ctx, dc, cur.ID)
		}

		slog.Info("creating container", "stack", stackID, "service", svcName)
		if err := createAndStart(ctx, dc, stackID, version, proj.Name, svcName, svc, image, desired); err != nil {
			// Partial failure: record and keep reconciling the rest of the stack. Aborting here
			// would leave every alphabetically-later service and the orphan cleanup untouched.
			slog.Error("create container failed", "stack", stackID, "service", svcName, "err", err)
			svcErrs = append(svcErrs, ServiceFailure{Service: svcName, Err: err})
			continue
		}
	}

	// Orphan removal always runs, even after a service failure above.
	for svcName, c := range existingByService {
		slog.Info("removing orphan", "stack", stackID, "service", svcName)
		_ = removeContainer(ctx, dc, c.ID)
	}

	if len(svcErrs) > 0 {
		return &ConvergeError{StackID: stackID, Services: svcErrs}
	}
	return nil
}

// ServiceFailure is one service that failed inside a stack reconcile.
type ServiceFailure struct {
	Service string
	Err     error
}

// ConvergeError reports which services of a stack failed. Converge keeps reconciling after a
// service fails, so a failed reconcile is a list, not a single cause — the agent reports the
// per-service detail so the UI can point at the service instead of the whole stack.
type ConvergeError struct {
	StackID  string
	Services []ServiceFailure
}

func (e *ConvergeError) Error() string {
	errs := make([]error, 0, len(e.Services))
	for _, f := range e.Services {
		errs = append(errs, fmt.Errorf("%s: %w", f.Service, f.Err))
	}
	return fmt.Sprintf("converge %s: %d service(s) failed: %v", e.StackID, len(e.Services), errors.Join(errs...))
}

// Unwrap exposes the underlying causes to errors.Is/errors.As.
func (e *ConvergeError) Unwrap() []error {
	errs := make([]error, 0, len(e.Services))
	for _, f := range e.Services {
		errs = append(errs, f.Err)
	}
	return errs
}

// ListManagedStackIDs returns the distinct stack IDs of every orkestra-managed container on the
// host. It is used to detect stacks that have disappeared from the desired state and must be
// removed, since the Master always pushes the full desired state rather than a diff.
func ListManagedStackIDs(ctx context.Context, dc *client.Client) ([]string, error) {
	f := make(client.Filters).Add("label", LabelManaged+"=true")
	res, err := dc.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	ids := make([]string, 0, len(res.Items))
	for _, c := range res.Items {
		id := c.Labels[LabelStackID]
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

// ManagedContainer is one orkestra-managed container as reported to the Master. It carries the
// summary fields of a container list; restart count and start time need an inspect on top.
type ManagedContainer struct {
	ID           string
	Name         string // no leading slash
	Image        string
	StackID      string
	Service      string
	SpecHash     string
	StackVersion string
	State        string // running | exited | restarting | ...
	Status       string // "Up 3 hours"
	Health       string // "" (no healthcheck) | starting | healthy | unhealthy
}

// ListManagedContainers returns every orkestra-managed container on the host, in any state.
// It is the source of the inventory the agent reports in its StatusReport.
func ListManagedContainers(ctx context.Context, dc *client.Client) ([]ManagedContainer, error) {
	f := make(client.Filters).Add("label", LabelManaged+"=true")
	res, err := dc.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	out := make([]ManagedContainer, 0, len(res.Items))
	for _, c := range res.Items {
		mc := ManagedContainer{
			ID:           c.ID,
			Image:        c.Image,
			StackID:      c.Labels[LabelStackID],
			Service:      c.Labels[LabelService],
			SpecHash:     c.Labels[LabelSpecHash],
			StackVersion: c.Labels[LabelStackVersion],
			State:        string(c.State),
			Status:       c.Status,
		}
		if len(c.Names) > 0 {
			mc.Name = strings.TrimPrefix(c.Names[0], "/")
		}
		if c.Health != nil {
			mc.Health = healthStatus(string(c.Health.Status))
		}
		out = append(out, mc)
	}
	return out, nil
}

// healthStatus normalises Docker's health status: a container without a healthcheck reports
// "none", which is noise on the wire and in the UI — report it as empty instead.
func healthStatus(s string) string {
	if s == "none" {
		return ""
	}
	return s
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
		Add("label", LabelManaged+"=true").
		Add("label", LabelStackID+"="+stackID)
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
func createAndStart(ctx context.Context, dc *client.Client, stackID, version, projectName, svcName string, svc composetypes.ServiceConfig, image, hash string) error {
	labels := map[string]string{
		LabelManaged:                 "true",
		LabelStackID:                 stackID,
		LabelService:                 svcName,
		LabelSpecHash:                hash,
		LabelStackVersion:            version,
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
	binds, err := buildBinds(svc.Volumes)
	if err != nil {
		return err
	}
	hostCfg := &container.HostConfig{
		PortBindings:  portBindings,
		RestartPolicy: restartPolicy,
		Binds:         binds,
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

// buildBinds converts a service's volumes into Docker bind-mount strings. Only bind mounts are
// supported today; any other mount type is a loud error, never a silent drop. Silently dropping a
// named volume is the #70 data-loss bug: the service then writes into the container's writable layer
// and loses everything on the next recreate. Named and tmpfs volume support is tracked in #11.
func buildBinds(vols []composetypes.ServiceVolumeConfig) ([]string, error) {
	binds := make([]string, 0, len(vols))
	for _, v := range vols {
		if v.Type != composetypes.VolumeTypeBind {
			target := v.Target
			if target == "" {
				target = v.Source
			}
			kind := v.Type
			switch {
			case kind == composetypes.VolumeTypeVolume && v.Source != "":
				kind = "named volume"
			case kind == "":
				kind = "non-bind"
			}
			return nil, fmt.Errorf("volume %q is not supported: only bind mounts work today, got a %s mount (named/tmpfs volumes: #11)", target, kind)
		}
		if v.Source == "" {
			return nil, fmt.Errorf("bind mount %q has no host source path", v.Target)
		}
		bind := v.Source + ":" + v.Target
		if v.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}
	return binds, nil
}
