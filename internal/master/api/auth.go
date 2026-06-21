package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	masterauth "github.com/heckertobias/orkestra/internal/master/auth"
	"github.com/heckertobias/orkestra/internal/master/email"
	"github.com/heckertobias/orkestra/internal/master/pki"
	"github.com/heckertobias/orkestra/internal/master/store"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

const sessionTTL = 24 * time.Hour

// AuthServiceHandler implements the UI-facing AuthService RPCs.
type AuthServiceHandler struct {
	db         *pgxpool.Pool
	q          *store.Queries
	kek        []byte
	setupToken *string // non-nil when first-run setup is pending
	mailer     *email.Mailer
}

// NewAuthServiceHandler constructs an AuthServiceHandler.
func NewAuthServiceHandler(db *pgxpool.Pool, kek []byte, setupToken *string, mailer *email.Mailer) *AuthServiceHandler {
	return &AuthServiceHandler{db: db, q: store.New(db), kek: kek, setupToken: setupToken, mailer: mailer}
}

// AuditLogHTTPHandler returns audit log entries as JSON (GET /api/audit).
func (h *AuthServiceHandler) AuditLogHTTPHandler(w http.ResponseWriter, r *http.Request) {
	// SessionMiddleware runs before this handler and injects the user into context.
	u := masterauth.UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	if !masterauth.IsAdmin(u) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	entries, err := h.q.ListAuditLog(r.Context(), 200)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	type entry struct {
		ID         int64   `json:"id"`
		Ts         int64   `json:"ts"`
		ActorID    *string `json:"actorId"`
		ActorName  *string `json:"actorName"`
		Action     string  `json:"action"`
		TargetType string  `json:"targetType"`
		TargetID   *string `json:"targetId"`
		IpAddress  *string `json:"ipAddress"`
		Error      *string `json:"error"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, entry{
			ID:         e.ID,
			Ts:         e.Ts,
			ActorID:    e.ActorID,
			ActorName:  e.ActorName,
			Action:     e.Action,
			TargetType: e.TargetType,
			TargetID:   e.TargetID,
			IpAddress:  e.IpAddress,
			Error:      e.Error,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"entries": out})
}

// SetupHTTPHandler handles the /api/setup endpoint for first-run user creation.
// This is a plain HTTP endpoint, not a Connect RPC, so it bypasses auth enforcement.
func (h *AuthServiceHandler) SetupHTTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.setupToken == nil || *h.setupToken == "" {
		http.Error(w, "setup already complete", http.StatusGone)
		return
	}

	var body struct {
		Token       string `json:"token"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if body.Token != *h.setupToken {
		http.Error(w, "invalid setup token", http.StatusForbidden)
		return
	}

	count, err := h.q.CountUsers(r.Context())
	if err != nil || count > 0 {
		http.Error(w, "setup already complete", http.StatusGone)
		return
	}

	if body.Username == "" || body.Password == "" {
		http.Error(w, "username and password required", http.StatusBadRequest)
		return
	}

	hash, err := masterauth.HashPassword(body.Password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UnixMilli()
	user, err := h.q.InsertUser(r.Context(), store.InsertUserParams{
		ID:           uuid.NewString(),
		Username:     body.Username,
		DisplayName:  ptrString(body.DisplayName),
		PasswordHash: &hash,
		Disabled:     false,
		CreatedAt:    now,
	})
	if err != nil {
		http.Error(w, "create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Assign admin role.
	_, err = h.q.InsertRoleBinding(r.Context(), store.InsertRoleBindingParams{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		RoleID:    "role-admin",
		CreatedAt: now,
	})
	if err != nil {
		slog.Error("assign admin role during setup", "err", err)
	}

	*h.setupToken = "" // invalidate token
	slog.Info("first-run setup complete", "username", user.Username)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "username": user.Username})
}

// Login validates credentials and sets the session cookie.
func (h *AuthServiceHandler) Login(ctx context.Context, req *connect.Request[orkestraV1.LoginRequest]) (*connect.Response[orkestraV1.LoginResponse], error) {
	r := req.Msg
	if r.Username == "" || r.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("username and password required"))
	}

	user, err := h.q.GetUserByUsername(ctx, r.Username)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid credentials"))
	}
	if user.Disabled {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("account disabled"))
	}
	if user.PasswordHash == nil || !masterauth.VerifyPassword(*user.PasswordHash, r.Password) {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid credentials"))
	}

	rawToken, sessionID, err := masterauth.GenerateSessionToken()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generate session: %w", err))
	}

	now := time.Now()
	expires := now.Add(sessionTTL)
	ipAddr := ipFromRequest(req.Header())
	ua := req.Header().Get("User-Agent")

	if err := h.q.InsertSession(ctx, store.InsertSessionParams{
		ID:        sessionID,
		UserID:    user.ID,
		CreatedAt: now.UnixMilli(),
		ExpiresAt: expires.UnixMilli(),
		LastSeen:  now.UnixMilli(),
		IpAddress: ptrString(ipAddr),
		UserAgent: ptrString(ua),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create session: %w", err))
	}

	nowMs := now.UnixMilli()
	_ = h.q.SetLastLogin(ctx, store.SetLastLoginParams{ID: user.ID, LastLoginAt: &nowMs})

	roles, _ := h.q.GetUserRoles(ctx, user.ID)
	bindings := h.loadUserBindings(ctx, user.ID)

	resp := connect.NewResponse(&orkestraV1.LoginResponse{
		User:      userToProto(user, roles, bindings),
		SessionId: sessionID,
	})
	masterauth.SetSessionCookie(resp.Header(), rawToken, expires)

	h.auditAuth(ctx, &user, "auth.login", nil)
	return resp, nil
}

