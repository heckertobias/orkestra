// Package api implements the Connect RPC handlers for the UI API.
package api

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heckertobias/orkestra/internal/master/agentgw"
	masterauth "github.com/heckertobias/orkestra/internal/master/auth"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// EventEmitter allows other packages to emit events via the StackService.
type EventEmitter interface {
	EmitEvent(ctx context.Context, p store.InsertEventParams)
}

// StackServiceHandler implements the UI-facing StackService RPC handlers.
type StackServiceHandler struct {
	db           *pgxpool.Pool
	q            *store.Queries
	registry     *agentgw.Registry
	reconcilerFn func() // called after mutations that affect desired state
}

// NewStackServiceHandler creates a StackServiceHandler.
func NewStackServiceHandler(db *pgxpool.Pool, registry *agentgw.Registry, reconcilerFn func()) *StackServiceHandler {
	return &StackServiceHandler{db: db, q: store.New(db), registry: registry, reconcilerFn: reconcilerFn}
}

// assignedServerIDs returns the distinct server IDs a stack is assigned to.
// Returns an empty slice if the stack is unassigned (any operator may then manage it).
func (h *StackServiceHandler) assignedServerIDs(ctx context.Context, stackID string) []string {
	rows, err := h.q.ListAssignmentsForStack(ctx, stackID)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ServerID)
	}
	return ids
}

// errPermission returns a standardised PermissionDenied Connect error.
func errPermission(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("%s", msg))
}

// ListServers returns all non-deleted servers, filtered to those the caller may view.
func (h *StackServiceHandler) ListServers(ctx context.Context, req *connect.Request[orkestraV1.ListServersRequest]) (*connect.Response[orkestraV1.ListServersResponse], error) {
	u := masterauth.UserFromContext(ctx)
	rows, err := h.q.ListServers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list servers: %w", err))
	}

	connected := make(map[string]bool)
	for _, id := range h.registry.ConnectedIDs() {
		connected[id] = true
	}

	servers := make([]*orkestraV1.Server, 0, len(rows))
	for _, row := range rows {
		if !masterauth.CanViewServer(u, row.ID) {
			continue
		}
		status := row.Status
		if connected[row.ID] && status != "online" {
			status = "online"
		}
		srv := serverFromRow(row, status)
		if asgns, err := h.q.ListAssignmentsForServer(ctx, row.ID); err == nil {
			for _, a := range asgns {
				srv.Assignments = append(srv.Assignments, &orkestraV1.Assignment{
					Id:             a.ID,
					ServerId:       a.ServerID,
					StackId:        a.StackID,
					StackVersionId: a.StackVersionID,
					DesiredStatus:  a.DesiredStatus,
					AssignedAt:     a.AssignedAt,
				})
			}
		}
		servers = append(servers, srv)
	}
	return connect.NewResponse(&orkestraV1.ListServersResponse{Servers: servers}), nil
}

// GetServer returns a single server by ID.
func (h *StackServiceHandler) GetServer(ctx context.Context, req *connect.Request[orkestraV1.GetServerRequest]) (*connect.Response[orkestraV1.Server], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanViewServer(u, req.Msg.Id) {
		return nil, errPermission("no access to this server")
	}
	row, err := h.q.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	status := row.Status
	if h.registry.Get(row.ID) != nil {
		status = "online"
	}
	return connect.NewResponse(serverFromRow(row, status)), nil
}

