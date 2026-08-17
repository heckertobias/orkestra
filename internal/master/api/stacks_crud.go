package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	masterauth "github.com/heckertobias/orkestra/internal/master/auth"
	"github.com/heckertobias/orkestra/internal/master/store"
	sharedcompose "github.com/heckertobias/orkestra/internal/shared/compose"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// validateComposeOrError returns an InvalidArgument error listing every error-severity diagnostic
// from ValidateCompose, or nil when the compose has no blocking problems (warnings do not block).
// This is what makes the editor's diagnostics actually enforced: an unsupported construct such as a
// named volume (#70) is refused at create/update instead of being stored and silently dropped by the
// agent. An empty compose is treated as valid — a stack may be created without an initial version.
func validateComposeOrError(composeYAML string) error {
	if composeYAML == "" {
		return nil
	}
	var msgs []string
	for _, d := range sharedcompose.ValidateCompose(composeYAML) {
		if d.Severity != sharedcompose.SeverityError {
			continue
		}
		if d.Line > 0 {
			msgs = append(msgs, fmt.Sprintf("line %d: %s", d.Line, d.Message))
		} else {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("compose validation failed: %s", strings.Join(msgs, "; ")))
}

// validateEnvValuesOrError refuses an assignment whose compose YAML references variables that would
// interpolate to an empty string (#81). Since #76 the assignment's env values are the *complete*
// input to interpolation, so the Master can decide this before anything reaches an agent — and it
// is the only place that can, because the same stack version may be complete for one server and
// incomplete for another. Without the check `image: ${REGISTRY}/app:${TAG}` silently becomes
// `/app:` and the first symptom is a Docker error naming neither variable.
func validateEnvValuesOrError(composeYAML string, values map[string]string) error {
	missing := sharedcompose.MissingVars(composeYAML, values)
	if len(missing) == 0 {
		return nil
	}
	msgs := make([]string, len(missing))
	for i, ref := range missing {
		msgs[i] = fmt.Sprintf("%s (line %d)", ref.Name, ref.Line)
	}
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(
		"missing values for compose variables: %s — supply a value, or give the reference a default such as ${VAR:-fallback}",
		strings.Join(msgs, ", ")))
}

// CreateStack creates a new stack with an initial version.
// Any operator (any scope) may create a stack definition.
func (h *StackServiceHandler) CreateStack(ctx context.Context, req *connect.Request[orkestraV1.CreateStackRequest]) (*connect.Response[orkestraV1.Stack], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.HasAnyOperator(u) {
		return nil, errPermission("operator role required to create stacks")
	}
	r := req.Msg
	if r.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if err := validateComposeOrError(r.ComposeYaml); err != nil {
		return nil, err
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
		_, err = h.q.InsertStackVersion(ctx, store.InsertStackVersionParams{
			ID:          uuid.NewString(),
			StackID:     stackID,
			Version:     1,
			ComposeYaml: r.ComposeYaml,
			EnvVarNames: marshalEnvVarNames(r.EnvVarNames),
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
// Requires operator access on at least one server the stack is assigned to (or any operator for unassigned stacks).
func (h *StackServiceHandler) UpdateStack(ctx context.Context, req *connect.Request[orkestraV1.UpdateStackRequest]) (*connect.Response[orkestraV1.StackVersion], error) {
	u := masterauth.UserFromContext(ctx)
	serverIDs := h.assignedServerIDs(ctx, req.Msg.Id)
	if !masterauth.CanEditStack(u, req.Msg.Id, serverIDs) {
		return nil, errPermission("operator access required on an assigned server to update this stack")
	}
	r := req.Msg
	if err := validateComposeOrError(r.ComposeYaml); err != nil {
		return nil, err
	}
	nextVer, err := h.q.GetNextVersionNumber(ctx, r.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("stack not found"))
	}
	versionID := uuid.NewString()
	now := time.Now().UnixMilli()

	_, err = h.q.InsertStackVersion(ctx, store.InsertStackVersionParams{
		ID:          versionID,
		StackID:     r.Id,
		Version:     int64(nextVer),
		ComposeYaml: r.ComposeYaml,
		EnvVarNames: marshalEnvVarNames(r.EnvVarNames),
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
		EnvVarNames: r.EnvVarNames,
		CreatedAt:   now,
	}), nil
}

// GetStack returns a stack by ID (viewer+ access required on any assigned server).
func (h *StackServiceHandler) GetStack(ctx context.Context, req *connect.Request[orkestraV1.GetStackRequest]) (*connect.Response[orkestraV1.Stack], error) {
	u := masterauth.UserFromContext(ctx)
	serverIDs := h.assignedServerIDs(ctx, req.Msg.Id)
	if !masterauth.CanViewStack(u, req.Msg.Id, serverIDs) {
		return nil, errPermission("no access to this stack")
	}
	row, err := h.q.GetStack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("stack not found"))
	}
	latest, _ := h.q.GetLatestStackVersion(ctx, row.ID)
	return connect.NewResponse(stackFromRow(row, int32(latest.Version))), nil
}

// ListStacks returns all non-deleted stacks the caller may view.
func (h *StackServiceHandler) ListStacks(ctx context.Context, _ *connect.Request[orkestraV1.ListStacksRequest]) (*connect.Response[orkestraV1.ListStacksResponse], error) {
	u := masterauth.UserFromContext(ctx)
	rows, err := h.q.ListStacks(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list stacks: %w", err))
	}
	stacks := make([]*orkestraV1.Stack, 0, len(rows))
	for _, row := range rows {
		serverIDs := h.assignedServerIDs(ctx, row.ID)
		if !masterauth.CanViewStack(u, row.ID, serverIDs) {
			continue
		}
		latest, _ := h.q.GetLatestStackVersion(ctx, row.ID)
		stacks = append(stacks, stackFromRow(row, int32(latest.Version)))
	}
	return connect.NewResponse(&orkestraV1.ListStacksResponse{Stacks: stacks}), nil
}

// DeleteStack soft-deletes a stack. Requires operator access on ALL assigned servers.
func (h *StackServiceHandler) DeleteStack(ctx context.Context, req *connect.Request[orkestraV1.DeleteStackRequest]) (*connect.Response[orkestraV1.Empty], error) {
	u := masterauth.UserFromContext(ctx)
	serverIDs := h.assignedServerIDs(ctx, req.Msg.Id)
	if !masterauth.CanDeleteStack(u, req.Msg.Id, serverIDs) {
		return nil, errPermission("operator access required on all assigned servers to delete this stack")
	}
	if err := h.q.SoftDeleteStack(ctx, store.SoftDeleteStackParams{
		DeletedAt: ptrInt64(time.Now().UnixMilli()),
		ID:        req.Msg.Id,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete stack: %w", err))
	}
	// Push so agents drop the now soft-deleted stack from their desired state and remove its
	// containers (ListAssignmentsForServer filters deleted_at).
	if h.reconcilerFn != nil {
		h.reconcilerFn()
	}
	return connect.NewResponse(&orkestraV1.Empty{}), nil
}

// ListStackVersions returns all versions for a stack (viewer+ access required).
func (h *StackServiceHandler) ListStackVersions(ctx context.Context, req *connect.Request[orkestraV1.ListStackVersionsRequest]) (*connect.Response[orkestraV1.ListStackVersionsResponse], error) {
	u := masterauth.UserFromContext(ctx)
	serverIDs := h.assignedServerIDs(ctx, req.Msg.StackId)
	if !masterauth.CanViewStack(u, req.Msg.StackId, serverIDs) {
		return nil, errPermission("no access to this stack")
	}
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
			EnvVarNames: unmarshalEnvVarNames(row.EnvVarNames),
			CreatedAt:   row.CreatedAt,
		})
	}
	return connect.NewResponse(&orkestraV1.ListStackVersionsResponse{Versions: versions}), nil
}