// Logout revokes the session cookie.
func (h *AuthServiceHandler) Logout(ctx context.Context, _ *connect.Request[orkestraV1.AuthEmpty]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	sessionID := masterauth.SessionIDFromContext(ctx)
	if sessionID != "" {
		_ = h.q.RevokeSession(ctx, sessionID)
	}
	u := masterauth.UserFromContext(ctx)
	if u != nil {
		user, _ := h.q.GetUser(ctx, u.ID)
		h.auditAuth(ctx, &user, "auth.logout", nil)
	}
	resp := connect.NewResponse(&orkestraV1.AuthEmpty{})
	masterauth.ClearSessionCookie(resp.Header())
	return resp, nil
}

// GetCurrentUser returns the authenticated user.
func (h *AuthServiceHandler) GetCurrentUser(ctx context.Context, _ *connect.Request[orkestraV1.AuthEmpty]) (*connect.Response[orkestraV1.User], error) {
	u := masterauth.UserFromContext(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("not authenticated"))
	}
	user, err := h.q.GetUser(ctx, u.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("user not found"))
	}
	roles, _ := h.q.GetUserRoles(ctx, user.ID)
	bindings := h.loadUserBindings(ctx, user.ID)
	return connect.NewResponse(userToProto(user, roles, bindings)), nil
}

// ListUsers returns all users (admin only).
func (h *AuthServiceHandler) ListUsers(ctx context.Context, _ *connect.Request[orkestraV1.ListUsersRequest]) (*connect.Response[orkestraV1.ListUsersResponse], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	rows, err := h.q.ListUsers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list users: %w", err))
	}
	users := make([]*orkestraV1.User, 0, len(rows))
	for _, row := range rows {
		roles, _ := h.q.GetUserRoles(ctx, row.ID)
		bindings := h.loadUserBindings(ctx, row.ID)
		users = append(users, userToProto(row, roles, bindings))
	}
	return connect.NewResponse(&orkestraV1.ListUsersResponse{Users: users}), nil
}

// CreateUser creates a new user (admin only).
// The username must be a valid email address. No password is set; the user
// receives an invite email with a link to set their own password.
func (h *AuthServiceHandler) CreateUser(ctx context.Context, req *connect.Request[orkestraV1.CreateUserRequest]) (*connect.Response[orkestraV1.User], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	r := req.Msg
	if r.Username == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("email (username) required"))
	}
	if _, err := mail.ParseAddress(r.Username); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("username must be a valid email address"))
	}
	row, err := h.q.InsertUser(ctx, store.InsertUserParams{
		ID:          uuid.NewString(),
		Username:    r.Username,
		DisplayName: ptrString(r.DisplayName),
		// PasswordHash is nil — user must set password via invite link
		Disabled:  false,
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create user: %w", err))
	}
	actor := masterauth.UserFromContext(ctx)
	h.auditAuth(ctx, nil, "user.create", ptrString(fmt.Sprintf("actor=%s target=%s", actor.Username, row.Username)))

	// Generate and email an invite token.
	rawToken, tokenHash := generateResetToken()
	now := time.Now()
	_ = h.q.InsertPasswordResetToken(ctx, store.InsertPasswordResetTokenParams{
		ID:        uuid.NewString(),
		UserID:    row.ID,
		TokenHash: tokenHash,
		Purpose:   "invite",
		ExpiresAt: now.Add(72 * time.Hour).UnixMilli(),
		CreatedAt: now.UnixMilli(),
	})
	link := h.publicURL(req.Header()) + "/set-password?token=" + rawToken
	h.mailer.SendInvite(ctx, r.Username, link)

	return connect.NewResponse(userToProto(row, nil, nil)), nil
}

