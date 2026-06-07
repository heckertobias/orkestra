// Package reconciler watches the assignments table and pushes ApplyDesiredState to connected Agents.
package reconciler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heckertobias/orkestra/internal/master/agentgw"
	mastermetrics "github.com/heckertobias/orkestra/internal/master/metrics"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// Reconciler polls the assignments table and pushes desired state to connected Agents.
type Reconciler struct {
	db       *pgxpool.Pool
	q        *store.Queries
	registry *agentgw.Registry
	interval time.Duration
}

// New creates a Reconciler that polls every interval.
func New(db *pgxpool.Pool, registry *agentgw.Registry, interval time.Duration) *Reconciler {
	return &Reconciler{
		db:       db,
		q:        store.New(db),
		registry: registry,
		interval: interval,
	}
}

// Run polls the DB and pushes desired state to each connected Agent. Blocks until ctx is done.
func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.push(ctx)
		}
	}
}

// PushNow triggers an immediate reconcile push (e.g. after a stack assignment change).
func (r *Reconciler) PushNow(ctx context.Context) {
	go r.push(ctx)
}

func (r *Reconciler) push(ctx context.Context) {
	// For each connected Agent, compute their desired state and send it.
	for _, agentID := range r.registry.ConnectedIDs() {
		state, err := r.buildDesiredState(ctx, agentID)
		if err != nil {
			slog.Error("build desired state", "agent_id", agentID, "err", err)
			continue
		}
		sess := r.registry.Get(agentID)
		if sess == nil {
			continue
		}
		sess.Send(&orkestraV1.MasterMessage{
			Payload: &orkestraV1.MasterMessage_ApplyDesiredState{
				ApplyDesiredState: state,
			},
		})
		mastermetrics.ReconcilePushTotal.Inc()
		slog.Debug("pushed desired state", "agent_id", agentID, "stacks", len(state.Stacks))
	}
}

func (r *Reconciler) buildDesiredState(ctx context.Context, serverID string) (*orkestraV1.ApplyDesiredState, error) {
	rows, err := r.q.ListAssignmentsForServer(ctx, serverID)
	if err != nil {
		return nil, err
	}

	stacks := make([]*orkestraV1.StackDesiredState, 0, len(rows))
	for _, row := range rows {
		status := desiredStatusFromString(row.DesiredStatus)

		var envVars map[string]string
		if row.EnvVars != nil {
			_ = json.Unmarshal(row.EnvVars, &envVars)
		}
		if envVars == nil {
			envVars = make(map[string]string)
		}

		stacks = append(stacks, &orkestraV1.StackDesiredState{
			StackId:     row.StackID,
			Version:     row.StackVersionID,
			ComposeYaml: row.ComposeYaml,
			EnvVars:     envVars,
			Status:      status,
		})
	}
	return &orkestraV1.ApplyDesiredState{Stacks: stacks}, nil
}

func desiredStatusFromString(s string) orkestraV1.DesiredStatus {
	switch s {
	case "running":
		return orkestraV1.DesiredStatus_DESIRED_STATUS_RUNNING
	case "stopped":
		return orkestraV1.DesiredStatus_DESIRED_STATUS_STOPPED
	case "removed":
		return orkestraV1.DesiredStatus_DESIRED_STATUS_REMOVED
	default:
		return orkestraV1.DesiredStatus_DESIRED_STATUS_UNSPECIFIED
	}
}
