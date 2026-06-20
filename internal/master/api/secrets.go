package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	masterauth "github.com/heckertobias/orkestra/internal/master/auth"
	mastersecrets "github.com/heckertobias/orkestra/internal/master/secrets"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// SecretServiceHandler implements the UI-facing SecretService RPCs.
type SecretServiceHandler struct {
	db  *pgxpool.Pool
	q   *store.Queries
	kek []byte
}

// NewSecretServiceHandler constructs a SecretServiceHandler.
func NewSecretServiceHandler(db *pgxpool.Pool, kek []byte) *SecretServiceHandler {
	return &SecretServiceHandler{db: db, q: store.New(db), kek: kek}
}

func (h *SecretServiceHandler) ListSecrets(ctx context.Context, _ *connect.Request[orkestraV1.ListSecretsRequest]) (*connect.Response[orkestraV1.ListSecretsResponse], error) {
	rows, err := h.q.ListSecrets(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list secrets: %w", err))
	}
	metas := make([]*orkestraV1.SecretMeta, 0, len(rows))
	for _, row := range rows {
		bindings, _ := h.q.CountSecretBindings(ctx, row.ID)
		metas = append(metas, secretMetaFromRow(row.ID, row.Name, derefStr(row.Description), row.Provider, row.Version, row.BaoMount, row.BaoPath, row.BaoKey, row.CreatedAt, row.UpdatedAt, int32(bindings)))
	}
	return connect.NewResponse(&orkestraV1.ListSecretsResponse{Secrets: metas}), nil
}

func (h *SecretServiceHandler) GetSecret(ctx context.Context, req *connect.Request[orkestraV1.GetSecretRequest]) (*connect.Response[orkestraV1.SecretMeta], error) {
	row, err := h.q.GetSecret(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("secret not found"))
	}
	bindings, _ := h.q.CountSecretBindings(ctx, row.ID)
	return connect.NewResponse(secretMetaFromSecret(row, int32(bindings))), nil
}

func (h *SecretServiceHandler) CreateSecret(ctx context.Context, req *connect.Request[orkestraV1.CreateSecretRequest]) (*connect.Response[orkestraV1.SecretMeta], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanManageSecrets(u) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("requires secrets-manager or admin role"))
	}
	r := req.Msg
	if r.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}
	if r.Provider != "builtin" && r.Provider != "openbao" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("provider must be 'builtin' or 'openbao'"))
	}

	var ciphertext []byte
	if r.Provider == "builtin" {
		if len(r.ValueBytes) == 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("value_bytes required for builtin provider"))
		}
		var err error
		ciphertext, err = mastersecrets.Seal(h.kek, r.ValueBytes)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encrypt secret: %w", err))
		}
	}

	now := time.Now().UnixMilli()
	actor := masterauth.UserFromContext(ctx)
	row, err := h.q.InsertSecret(ctx, store.InsertSecretParams{
		ID:          uuid.NewString(),
		Name:        r.Name,
		Description: ptrString(r.Description),
		Provider:    r.Provider,
		Ciphertext:  ciphertext,
		Version:     1,
		BaoMount:    ptrString(r.BaoMount),
		BaoPath:     ptrString(r.BaoPath),
		BaoKey:      ptrString(r.BaoKey),
		CreatedBy:   actorID(actor),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create secret: %w", err))
	}

	h.audit(ctx, actor, "secret.create", "secret", row.ID, nil)
	return connect.NewResponse(secretMetaFromSecret(row, 0)), nil
}

func (h *SecretServiceHandler) UpdateSecret(ctx context.Context, req *connect.Request[orkestraV1.UpdateSecretRequest]) (*connect.Response[orkestraV1.SecretMeta], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanManageSecrets(u) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("requires secrets-manager or admin role"))
	}
	r := req.Msg
	existing, err := h.q.GetSecret(ctx, r.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("secret not found"))
	}

	ciphertext := existing.Ciphertext
	if existing.Provider == "builtin" && len(r.ValueBytes) > 0 {
		ciphertext, err = mastersecrets.Seal(h.kek, r.ValueBytes)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encrypt secret: %w", err))
		}
	}

	actor := masterauth.UserFromContext(ctx)
	desc := ptrString(r.Description)
	if desc == nil {
		desc = existing.Description
	}

	row, err := h.q.UpdateSecret(ctx, store.UpdateSecretParams{
		ID:          r.Id,
		Description: desc,
		Ciphertext:  ciphertext,
		BaoMount:    ptrString(r.BaoMount),
		BaoPath:     ptrString(r.BaoPath),
		BaoKey:      ptrString(r.BaoKey),
		UpdatedAt:   time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update secret: %w", err))
	}

	bindings, _ := h.q.CountSecretBindings(ctx, row.ID)
	h.audit(ctx, actor, "secret.update", "secret", row.ID, nil)
	return connect.NewResponse(secretMetaFromSecret(row, int32(bindings))), nil
}