// UpdateUser updates display_name and disabled flag (admin only).
func (h *AuthServiceHandler) UpdateUser(ctx context.Context, req *connect.Request[orkestraV1.UpdateUserRequest]) (*connect.Response[orkestraV1.User], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	// An admin cannot deactivate their own account.
	if req.Msg.Disabled {
		if actor := masterauth.UserFromContext(ctx); actor != nil && req.Msg.Id == actor.ID {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cannot deactivate your own account"))
		}
	}
	row, err := h.q.UpdateUser(ctx, store.UpdateUserParams{
		ID:          req.Msg.Id,
		DisplayName: ptrString(req.Msg.DisplayName),
		Disabled:    req.Msg.Disabled,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update user: %w", err))
	}
	roles, _ := h.q.GetUserRoles(ctx, row.ID)
	bindings := h.loadUserBindings(ctx, row.ID)
	return connect.NewResponse(userToProto(row, roles, bindings)), nil
}

// DeleteUser permanently removes a user from the database (admin only).
// Sessions and role bindings are cascade-deleted; stacks/secrets created by the
// user retain a null owner (see migration 00004_user_ondelete).
func (h *AuthServiceHandler) DeleteUser(ctx context.Context, req *connect.Request[orkestraV1.DeleteUserRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	actor := masterauth.UserFromContext(ctx)
	if actor != nil && req.Msg.Id == actor.ID {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cannot delete your own account"))
	}
	// Load user info before deletion for the audit entry.
	target, _ := h.q.GetUser(ctx, req.Msg.Id)
	if err := h.q.DeleteUserByID(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete user: %w", err))
	}
	if actor != nil {
		detail := fmt.Sprintf("actor=%s target=%s", actor.Username, target.Username)
		h.auditAuth(ctx, nil, "user.delete", ptrString(detail))
	}
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// ResetPassword sets a new password for a user (admin only).
func (h *AuthServiceHandler) ResetPassword(ctx context.Context, req *connect.Request[orkestraV1.ResetPasswordRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	if req.Msg.NewPassword == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("new_password required"))
	}
	if err := h.validatePassword(ctx, req.Msg.NewPassword); err != nil {
		return nil, err
	}
	hash, err := masterauth.HashPassword(req.Msg.NewPassword)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("hash password: %w", err))
	}
	if err := h.q.SetPasswordHash(ctx, store.SetPasswordHashParams{
		ID:           req.Msg.UserId,
		PasswordHash: &hash,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reset password: %w", err))
	}
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// ListRoleBindings returns role bindings, filtered by user_id if provided (admin only).
func (h *AuthServiceHandler) ListRoleBindings(ctx context.Context, req *connect.Request[orkestraV1.ListRoleBindingsRequest]) (*connect.Response[orkestraV1.ListRoleBindingsResponse], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	var rows []store.RoleBinding
	var err error
	if req.Msg.UserId != "" {
		rows, err = h.q.ListRoleBindingsByUser(ctx, req.Msg.UserId)
	} else {
		rows, err = h.q.ListAllRoleBindings(ctx)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list role bindings: %w", err))
	}
	bindings := make([]*orkestraV1.RoleBinding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, roleBindingToProto(row))
	}
	return connect.NewResponse(&orkestraV1.ListRoleBindingsResponse{Bindings: bindings}), nil
}

// AssignRole creates a new role binding (admin only).
func (h *AuthServiceHandler) AssignRole(ctx context.Context, req *connect.Request[orkestraV1.AssignRoleRequest]) (*connect.Response[orkestraV1.RoleBinding], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	r := req.Msg
	roleID := "role-" + r.Role
	row, err := h.q.InsertRoleBinding(ctx, store.InsertRoleBindingParams{
		ID:        uuid.NewString(),
		UserID:    r.UserId,
		RoleID:    roleID,
		ServerID:  ptrString(r.ServerId),
		StackID:   ptrString(r.StackId),
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("assign role: %w", err))
	}
	return connect.NewResponse(roleBindingToProto(row)), nil
}

// RevokeRole removes a role binding (admin only).
func (h *AuthServiceHandler) RevokeRole(ctx context.Context, req *connect.Request[orkestraV1.RevokeRoleRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	// An admin may not revoke their OWN global admin role — analogous to the
	// self-deletion guard in DeleteUser — ensuring at least one admin always remains.
	if actor := masterauth.UserFromContext(ctx); actor != nil {
		mine, err := h.q.ListRoleBindingsByUser(ctx, actor.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke role: %w", err))
		}
		for _, b := range mine {
			if b.ID == req.Msg.BindingId && b.RoleID == "role-admin" && b.ServerID == nil && b.StackID == nil {
				return nil, connect.NewError(connect.CodeFailedPrecondition,
					fmt.Errorf("cannot revoke your own admin role"))
			}
		}
	}
	if err := h.q.DeleteRoleBinding(ctx, req.Msg.BindingId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke role: %w", err))
	}
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// ChangePassword lets an authenticated user change their own password.
func (h *AuthServiceHandler) ChangePassword(ctx context.Context, req *connect.Request[orkestraV1.ChangePasswordRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	u := masterauth.UserFromContext(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("not authenticated"))
	}
	r := req.Msg
	if r.CurrentPassword == "" || r.NewPassword == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("current_password and new_password required"))
	}
	user, err := h.q.GetUser(ctx, u.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("user not found"))
	}
	if user.PasswordHash == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("account uses SSO — no local password to change"))
	}
	if !masterauth.VerifyPassword(*user.PasswordHash, r.CurrentPassword) {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("current password is incorrect"))
	}
	if err := h.validatePassword(ctx, r.NewPassword); err != nil {
		return nil, err
	}
	hash, err := masterauth.HashPassword(r.NewPassword)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("hash password: %w", err))
	}
	if err := h.q.SetPasswordHash(ctx, store.SetPasswordHashParams{ID: u.ID, PasswordHash: &hash}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("set password: %w", err))
	}
	h.auditAuth(ctx, &user, "auth.change_password", nil)
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// RequestPasswordReset sends a password-reset email.
// Always returns OK (never reveals whether the email exists).
func (h *AuthServiceHandler) RequestPasswordReset(ctx context.Context, req *connect.Request[orkestraV1.RequestPasswordResetRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	em := req.Msg.Email
	if em == "" {
		return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
	}
	go func() {
		user, err := h.q.GetUserByUsername(context.Background(), em)
		if err != nil || user.Disabled || user.PasswordHash == nil && user.OidcSubject != nil {
			return // silently drop: unknown, disabled, or OIDC-only account
		}
		rawToken, tokenHash := generateResetToken()
		now := time.Now()
		_ = h.q.InsertPasswordResetToken(context.Background(), store.InsertPasswordResetTokenParams{
			ID:        uuid.NewString(),
			UserID:    user.ID,
			TokenHash: tokenHash,
			Purpose:   "reset",
			ExpiresAt: now.Add(time.Hour).UnixMilli(),
			CreatedAt: now.UnixMilli(),
		})
		link := h.publicURL(req.Header()) + "/set-password?token=" + rawToken
		h.mailer.SendPasswordReset(context.Background(), em, link)
	}()
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// ResetPasswordWithToken validates a password-reset or invite token and sets the new password.
func (h *AuthServiceHandler) ResetPasswordWithToken(ctx context.Context, req *connect.Request[orkestraV1.ResetPasswordWithTokenRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	r := req.Msg
	if r.Token == "" || r.NewPassword == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("token and new_password required"))
	}
	tokenHash := hashResetToken(r.Token)
	record, err := h.q.GetPasswordResetTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("invalid or expired token"))
	}
	if record.UsedAt != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("token already used"))
	}
	if time.Now().UnixMilli() > record.ExpiresAt {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("token expired"))
	}
	if err := h.validatePassword(ctx, r.NewPassword); err != nil {
		return nil, err
	}
	hash, err := masterauth.HashPassword(r.NewPassword)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("hash password: %w", err))
	}
	if err := h.q.SetPasswordHash(ctx, store.SetPasswordHashParams{
		ID:           record.UserID,
		PasswordHash: &hash,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("set password: %w", err))
	}
	now := time.Now().UnixMilli()
	_ = h.q.MarkPasswordResetTokenUsed(ctx, store.MarkPasswordResetTokenUsedParams{
		ID:     record.ID,
		UsedAt: &now,
	})
	// Invalidate all existing sessions after a password reset.
	_ = h.q.RevokeAllSessionsForUser(ctx, record.UserID)
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// GetPasswordPolicy returns the current password policy (admin only).
func (h *AuthServiceHandler) GetPasswordPolicy(ctx context.Context, _ *connect.Request[orkestraV1.AuthEmpty]) (*connect.Response[orkestraV1.PasswordPolicy], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	p, err := h.q.GetPasswordPolicy(ctx)
	if err != nil {
		return connect.NewResponse(&orkestraV1.PasswordPolicy{}), nil // no policy yet
	}
	return connect.NewResponse(policyToProto(p)), nil
}