// UpdateServer updates server name and labels (admin only).
func (h *StackServiceHandler) UpdateServer(ctx context.Context, req *connect.Request[orkestraV1.UpdateServerRequest]) (*connect.Response[orkestraV1.Server], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.IsAdmin(u) {
		return nil, errPermission("server metadata changes require admin role")
	}
	labelsJSON, err := labelsToJSON(req.Msg.Labels)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row, err := h.q.UpdateServer(ctx, store.UpdateServerParams{
		Name:   req.Msg.Name,
		Labels: labelsJSON,
		ID:     req.Msg.Id,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update server: %w", err))
	}
	return connect.NewResponse(serverFromRow(row, row.Status)), nil
}

// DeleteServer soft-deletes a server (admin only).
func (h *StackServiceHandler) DeleteServer(ctx context.Context, req *connect.Request[orkestraV1.DeleteServerRequest]) (*connect.Response[orkestraV1.Empty], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.IsAdmin(u) {
		return nil, errPermission("deleting a server requires admin role")
	}
	if err := h.q.SoftDeleteServer(ctx, store.SoftDeleteServerParams{
		DeletedAt: ptrInt64(time.Now().UnixMilli()),
		ID:        req.Msg.Id,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete server: %w", err))
	}
	return connect.NewResponse(&orkestraV1.Empty{}), nil
}

// Stack CRUD implementations are in stacks_crud.go.

func (h *StackServiceHandler) ExecOnContainer(ctx context.Context, req *connect.Request[orkestraV1.ExecOnContainerRequest]) (*connect.Response[orkestraV1.ExecOnContainerResponse], error) {
	u := masterauth.UserFromContext(ctx)
	// Container→Stack resolution is not yet available (agent_state not populated).
	// Use server-level check: operator access on the server (stack_id unset = any stack).
	if !masterauth.CanOperateOn(u, req.Msg.ServerId, "") {
		return nil, errPermission("operator access required on this server")
	}
	sess := h.registry.Get(req.Msg.ServerId)
	if sess == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("server not connected"))
	}
	// Forward ExecCommand to Agent via stream.
	sess.Send(&orkestraV1.MasterMessage{
		Payload: &orkestraV1.MasterMessage_ExecCommand{
			ExecCommand: &orkestraV1.ExecCommand{
				ContainerId: req.Msg.ContainerId,
				Type:        commandTypeFromString(req.Msg.CommandType),
				Args:        req.Msg.Args,
			},
		},
	})
	return connect.NewResponse(&orkestraV1.ExecOnContainerResponse{Success: true}), nil
}

func (h *StackServiceHandler) StreamLogs(ctx context.Context, req *connect.Request[orkestraV1.StreamLogsRequest], stream *connect.ServerStream[orkestraV1.LogLine]) error {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanViewOn(u, req.Msg.ServerId, "") {
		return errPermission("viewer access required on this server")
	}
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("stream bridging implemented in M2 integration"))
}

func (h *StackServiceHandler) StreamStats(ctx context.Context, req *connect.Request[orkestraV1.StreamStatsRequest], stream *connect.ServerStream[orkestraV1.ServerStats]) error {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanViewServer(u, req.Msg.ServerId) {
		return errPermission("viewer access required on this server")
	}
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("stream bridging implemented in M2 integration"))
}

// StreamEvents polls the events table and streams new events to the client.
// It sends the last 20 events on connect, then polls for new events every 2 seconds.
func (h *StackServiceHandler) StreamEvents(ctx context.Context, req *connect.Request[orkestraV1.StreamEventsRequest], stream *connect.ServerStream[orkestraV1.Event]) error {
	u := masterauth.UserFromContext(ctx)
	if req.Msg.ServerId != "" && !masterauth.CanViewServer(u, req.Msg.ServerId) {
		return errPermission("viewer access required on this server")
	}

	serverID := req.Msg.ServerId
	stackID := req.Msg.StackId
	filter := store.ListEventsFilteredParams{
		ServerID: serverID,
		StackID:  stackID,
	}

	// Send recent history first.
	rows, err := h.q.ListEventsFiltered(ctx, filter)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("list events: %w", err))
	}
	var lastID int64
	for i := len(rows) - 1; i >= 0; i-- {
		ev := rows[i]
		if err := stream.Send(eventToProto(ev)); err != nil {
			return err
		}
		if ev.ID > lastID {
			lastID = ev.ID
		}
	}

	// Poll for new events.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			newRows, err := h.q.ListEventsAfter(ctx, store.ListEventsAfterParams{
				AfterID:  lastID,
				ServerID: serverID,
				StackID:  stackID,
			})
			if err != nil {
				continue
			}
			for _, ev := range newRows {
				if err := stream.Send(eventToProto(ev)); err != nil {
					return err
				}
				if ev.ID > lastID {
					lastID = ev.ID
				}
			}
		}
	}
}

func ptrStringIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func eventToProto(e store.Event) *orkestraV1.Event {
	ev := &orkestraV1.Event{
		Id:        e.ID,
		Ts:        e.Ts,
		EventType: e.EventType,
		Severity:  e.Severity,
		Message:   e.Message,
	}
	if e.ServerID != nil {
		ev.ServerId = *e.ServerID
	}
	if e.StackID != nil {
		ev.StackId = *e.StackID
	}
	if e.DetailJson != nil {
		ev.DetailJson = string(e.DetailJson)
	}
	return ev
}
