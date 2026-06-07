// Package api implements the Connect RPC handlers for the UI API.
package api

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heckertobias/orkestra/internal/master/agentgw"
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

// ListServers returns all non-deleted servers merged with live connection state.
func (h *StackServiceHandler) ListServers(ctx context.Context, req *connect.Request[orkestraV1.ListServersRequest]) (*connect.Response[orkestraV1.ListServersResponse], error) {
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
		status := row.Status
		if connected[row.ID] && status != "online" {
			status = "online"
		}
		servers = append(servers, serverFromRow(row, status))
	}
	return connect.NewResponse(&orkestraV1.ListServersResponse{Servers: servers}), nil
}

// GetServer returns a single server by ID.
func (h *StackServiceHandler) GetServer(ctx context.Context, req *connect.Request[orkestraV1.GetServerRequest]) (*connect.Response[orkestraV1.Server], error) {
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

// UpdateServer updates server name and labels.
func (h *StackServiceHandler) UpdateServer(ctx context.Context, req *connect.Request[orkestraV1.UpdateServerRequest]) (*connect.Response[orkestraV1.Server], error) {
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

// DeleteServer soft-deletes a server.
func (h *StackServiceHandler) DeleteServer(ctx context.Context, req *connect.Request[orkestraV1.DeleteServerRequest]) (*connect.Response[orkestraV1.Empty], error) {
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

func (h *StackServiceHandler) StreamLogs(_ context.Context, req *connect.Request[orkestraV1.StreamLogsRequest], stream *connect.ServerStream[orkestraV1.LogLine]) error {
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("stream bridging implemented in M2 integration"))
}

func (h *StackServiceHandler) StreamStats(_ context.Context, req *connect.Request[orkestraV1.StreamStatsRequest], stream *connect.ServerStream[orkestraV1.ServerStats]) error {
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("stream bridging implemented in M2 integration"))
}

// StreamEvents polls the events table and streams new events to the client.
// It sends the last 20 events on connect, then polls for new events every 2 seconds.
func (h *StackServiceHandler) StreamEvents(ctx context.Context, req *connect.Request[orkestraV1.StreamEventsRequest], stream *connect.ServerStream[orkestraV1.Event]) error {
	filter := store.ListEventsFilteredParams{
		ServerID: ptrStringIfNonEmpty(req.Msg.ServerId),
		StackID:  ptrStringIfNonEmpty(req.Msg.StackId),
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
				ID:       lastID,
				ServerID: filter.ServerID,
				StackID:  filter.StackID,
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