// UpdatePasswordPolicy saves the password policy (admin only).
func (h *AuthServiceHandler) UpdatePasswordPolicy(ctx context.Context, req *connect.Request[orkestraV1.PasswordPolicy]) (*connect.Response[orkestraV1.PasswordPolicy], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	r := req.Msg
	p, err := h.q.UpsertPasswordPolicy(ctx, store.UpsertPasswordPolicyParams{
		MinLength:  int32(r.MinLength),
		SpecialMin: int32(r.SpecialMin),
		SpecialMax: int32(r.SpecialMax),
		DigitMin:   int32(r.DigitMin),
		DigitMax:   int32(r.DigitMax),
		UpperMin:   int32(r.UpperMin),
		UpperMax:   int32(r.UpperMax),
		LowerMin:   int32(r.LowerMin),
		LowerMax:   int32(r.LowerMax),
		UpdatedAt:  time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("save policy: %w", err))
	}
	return connect.NewResponse(policyToProto(p)), nil
}

// GetSMTPConfig returns the current SMTP configuration (admin only).
func (h *AuthServiceHandler) GetSMTPConfig(ctx context.Context, _ *connect.Request[orkestraV1.AuthEmpty]) (*connect.Response[orkestraV1.SMTPConfig], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	cfg, err := h.q.GetSMTPConfig(ctx)
	if err != nil {
		return connect.NewResponse(&orkestraV1.SMTPConfig{Port: 587, Starttls: true}), nil
	}
	return connect.NewResponse(&orkestraV1.SMTPConfig{
		Enabled:     cfg.Enabled,
		Host:        cfg.Host,
		Port:        int32(cfg.Port),
		Username:    cfg.Username,
		FromAddress: cfg.FromAddress,
		PublicUrl:   cfg.PublicUrl,
		Starttls:    cfg.Starttls,
		// password not returned (write-only)
	}), nil
}

