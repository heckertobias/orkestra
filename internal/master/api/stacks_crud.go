package api

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// CreateStack creates a new stack with an initial version.
func (h *StackServiceHandler) CreateStack(ctx context.Context, req *connect.Request[orkestraV1.CreateStackRequest]) (*connect.Response[orkestraV1.Stack], error) {
	r := req.Msg
	if r.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	stackID := uuid.NewString()
	now := time.Now().UnixMilli()

	row, err := h.q.InsertStack(ctx, store.InsertStackParams{
		ID:          stackID,
		Name:        r.Name,
		Description: ptrString(r.Description),
		Owner:       nil,
		CreatedAt:   now,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create stack: %w", err))
	}

	// Create initial version if compose YAML provided.
	if r.ComposeYaml != "" {
		envJSON, _ := labelsToJSON(envVarsToLabels(r.EnvVars))
		_, err = h.q.InsertStackVersion(ctx, store.InsertStackVersionParams{
			ID:          uuid.NewString(),
			StackID:     stackID,
			Version:     1,
			ComposeYaml: r.ComposeYaml,
			EnvVars:     envJSON,
			SecretRefs:  []byte("[]"),
			CreatedBy:   nil,
			CreatedAt:   now,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create stack version: %w", err))
		}
	}

	return connect.NewResponse(stackFromRow(row, 1)), nil
}

// UpdateStack creates a new immutable version for an existing stack.
func (h *StackServiceHandler) UpdateStack(ctx context.Context, req *connect.Request[orkestraV1.UpdateStackRequest]) (*connect.Response[orkestraV1.StackVersion], error) {
	r := req.Msg
	nextVer, err := h.q.GetNextVersionNumber(ctx, r.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("stack not found"))
	}
	envJSON, _ := labelsToJSON(envVarsToLabels(r.EnvVars))
	versionID := uuid.NewString()
	now := time.Now().UnixMilli()

	_, err = h.q.InsertStackVersion(ctx, store.InsertStackVersionParams{
		ID:          versionID,
		StackID:     r.Id,
		Version:     int64(nextVer),
		ComposeYaml: r.ComposeYaml,
		EnvVars:     envJSON,
		SecretRefs:  []byte("[]"),
		CreatedBy:   nil,
		CreatedAt:   now,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create version: %w", err))
	}

	// Trigger reconciler push if available.
	if h.reconcilerFn != nil {
		h.reconcilerFn()
	}

	return connect.NewResponse(&orkestraV1.StackVersion{
		Id:          versionID,
		StackId:     r.Id,
		Version:     int32(nextVer),
		ComposeYaml: r.ComposeYaml,
		CreatedAt:   now,
	}), nil
}

// GetStack returns a stack by ID.
func (h *StackServiceHandler) GetStack(ctx context.Context, req *connect.Request[orkestraV1.GetStackRequest]) (*connect.Response[orkestraV1.Stack], error) {
	row, err := h.q.GetStack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("stack not found"))
	}
	latest, _ := h.q.GetLatestStackVersion(ctx, row.ID)
	return connect.NewResponse(stackFromRow(row, int32(latest.Version))), nil
}

// ListStacks returns all non-deleted stacks.
func (h *StackServiceHandler) ListStacks(ctx context.Context, _ *connect.Request[orkestraV1.ListStacksRequest]) (*connect.Response[orkestraV1.ListStacksResponse], error) {
	rows, err := h.q.ListStacks(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list stacks: %w", err))
	}
	stacks := make([]*orkestraV1.Stack, 0, len(rows))
	for _, row := range rows {
		latest, _ := h.q.GetLatestStackVersion(ctx, row.ID)
		stacks = append(stacks, stackFromRow(row, int32(latest.Version)))
	}
	return connect.NewResponse(&orkestraV1.ListStacksResponse{Stacks: stacks}), nil
}