// AssignStack assigns a stack version to a server and triggers reconciliation.
// Requires operator access on (serverID, stackID).
func (h *StackServiceHandler) AssignStack(ctx context.Context, req *connect.Request[orkestraV1.AssignStackRequest]) (*connect.Response[orkestraV1.Assignment], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanOperateOn(u, req.Msg.ServerId, req.Msg.StackId) {
		return nil, errPermission("operator access required on this server/stack")
	}
	r := req.Msg
	var version store.StackVersion
	if r.StackVersionId == "" {
		latest, err := h.q.GetLatestStackVersion(ctx, r.StackId)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no versions for stack"))
		}
		version = latest
	} else {
		v, err := h.q.GetStackVersion(ctx, r.StackVersionId)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("stack version not found"))
		}
		// The version must belong to the stack being assigned — otherwise the caller's permission
		// check and the env validation below both apply to a different stack's compose.
		if v.StackID != r.StackId {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("stack version belongs to a different stack"))
		}
		version = v
	}
	versionID := version.ID

	// Checked for every desired status, not just "running": the values are being typed now, so the
	// error belongs now rather than whenever the stack is first started.
	if err := validateEnvValuesOrError(version.ComposeYaml, r.EnvValues); err != nil {
		return nil, err
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
		EnvValues:      marshalEnvValues(r.EnvValues),
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
		EnvValues:      unmarshalEnvValues(row.EnvValues),
	}), nil
}

// UnassignStack removes a stack assignment.
// Requires operator access on (serverID, stackID).
func (h *StackServiceHandler) UnassignStack(ctx context.Context, req *connect.Request[orkestraV1.UnassignStackRequest]) (*connect.Response[orkestraV1.Empty], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanOperateOn(u, req.Msg.ServerId, req.Msg.StackId) {
		return nil, errPermission("operator access required on this server/stack")
	}
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

// RollbackStack reassigns to an older version, preserving the server's existing
// per-assignment env values so a rollback doesn't wipe them.
func (h *StackServiceHandler) RollbackStack(ctx context.Context, req *connect.Request[orkestraV1.RollbackStackRequest]) (*connect.Response[orkestraV1.Assignment], error) {
	var envValues map[string]string
	if existing, err := h.q.ListAssignmentsForStack(ctx, req.Msg.StackId); err == nil {
		for _, a := range existing {
			if a.ServerID == req.Msg.ServerId {
				envValues = unmarshalEnvValues(a.EnvValues)
				break
			}
		}
	}
	return h.AssignStack(ctx, connect.NewRequest(&orkestraV1.AssignStackRequest{
		ServerId:       req.Msg.ServerId,
		StackId:        req.Msg.StackId,
		StackVersionId: req.Msg.StackVersionId,
		DesiredStatus:  "running",
		EnvValues:      envValues,
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

// marshalEnvVarNames encodes a list of required env-var names as a JSONB array.
func marshalEnvVarNames(names []string) []byte {
	if len(names) == 0 {
		return []byte("[]")
	}
	b, err := json.Marshal(names)
	if err != nil {
		return []byte("[]")
	}
	return b
}

// unmarshalEnvVarNames parses a JSONB array of env-var names from the DB.
func unmarshalEnvVarNames(raw []byte) []string {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil
	}
	return names
}

// marshalEnvValues encodes per-assignment env values as a JSONB object.
func marshalEnvValues(m map[string]string) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// unmarshalEnvValues parses a JSONB object of per-assignment env values.
// Returns nil (empty map in proto) on empty/null input.
func unmarshalEnvValues(raw []byte) map[string]string {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