// UpdateSMTPConfig saves the SMTP configuration (admin only).
func (h *AuthServiceHandler) UpdateSMTPConfig(ctx context.Context, req *connect.Request[orkestraV1.UpdateSMTPConfigRequest]) (*connect.Response[orkestraV1.SMTPConfig], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	r := req.Msg

	// Encrypt the password with KEK (keep existing if blank).
	passwordEnc := ""
	if r.Password != "" {
		enc, err := pki.Encrypt(h.kek, []byte(r.Password))
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encrypt password: %w", err))
		}
		passwordEnc = base64.StdEncoding.EncodeToString(enc)
	} else {
		existing, err := h.q.GetSMTPConfig(ctx)
		if err == nil {
			passwordEnc = existing.PasswordEnc
		}
	}

	port := int32(r.Port)
	if port == 0 {
		port = 587
	}

	cfg, err := h.q.UpsertSMTPConfig(ctx, store.UpsertSMTPConfigParams{
		Enabled:     r.Enabled,
		Host:        r.Host,
		Port:        port,
		Username:    r.Username,
		PasswordEnc: passwordEnc,
		FromAddress: r.FromAddress,
		PublicUrl:   r.PublicUrl,
		Starttls:    r.Starttls,
		UpdatedAt:   time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("save smtp config: %w", err))
	}
	return connect.NewResponse(&orkestraV1.SMTPConfig{
		Enabled:     cfg.Enabled,
		Host:        cfg.Host,
		Port:        int32(cfg.Port),
		Username:    cfg.Username,
		FromAddress: cfg.FromAddress,
		PublicUrl:   cfg.PublicUrl,
		Starttls:    cfg.Starttls,
	}), nil
}

