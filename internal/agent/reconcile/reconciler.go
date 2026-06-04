// Package reconcile implements the Agent-side reconcile loop.
// It receives ApplyDesiredState from the Master and converges containers toward it.
package reconcile

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/client"

	"github.com/heckertobias/orkestra/internal/agent/compose"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// Reconciler applies desired state deltas from the Master to local Docker.
type Reconciler struct {
	dc      *client.Client
	desired chan *orkestraV1.ApplyDesiredState
	mu      sync.Mutex
	last    *orkestraV1.ApplyDesiredState
}

// New creates a Reconciler backed by the given Docker client.
func New(dc *client.Client) *Reconciler {
	return &Reconciler{
		dc:      dc,
		desired: make(chan *orkestraV1.ApplyDesiredState, 4),
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
	for _, stack := range state.Stacks {
		if err := r.reconcileStack(ctx, stack); err != nil {
			slog.Error("reconcile stack failed",
				"stack_id", stack.StackId,
				"err", err,
			)
		}
	}
}

func (r *Reconciler) reconcileStack(ctx context.Context, s *orkestraV1.StackDesiredState) error {
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
		return compose.Converge(ctx, r.dc, s.StackId, proj)

	default:
		slog.Warn("unknown desired status", "stack_id", s.StackId, "status", s.Status)
		return nil
	}
}
