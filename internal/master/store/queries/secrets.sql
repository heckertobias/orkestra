-- name: ListSecrets :many
SELECT id, name, description, provider, version, bao_mount, bao_path, bao_key, created_by, created_at, updated_at
FROM secrets ORDER BY name;

-- name: GetSecret :one
SELECT * FROM secrets WHERE id = $1;

-- name: InsertSecret :one
INSERT INTO secrets (id, name, description, provider, ciphertext, version, bao_mount, bao_path, bao_key, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: UpdateSecret :one
UPDATE secrets
SET description = $2, ciphertext = $3, version = version + 1,
    bao_mount = $4, bao_path = $5, bao_key = $6, updated_at = $7
WHERE id = $1
RETURNING *;

-- name: DeleteSecret :exec
DELETE FROM secrets WHERE id = $1;

-- name: CountSecretBindings :one
SELECT COUNT(*) FROM secret_bindings WHERE secret_id = $1;

-- name: InsertAuditLog :exec
INSERT INTO audit_log (ts, actor_id, actor_name, action, target_type, target_id, ip_address, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAuditLog :many
SELECT * FROM audit_log ORDER BY ts DESC LIMIT $1;