// GetOIDCConfig returns the current OIDC configuration.
func (h *AuthServiceHandler) GetOIDCConfig(ctx context.Context, _ *connect.Request[orkestraV1.AuthEmpty]) (*connect.Response[orkestraV1.OIDCConfig], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	cfg, err := h.q.GetOIDCConfig(ctx)
	if err != nil {
		// No config yet — return disabled empty config.
		return connect.NewResponse(&orkestraV1.OIDCConfig{Enabled: false}), nil
	}
	var scopes []string
	_ = json.Unmarshal(cfg.Scopes, &scopes)
	var claimMapping map[string]string
	_ = json.Unmarshal(cfg.ClaimMapping, &claimMapping)
	return connect.NewResponse(&orkestraV1.OIDCConfig{
		Enabled:      cfg.Enabled,
		IssuerUrl:    cfg.IssuerUrl,
		ClientId:     cfg.ClientID,
		Scopes:       scopes,
		ClaimMapping: claimMapping,
		GroupsClaim:  cfg.GroupsClaim,
	}), nil
}

// UpdateOIDCConfig stores an encrypted OIDC configuration.
func (h *AuthServiceHandler) UpdateOIDCConfig(ctx context.Context, req *connect.Request[orkestraV1.UpdateOIDCConfigRequest]) (*connect.Response[orkestraV1.OIDCConfig], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	r := req.Msg

	// Encrypt the client secret with KEK.
	secretEnc := ""
	if r.ClientSecret != "" {
		enc, err := pki.Encrypt(h.kek, []byte(r.ClientSecret))
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encrypt secret: %w", err))
		}
		secretEnc = base64.StdEncoding.EncodeToString(enc)
	} else {
		// Keep the existing encrypted secret.
		existing, err := h.q.GetOIDCConfig(ctx)
		if err == nil {
			secretEnc = existing.ClientSecretEnc
		}
	}

	scopes := r.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	scopesJSON, _ := json.Marshal(scopes)
	claimJSON, _ := json.Marshal(r.ClaimMapping)

	groupsClaim := r.GroupsClaim
	if groupsClaim == "" {
		groupsClaim = "groups"
	}

	cfg, err := h.q.UpsertOIDCConfig(ctx, store.UpsertOIDCConfigParams{
		IssuerUrl:       r.IssuerUrl,
		ClientID:        r.ClientId,
		ClientSecretEnc: secretEnc,
		Scopes:          scopesJSON,
		ClaimMapping:    claimJSON,
		Enabled:         r.Enabled,
		GroupsClaim:     groupsClaim,
		UpdatedAt:       time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("save oidc config: %w", err))
	}

	var respScopes []string
	_ = json.Unmarshal(cfg.Scopes, &respScopes)
	var respClaimMapping map[string]string
	_ = json.Unmarshal(cfg.ClaimMapping, &respClaimMapping)
	return connect.NewResponse(&orkestraV1.OIDCConfig{
		Enabled:      cfg.Enabled,
		IssuerUrl:    cfg.IssuerUrl,
		ClientId:     cfg.ClientID,
		Scopes:       respScopes,
		ClaimMapping: respClaimMapping,
		GroupsClaim:  cfg.GroupsClaim,
	}), nil
}

// ListAPIKeys returns API keys for the current user (or all if admin with user_id).
func (h *AuthServiceHandler) ListAPIKeys(ctx context.Context, req *connect.Request[orkestraV1.ListAPIKeysRequest]) (*connect.Response[orkestraV1.ListAPIKeysResponse], error) {
	u := masterauth.UserFromContext(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("not authenticated"))
	}
	userID := u.ID
	if req.Msg.UserId != "" {
		if err := requireRole(ctx, "admin"); err != nil {
			return nil, err
		}
		userID = req.Msg.UserId
	}
	rows, err := h.q.ListAPIKeysByUser(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list api keys: %w", err))
	}
	keys := make([]*orkestraV1.APIKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, apiKeyToProto(row, ""))
	}
	return connect.NewResponse(&orkestraV1.ListAPIKeysResponse{Keys: keys}), nil
}