// DeleteStack soft-deletes a stack.
func (h *StackServiceHandler) DeleteStack(ctx context.Context, req *connect.Request[orkestraV1.DeleteStackRequest]) (*connect.Response[orkestraV1.Empty], error) {
	if err := h.q.SoftDeleteStack(ctx, store.SoftDeleteStackParams{
		DeletedAt: ptrInt64(time.Now().UnixMilli()),
		ID:        req.Msg.Id,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete stack: %w", err))
	}
	return connect.NewResponse(&orkestraV1.Empty{}), nil
}

// ListStackVersions returns all versions for a stack.
func (h *StackServiceHandler) ListStackVersions(ctx context.Context, req *connect.Request[orkestraV1.ListStackVersionsRequest]) (*connect.Response[orkestraV1.ListStackVersionsResponse], error) {
	rows, err := h.q.ListStackVersions(ctx, req.Msg.StackId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list versions: %w", err))
	}
	versions := make([]*orkestraV1.StackVersion, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, &orkestraV1.StackVersion{
			Id:          row.ID,
			StackId:     row.StackID,
			Version:     int32(row.Version),
			ComposeYaml: row.ComposeYaml,
			CreatedAt:   row.CreatedAt,
		})
	}
	return connect.NewResponse(&orkestraV1.ListStackVersionsResponse{Versions: versions}), nil
}

// AssignStack assigns a stack version to a server and triggers reconciliation.
func (h *StackServiceHandler) AssignStack(ctx context.Context, req *connect.Request[orkestraV1.AssignStackRequest]) (*connect.Response[orkestraV1.Assignment], error) {
	r := req.Msg
	versionID := r.StackVersionId
	if versionID == "" {
		latest, err := h.q.GetLatestStackVersion(ctx, r.StackId)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no versions for stack"))
		}
		versionID = latest.ID
	}

	status := r.DesiredStatus
	if status == "" {
		status = "running"
	}

	row, err := h.q.UpsertAssignment(ctx, store.UpsertAssignmentParams{
		ID:             uuid.NewString(),
		ServerID:       r.ServerId,
		StackID:        r.StackId,
		StackVersionID: versionID,
		DesiredStatus:  status,
		AssignedBy:     nil,
		AssignedAt:     time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("assign stack: %w", err))
	}

	if h.reconcilerFn != nil {
		h.reconcilerFn()
	}

	return connect.NewResponse(&orkestraV1.Assignment{
		Id:             row.ID,
		ServerId:       row.ServerID,
		StackId:        row.StackID,
		StackVersionId: row.StackVersionID,
		DesiredStatus:  row.DesiredStatus,
		AssignedAt:     row.AssignedAt,
	}), nil
}

// UnassignStack removes a stack assignment.
func (h *StackServiceHandler) UnassignStack(ctx context.Context, req *connect.Request[orkestraV1.UnassignStackRequest]) (*connect.Response[orkestraV1.Empty], error) {
	if err := h.q.DeleteAssignment(ctx, store.DeleteAssignmentParams{
		ServerID: req.Msg.ServerId,
		StackID:  req.Msg.StackId,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unassign stack: %w", err))
	}
	if h.reconcilerFn != nil {
		h.reconcilerFn()
	}
	return connect.NewResponse(&orkestraV1.Empty{}), nil
}

// RollbackStack reassigns to an older version.
func (h *StackServiceHandler) RollbackStack(ctx context.Context, req *connect.Request[orkestraV1.RollbackStackRequest]) (*connect.Response[orkestraV1.Assignment], error) {
	return h.AssignStack(ctx, connect.NewRequest(&orkestraV1.AssignStackRequest{
		ServerId:       req.Msg.ServerId,
		StackId:        req.Msg.StackId,
		StackVersionId: req.Msg.StackVersionId,
		DesiredStatus:  "running",
	}))
}

// helpers

func stackFromRow(row store.Stack, version int32) *orkestraV1.Stack {
	var desc string
	if row.Description != nil {
		desc = *row.Description
	}
	return &orkestraV1.Stack{
		Id:          row.ID,
		Name:        row.Name,
		Description: desc,
		Version:     version,
		CreatedAt:   row.CreatedAt,
	}
}

func envVarsToLabels(m map[string]string) map[string]string { return m }

func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
