// Package reconcile implements the Agent-side reconcile loop.
// It receives ApplyDesiredState from the Master and converges containers toward it.
package reconcile

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/moby/moby/client"

	"github.com/heckertobias/orkestra/internal/agent/compose"
	agentmetrics "github.com/heckertobias/orkestra/internal/agent/metrics"
	"github.com/heckertobias/orkestra/internal/agent/status"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// Reconciler applies desired state deltas from the Master to local Docker.
type Reconciler struct {
	dc      *client.Client
	desired chan *orkestraV1.ApplyDesiredState
	mu      sync.Mutex
	last    *orkestraV1.ApplyDesiredState
	// status records the outcome of each reconcile so the agent can report failures that leave
	// no trace on the host. May be nil (tests).
	status *status.Store
}

// New creates a Reconciler backed by the given Docker client. Reconcile outcomes are recorded
// in st, which may be nil when no status reporting is wired up.
func New(dc *client.Client, st *status.Store) *Reconciler {
	return &Reconciler{
		dc:      dc,
		desired: make(chan *orkestraV1.ApplyDesiredState, 4),
		status:  st,
	}
}

// Apply enqueues a new desired state push from the Master.
func (r *Reconciler) Apply(state *orkestraV1.ApplyDesiredState) {
	select {
	case r.desired <- state:
	default:
		// Replace the pending state if the channel is full (latest wins).
		select {
		case <-r.desired:
		default:
		}
		r.desired <- state
	}
}

// Run starts the reconcile loop. It processes incoming ApplyDesiredState messages
// and re-syncs every 30 seconds. Blocks until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case state := <-r.desired:
			r.mu.Lock()
			r.last = state
			r.mu.Unlock()
			r.reconcile(ctx, state)
		case <-ticker.C:
			r.mu.Lock()
			last := r.last
			r.mu.Unlock()
			if last != nil {
				r.reconcile(ctx, last)
			}
		}
	}
}

func (r *Reconciler) reconcile(ctx context.Context, state *orkestraV1.ApplyDesiredState) {
	desired := make(map[string]struct{}, len(state.Stacks))
	for _, stack := range state.Stacks {
		desired[stack.StackId] = struct{}{}
		err := r.reconcileStack(ctx, stack)
		if err != nil {
			agentmetrics.ReconcileErrorsTotal.WithLabelValues(stack.StackId).Inc()
			slog.Error("reconcile stack failed",
				"stack_id", stack.StackId,
				"err", err,
			)
		}
		r.recordOutcome(stack, err)
	}
	// The Master pushes the full desired state, never a diff: any managed stack still on the
	// host but absent from this push has been unassigned or deleted and must be removed.
	r.removeAbsentStacks(ctx, desired)
	if r.status != nil {
		r.status.Prune(desired)
	}
}

// recordOutcome stores what this stack's reconcile produced, so the next status report can carry
// it to the Master. A converge that failed per service is unpacked into the per-service detail;
// any other error is reported as a single stack-level failure.
func (r *Reconciler) recordOutcome(stack *orkestraV1.StackDesiredState, err error) {
	if r.status == nil {
		return
	}
	o := status.StackOutcome{Version: stack.Version}
	if err != nil {
		o.Err = err.Error()
		var ce *compose.ConvergeError
		if errors.As(err, &ce) {
			o.ServiceErrors = make(map[string]string, len(ce.Services))
			for _, f := range ce.Services {
				o.ServiceErrors[f.Service] = f.Err.Error()
			}
		}
	}
	r.status.Set(stack.StackId, o)
}

// removeAbsentStacks stops and removes every orkestra-managed stack on the host whose ID is not
// present in the current desired state. An empty desired set legitimately removes everything —
// the Master always builds the push from the full assignment set, so an empty push means the
// server has no assignments.
func (r *Reconciler) removeAbsentStacks(ctx context.Context, desired map[string]struct{}) {
	if r.dc == nil {
		return
	}
	managed, err := compose.ListManagedStackIDs(ctx, r.dc)
	if err != nil {
		slog.Error("list managed stacks for pruning", "err", err)
		return
	}
	for _, stackID := range managed {
		if _, ok := desired[stackID]; ok {
			continue
		}
		slog.Info("removing stack absent from desired state", "stack_id", stackID)
		if err := compose.Remove(ctx, r.dc, stackID); err != nil {
			agentmetrics.ReconcileErrorsTotal.WithLabelValues(stackID).Inc()
			slog.Error("remove absent stack failed", "stack_id", stackID, "err", err)
		}
	}
}

func (r *Reconciler) reconcileStack(ctx context.Context, s *orkestraV1.StackDesiredState) error {
	start := time.Now()
	defer func() {
		agentmetrics.ReconcileDuration.Observe(time.Since(start).Seconds())
	}()
	if r.dc == nil {
		slog.Warn("docker client not available, skipping reconcile", "stack_id", s.StackId)
		return nil
	}
	switch s.Status {
	case orkestraV1.DesiredStatus_DESIRED_STATUS_REMOVED:
		slog.Info("removing stack", "stack_id", s.StackId)
		return compose.Remove(ctx, r.dc, s.StackId)

	case orkestraV1.DesiredStatus_DESIRED_STATUS_STOPPED:
		slog.Info("stopping stack", "stack_id", s.StackId)
		return compose.Stop(ctx, r.dc, s.StackId)

	case orkestraV1.DesiredStatus_DESIRED_STATUS_RUNNING:
		slog.Info("converging stack", "stack_id", s.StackId, "version", s.Version)
		envVars := make(map[string]string)
		for k, v := range s.EnvVars {
			envVars[k] = v
		}
		proj, err := compose.LoadProject(s.ComposeYaml, s.StackId, envVars)
		if err != nil {
			return err
		}
		return compose.Converge(ctx, r.dc, s.StackId, s.Version, proj)

	default:
		slog.Warn("unknown desired status", "stack_id", s.StackId, "status", s.Status)
		return nil
	}
}