// CreateAPIKey generates a new API key for the current user.
func (h *AuthServiceHandler) CreateAPIKey(ctx context.Context, req *connect.Request[orkestraV1.CreateAPIKeyRequest]) (*connect.Response[orkestraV1.APIKey], error) {
	u := masterauth.UserFromContext(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("not authenticated"))
	}
	if req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name required"))
	}

	rawKey, keyHash, err := generateAPIKey()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("generate key: %w", err))
	}
	var expiresAt *int64
	if req.Msg.ExpiresAt > 0 {
		expiresAt = &req.Msg.ExpiresAt
	}
	row, err := h.q.InsertAPIKey(ctx, store.InsertAPIKeyParams{
		ID:        uuid.NewString(),
		UserID:    u.ID,
		Name:      req.Msg.Name,
		KeyHash:   keyHash,
		CreatedAt: time.Now().UnixMilli(),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("insert api key: %w", err))
	}
	return connect.NewResponse(apiKeyToProto(row, rawKey)), nil
}

// RevokeAPIKey revokes an API key.
func (h *AuthServiceHandler) RevokeAPIKey(ctx context.Context, req *connect.Request[orkestraV1.RevokeAPIKeyRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	u := masterauth.UserFromContext(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("not authenticated"))
	}
	if err := h.q.RevokeAPIKey(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke api key: %w", err))
	}
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// ListEnrollmentTokens lists all enrollment tokens (admin only).
func (h *AuthServiceHandler) ListEnrollmentTokens(ctx context.Context, _ *connect.Request[orkestraV1.AuthEmpty]) (*connect.Response[orkestraV1.ListEnrollmentTokensResponse], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	rows, err := h.q.ListEnrollmentTokens(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list tokens: %w", err))
	}
	tokens := make([]*orkestraV1.EnrollmentToken, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, enrollmentTokenToProto(row, ""))
	}
	return connect.NewResponse(&orkestraV1.ListEnrollmentTokensResponse{Tokens: tokens}), nil
}

// CreateEnrollmentToken creates a new enrollment token (admin only).
func (h *AuthServiceHandler) CreateEnrollmentToken(ctx context.Context, req *connect.Request[orkestraV1.CreateEnrollmentTokenRequest]) (*connect.Response[orkestraV1.EnrollmentToken], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("use the existing enroll endpoint for token creation"))
}

// RevokeEnrollmentToken revokes an enrollment token (admin only).
func (h *AuthServiceHandler) RevokeEnrollmentToken(ctx context.Context, req *connect.Request[orkestraV1.RevokeEnrollmentTokenRequest]) (*connect.Response[orkestraV1.AuthEmpty], error) {
	if err := requireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	if err := h.q.RevokeEnrollmentToken(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke token: %w", err))
	}
	return connect.NewResponse(&orkestraV1.AuthEmpty{}), nil
}

// helpers

func userToProto(u store.User, roles []string, bindings []*orkestraV1.RoleBinding) *orkestraV1.User {
	var displayName string
	if u.DisplayName != nil {
		displayName = *u.DisplayName
	}
	var lastLogin int64
	if u.LastLoginAt != nil {
		lastLogin = *u.LastLoginAt
	}
	return &orkestraV1.User{
		Id:          u.ID,
		Username:    u.Username,
		DisplayName: displayName,
		HasPassword: u.PasswordHash != nil,
		HasOidc:     u.OidcSubject != nil,
		Disabled:    u.Disabled,
		CreatedAt:   u.CreatedAt,
		LastLoginAt: lastLogin,
		Roles:       roles,
		Bindings:    bindings,
	}
}

// loadUserBindings fetches and converts all role bindings for a user into proto form.
func (h *AuthServiceHandler) loadUserBindings(ctx context.Context, userID string) []*orkestraV1.RoleBinding {
	rows, err := h.q.ListRoleBindingsByUser(ctx, userID)
	if err != nil {
		return nil
	}
	out := make([]*orkestraV1.RoleBinding, 0, len(rows))
	for _, rb := range rows {
		out = append(out, roleBindingToProto(rb))
	}
	return out
}

func roleBindingToProto(rb store.RoleBinding) *orkestraV1.RoleBinding {
	// Strip "role-" prefix from role_id to get role name.
	role := rb.RoleID
	if len(role) > 5 && role[:5] == "role-" {
		role = role[5:]
	}
	return &orkestraV1.RoleBinding{
		Id:        rb.ID,
		UserId:    rb.UserID,
		Role:      role,
		ServerId:  derefStr(rb.ServerID),
		StackId:   derefStr(rb.StackID),
		CreatedAt: rb.CreatedAt,
	}
}

