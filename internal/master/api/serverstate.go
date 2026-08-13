package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	masterauth "github.com/heckertobias/orkestra/internal/master/auth"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// GetServerState returns the container inventory the agent last reported for one server.
// The Master never dials an agent, so this is a read of the last StatusReport (agent_state),
// not a live query of the host — it is at most one heartbeat interval old.
func (h *StackServiceHandler) GetServerState(
	ctx context.Context,
	req *connect.Request[orkestraV1.GetServerStateRequest],
) (*connect.Response[orkestraV1.ServerState], error) {
	u := masterauth.UserFromContext(ctx)
	serverID := req.Msg.ServerId
	if !masterauth.CanViewServer(u, serverID) {
		return nil, errPermission("no access to this server")
	}

	report, err := h.agentState(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if report == nil {
		// Enrolled but never reported (or offline since before this feature): an empty
		// inventory is the honest answer, not an error.
		return connect.NewResponse(&orkestraV1.ServerState{ServerId: serverID}), nil
	}

	state := serverStateFromReport(serverID, report, func(stackID string) bool {
		return masterauth.CanViewOn(u, serverID, stackID)
	})
	h.resolveStackMetadata(ctx, state)
	return connect.NewResponse(state), nil
}

// agentState loads and decodes the stored StatusReport for a server. A server that has never
// reported yields (nil, nil).
func (h *StackServiceHandler) agentState(ctx context.Context, serverID string) (*orkestraV1.StatusReport, error) {
	row, err := h.q.GetAgentState(ctx, serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get agent state: %w", err))
	}
	var report orkestraV1.StatusReport
	if err := protojson.Unmarshal(row.StateJson, &report); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("decode agent state: %w", err))
	}
	return &report, nil
}

// serverStateFromReport converts a reported StatusReport into the UI-facing ServerState,
// dropping stacks the caller may not view. Stack names and numeric versions are filled in
// afterwards by resolveStackMetadata — this function stays free of database access so it can
// be tested on its own.
func serverStateFromReport(
	serverID string,
	report *orkestraV1.StatusReport,
	canView func(stackID string) bool,
) *orkestraV1.ServerState {
	state := &orkestraV1.ServerState{
		ServerId:   serverID,
		ReportedAt: report.ReportedAtMs,
		Stacks:     make([]*orkestraV1.StackState, 0, len(report.Stacks)),
	}
	for _, s := range report.Stacks {
		if canView != nil && !canView(s.StackId) {
			continue
		}
		st := &orkestraV1.StackState{
			StackId:          s.StackId,
			RunningVersionId: s.RunningVersion,
			DriftDetected:    s.DriftDetected,
			DriftDescription: s.DriftDescription,
			Error:            s.Error,
			ServiceErrors:    s.ServiceErrors,
			Containers:       make([]*orkestraV1.ContainerState, 0, len(s.Containers)),
		}
		for _, c := range s.Containers {
			st.Containers = append(st.Containers, &orkestraV1.ContainerState{
				Id:           c.ContainerId,
				Name:         c.Name,
				Image:        c.Image,
				ServiceName:  c.ServiceName,
				State:        c.State,
				Status:       c.Status,
				Health:       c.Health,
				RestartCount: c.RestartCount,
				StartedAt:    c.StartedAtMs,
			})
		}
		state.Stacks = append(state.Stacks, st)
	}
	return state
}

// resolveStackMetadata fills in the stack name and the numeric version for each reported stack.
// The agent only knows IDs; a deleted stack or an unknown version simply stays blank rather than
// failing the whole read.
func (h *StackServiceHandler) resolveStackMetadata(ctx context.Context, state *orkestraV1.ServerState) {
	for _, s := range state.Stacks {
		if row, err := h.q.GetStack(ctx, s.StackId); err == nil {
			s.StackName = row.Name
		}
		if s.RunningVersionId == "" {
			continue
		}
		if row, err := h.q.GetStackVersion(ctx, s.RunningVersionId); err == nil {
			s.RunningVersion = int32(row.Version)
		}
	}
}

// containerStackID resolves which stack a container belongs to, using the inventory the agent
// reported. Returns "" when the container is unknown to the Master — the caller must then fall
// back to a server-wide permission check rather than assuming a stack.
func (h *StackServiceHandler) containerStackID(ctx context.Context, serverID, containerID string) string {
	report, err := h.agentState(ctx, serverID)
	if err != nil || report == nil {
		if err != nil {
			slog.Debug("resolve container stack", "server_id", serverID, "err", err)
		}
		return ""
	}
	for _, s := range report.Stacks {
		for _, c := range s.Containers {
			if c.ContainerId == containerID {
				return s.StackId
			}
		}
	}
	return ""
}
