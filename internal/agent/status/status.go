// Package status builds the StatusReport the agent sends to the Master: the inventory of
// orkestra-managed containers on this host, plus the outcome of the last reconcile per stack.
//
// The Master never dials an agent, so this report is the only way host state reaches it.
package status

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/moby/moby/client"

	"github.com/heckertobias/orkestra/internal/agent/compose"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// StackOutcome is what the last reconcile of one stack produced. It is the only part of the
// report that cannot be read back from Docker: a stack whose containers were never created
// leaves no trace on the host, so the failure has to be remembered.
type StackOutcome struct {
	Version       string            // stack_version_id the agent last tried to apply
	Err           string            // summary of the failure; empty on success
	ServiceErrors map[string]string // service name → error, for a partial failure
}

// Store holds the per-stack reconcile outcomes and turns them, together with a live container
// listing, into a StatusReport. It is safe for concurrent use: the reconcile loop writes, the
// connection's report ticker reads.
type Store struct {
	mu       sync.Mutex
	outcomes map[string]StackOutcome
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{outcomes: make(map[string]StackOutcome)}
}

// Set records the outcome of one stack's reconcile.
func (s *Store) Set(stackID string, o StackOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outcomes[stackID] = o
}

// Prune drops outcomes for stacks that are no longer in the desired state, so an unassigned
// stack stops being reported once its containers are gone.
func (s *Store) Prune(keep map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.outcomes {
		if _, ok := keep[id]; !ok {
			delete(s.outcomes, id)
		}
	}
}

func (s *Store) snapshot() map[string]StackOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]StackOutcome, len(s.outcomes))
	for k, v := range s.outcomes {
		out[k] = v
	}
	return out
}

// Report lists the managed containers on the host and merges them with the recorded outcomes.
// A Docker failure degrades to an outcome-only report rather than dropping the heartbeat — the
// Master uses the report's arrival to keep the server marked online.
func (s *Store) Report(ctx context.Context, dc *client.Client) *orkestraV1.StatusReport {
	outcomes := s.snapshot()

	var containers []compose.ManagedContainer
	if dc != nil {
		var err error
		containers, err = compose.ListManagedContainers(ctx, dc)
		if err != nil {
			slog.Error("list managed containers for status report", "err", err)
			containers = nil
		}
	}

	return &orkestraV1.StatusReport{
		Stacks:       buildStacks(ctx, dc, containers, outcomes),
		ReportedAtMs: time.Now().UnixMilli(),
	}
}

// buildStacks groups containers by stack and merges in the reconcile outcomes. Stacks are
// reported even when they have no containers left, so a stack that failed before anything was
// created still carries its error to the Master.
func buildStacks(
	ctx context.Context,
	dc *client.Client,
	containers []compose.ManagedContainer,
	outcomes map[string]StackOutcome,
) []*orkestraV1.StackStatus {
	byStack := make(map[string][]compose.ManagedContainer)
	for _, c := range containers {
		if c.StackID == "" {
			continue // managed but unattributable — nothing useful to report it under
		}
		byStack[c.StackID] = append(byStack[c.StackID], c)
	}

	stackIDs := make([]string, 0, len(byStack)+len(outcomes))
	for id := range byStack {
		stackIDs = append(stackIDs, id)
	}
	for id := range outcomes {
		if _, ok := byStack[id]; !ok {
			stackIDs = append(stackIDs, id)
		}
	}
	sort.Strings(stackIDs)

	stacks := make([]*orkestraV1.StackStatus, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		cs := byStack[stackID]
		sort.Slice(cs, func(i, j int) bool { return cs[i].Service < cs[j].Service })

		st := &orkestraV1.StackStatus{
			StackId:        stackID,
			RunningVersion: unanimousVersion(cs),
			Containers:     make([]*orkestraV1.ContainerStatus, 0, len(cs)),
		}
		for _, c := range cs {
			st.Containers = append(st.Containers, containerStatus(ctx, dc, c))
		}
		if o, ok := outcomes[stackID]; ok {
			st.Error = o.Err
			st.ServiceErrors = serviceErrors(o.ServiceErrors)
		}
		stacks = append(stacks, st)
	}
	return stacks
}

// unanimousVersion returns the stack version all containers agree on. A mid-rollout stack whose
// containers carry different versions has no single running version — report none rather than
// picking one arbitrarily.
func unanimousVersion(cs []compose.ManagedContainer) string {
	version := ""
	for i, c := range cs {
		if i == 0 {
			version = c.StackVersion
			continue
		}
		if c.StackVersion != version {
			return ""
		}
	}
	return version
}

// containerStatus converts one container, enriching it with the fields only an inspect carries:
// the restart count (the signal for crash-loop detection) and the start time. An inspect failure
// costs those two fields, not the container's presence in the report.
func containerStatus(ctx context.Context, dc *client.Client, c compose.ManagedContainer) *orkestraV1.ContainerStatus {
	cs := &orkestraV1.ContainerStatus{
		ContainerId: c.ID,
		ServiceName: c.Service,
		State:       c.State,
		Status:      c.Status,
		Health:      c.Health,
		Name:        c.Name,
		Image:       c.Image,
	}
	if dc == nil {
		return cs
	}
	res, err := dc.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
	if err != nil {
		slog.Debug("inspect container for status report", "id", c.ID, "err", err)
		return cs
	}
	insp := res.Container
	cs.RestartCount = int32(insp.RestartCount)
	if insp.State != nil {
		if t, err := time.Parse(time.RFC3339Nano, insp.State.StartedAt); err == nil && !t.IsZero() {
			cs.StartedAtMs = t.UnixMilli()
		}
		if cs.Health == "" && insp.State.Health != nil {
			cs.Health = healthFromInspect(string(insp.State.Health.Status))
		}
	}
	return cs
}

func healthFromInspect(s string) string {
	if s == "none" {
		return ""
	}
	return s
}

func serviceErrors(m map[string]string) []*orkestraV1.ServiceError {
	if len(m) == 0 {
		return nil
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]*orkestraV1.ServiceError, 0, len(names))
	for _, name := range names {
		out = append(out, &orkestraV1.ServiceError{ServiceName: name, Error: m[name]})
	}
	return out
}