func enrollmentTokenToProto(t store.EnrollmentToken, rawToken string) *orkestraV1.EnrollmentToken {
	var desc string
	if t.Description != nil {
		desc = *t.Description
	}
	return &orkestraV1.EnrollmentToken{
		Id:          t.ID,
		Description: desc,
		TtlSeconds:  int32(t.TtlSeconds),
		MaxUses:     int32(t.MaxUses),
		UsedCount:   int32(t.UsedCount),
		ExpiresAt:   t.ExpiresAt,
		CreatedAt:   t.CreatedAt,
		Revoked:     t.Revoked,
		RawToken:    rawToken,
	}
}

func ipFromRequest(h http.Header) string {
	if xff := h.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return ""
}

func requireRole(ctx context.Context, roles ...string) error {
	u := masterauth.UserFromContext(ctx)
	if u == nil {
		return connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("not authenticated"))
	}
	if !masterauth.HasRole(u, roles...) {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("requires role: %v", roles))
	}
	return nil
}

func generateAPIKey() (rawKey, keyHash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	rawKey = "ork_" + base64.URLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(rawKey))
	keyHash = fmt.Sprintf("%x", h)
	return
}

func apiKeyToProto(k store.ApiKey, rawKey string) *orkestraV1.APIKey {
	p := &orkestraV1.APIKey{
		Id:        k.ID,
		UserId:    k.UserID,
		Name:      k.Name,
		RawKey:    rawKey,
		CreatedAt: k.CreatedAt,
		Revoked:   k.Revoked,
	}
	if k.LastUsedAt != nil {
		p.LastUsedAt = *k.LastUsedAt
	}
	if k.ExpiresAt != nil {
		p.ExpiresAt = *k.ExpiresAt
	}
	return p
}

// validatePassword checks pw against the stored policy (no-op if no policy is configured).
func (h *AuthServiceHandler) validatePassword(ctx context.Context, pw string) error {
	policy, err := h.q.GetPasswordPolicy(ctx)
	if err != nil {
		return nil // no policy configured — all passwords accepted
	}
	if err := masterauth.ValidatePassword(policy, pw); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return nil
}

// publicURL derives the base URL for email links from the SMTP config or the request Host header.
func (h *AuthServiceHandler) publicURL(header http.Header) string {
	cfg, err := h.q.GetSMTPConfig(context.Background())
	if err == nil && cfg.PublicUrl != "" {
		return cfg.PublicUrl
	}
	host := header.Get("X-Forwarded-Host")
	if host == "" {
		host = header.Get("Host")
	}
	if host == "" {
		return "http://localhost:8080"
	}
	return "http://" + host
}

// generateResetToken returns a (rawToken, tokenHash) pair for password-reset/invite flows.
// rawToken is sent to the user; only tokenHash is stored in the DB.
func generateResetToken() (rawToken, tokenHash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	rawToken = base64.URLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash = hex.EncodeToString(sum[:])
	return rawToken, tokenHash
}

// hashResetToken derives the DB key from a raw reset token.
func hashResetToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// policyToProto converts a store.PasswordPolicy to the proto message.
func policyToProto(p store.PasswordPolicy) *orkestraV1.PasswordPolicy {
	return &orkestraV1.PasswordPolicy{
		MinLength:  int32(p.MinLength),
		SpecialMin: int32(p.SpecialMin),
		SpecialMax: int32(p.SpecialMax),
		DigitMin:   int32(p.DigitMin),
		DigitMax:   int32(p.DigitMax),
		UpperMin:   int32(p.UpperMin),
		UpperMax:   int32(p.UpperMax),
		LowerMin:   int32(p.LowerMin),
		LowerMax:   int32(p.LowerMax),
	}
}

func (h *AuthServiceHandler) auditAuth(ctx context.Context, user *store.User, action string, detail *string) {
	p := store.InsertAuditLogParams{
		Ts:         time.Now().UnixMilli(),
		Action:     action,
		TargetType: "user",
		Error:      nil,
	}
	if user != nil {
		p.ActorID = &user.ID
		p.ActorName = &user.Username
		p.TargetID = &user.ID
	}
	if detail != nil {
		p.Error = detail
	}
	if err := h.q.InsertAuditLog(ctx, p); err != nil {
		slog.Warn("audit log insert failed", "action", action, "err", err)
	}
}
