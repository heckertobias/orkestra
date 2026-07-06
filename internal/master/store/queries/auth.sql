-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;

-- name: InsertUser :one
INSERT INTO users (id, username, display_name, password_hash, disabled, created_at, sso_only)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateUser :one
UPDATE users SET username = $2, display_name = $3, disabled = $4, sso_only = $5 WHERE id = $1 RETURNING *;

-- name: UpdateDisplayName :one
UPDATE users SET display_name = $2 WHERE id = $1 RETURNING *;

-- name: SetUsername :one
UPDATE users SET username = $2 WHERE id = $1 RETURNING *;

-- name: DeleteUserByID :exec
DELETE FROM users WHERE id = $1;

-- name: SetPasswordHash :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: SetLastLogin :exec
UPDATE users SET last_login_at = $2 WHERE id = $1;

-- name: InsertSession :exec
INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen, ip_address, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1 AND revoked = false AND expires_at > $2;

-- name: TouchSession :exec
UPDATE sessions SET last_seen = $2 WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked = true WHERE id = $1;

-- name: RevokeAllSessionsForUser :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: ListRoleBindingsByUser :many
SELECT * FROM role_bindings WHERE user_id = $1 ORDER BY created_at;

-- name: ListAllRoleBindings :many
SELECT * FROM role_bindings ORDER BY created_at;

-- name: GetRoleBinding :one
SELECT * FROM role_bindings WHERE id = $1;

-- name: InsertRoleBinding :one
INSERT INTO role_bindings (id, user_id, role_id, server_id, stack_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteRoleBinding :exec
DELETE FROM role_bindings WHERE id = $1;

-- name: CountEnabledGlobalAdminsExcludingUser :one
-- Count enabled users with a global admin role binding, excluding the given user.
-- Used to enforce the invariant that at least one enabled global admin always remains.
SELECT COUNT(DISTINCT u.id) FROM users u
JOIN role_bindings rb ON rb.user_id = u.id
WHERE u.disabled = false
  AND rb.role_id = 'role-admin'
  AND rb.server_id IS NULL
  AND rb.stack_id IS NULL
  AND u.id <> $1;

-- name: GetUserRoles :many
SELECT r.name FROM role_bindings rb
JOIN roles r ON r.id = rb.role_id
WHERE rb.user_id = $1 AND rb.server_id IS NULL AND rb.stack_id IS NULL;

-- name: GetUserRoleBindings :many
-- Returns all role bindings (global + scoped) for a user, including role name and scope columns.
SELECT rb.id, r.name AS role_name, rb.server_id, rb.stack_id
FROM role_bindings rb
JOIN roles r ON r.id = rb.role_id
WHERE rb.user_id = $1
ORDER BY rb.created_at;