func (h *SecretServiceHandler) DeleteSecret(ctx context.Context, req *connect.Request[orkestraV1.DeleteSecretRequest]) (*connect.Response[orkestraV1.SecretEmpty], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanManageSecrets(u) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("requires secrets-manager or admin role"))
	}
	bindings, err := h.q.CountSecretBindings(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("check bindings: %w", err))
	}
	if bindings > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("secret has %d active binding(s); remove them first", bindings))
	}
	if err := h.q.DeleteSecret(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete secret: %w", err))
	}
	actor := masterauth.UserFromContext(ctx)
	h.audit(ctx, actor, "secret.delete", "secret", req.Msg.Id, nil)
	return connect.NewResponse(&orkestraV1.SecretEmpty{}), nil
}

func (h *SecretServiceHandler) RevealSecret(ctx context.Context, req *connect.Request[orkestraV1.RevealSecretRequest]) (*connect.Response[orkestraV1.RevealSecretResponse], error) {
	actor := masterauth.UserFromContext(ctx)
	if !masterauth.CanManageSecrets(actor) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("requires secrets-manager or admin role"))
	}

	// Re-authenticate: verify the provided password against the user's stored hash.
	if req.Msg.ReauthPassword == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("re-authentication password required"))
	}
	dbUser, err := h.q.GetUser(ctx, actor.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load user: %w", err))
	}
	if dbUser.PasswordHash == nil || !masterauth.VerifyPassword(*dbUser.PasswordHash, req.Msg.ReauthPassword) {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid password"))
	}

	row, err := h.q.GetSecret(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("secret not found"))
	}
	if row.Provider != "builtin" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("reveal is only available for builtin secrets"))
	}
	plaintext, err := mastersecrets.Open(h.kek, row.Ciphertext)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("decrypt secret: %w", err))
	}
	h.audit(ctx, actor, "secret.reveal", "secret", row.ID, nil)
	return connect.NewResponse(&orkestraV1.RevealSecretResponse{
		ValueBytes: plaintext,
		Version:    int32(row.Version),
	}), nil
}

func (h *SecretServiceHandler) MigrateProvider(ctx context.Context, req *connect.Request[orkestraV1.MigrateProviderRequest]) (*connect.Response[orkestraV1.SecretMeta], error) {
	u := masterauth.UserFromContext(ctx)
	if !masterauth.CanManageSecrets(u) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("requires secrets-manager or admin role"))
	}
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("provider migration available in a future release"))
}

// audit writes an audit log entry, logging on error.
func (h *SecretServiceHandler) audit(ctx context.Context, actor *masterauth.UserCtx, action, targetType, targetID string, errMsg *string) {
	p := store.InsertAuditLogParams{
		Ts:         time.Now().UnixMilli(),
		Action:     action,
		TargetType: targetType,
		TargetID:   ptrString(targetID),
		Error:      errMsg,
	}
	if actor != nil {
		p.ActorID = ptrString(actor.ID)
		p.ActorName = ptrString(actor.Username)
	}
	if err := h.q.InsertAuditLog(ctx, p); err != nil {
		slog.Warn("audit log insert failed", "action", action, "err", err)
	}
}

// helpers

func secretMetaFromSecret(row store.Secret, bindingCount int32) *orkestraV1.SecretMeta {
	return secretMetaFromRow(row.ID, row.Name, derefStr(row.Description), row.Provider, row.Version, row.BaoMount, row.BaoPath, row.BaoKey, row.CreatedAt, row.UpdatedAt, bindingCount)
}

func secretMetaFromRow(id, name, description, provider string, version int64, baoMount, baoPath, baoKey *string, createdAt, updatedAt int64, bindingCount int32) *orkestraV1.SecretMeta {
	return &orkestraV1.SecretMeta{
		Id:           id,
		Name:         name,
		Description:  description,
		Provider:     provider,
		Version:      int32(version),
		BaoMount:     derefStr(baoMount),
		BaoPath:      derefStr(baoPath),
		BaoKey:       derefStr(baoKey),
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		BindingCount: bindingCount,
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func actorID(u *masterauth.UserCtx) *string {
	if u == nil {
		return nil
	}
	return &u.ID
}
