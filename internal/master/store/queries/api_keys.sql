-- name: InsertAPIKey :one
INSERT INTO api_keys (id, user_id, name, key_hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = $1 AND revoked = false;

-- name: ListAPIKeysByUser :many
SELECT * FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC;

-- name: ListAllAPIKeys :many
SELECT * FROM api_keys ORDER BY created_at DESC;

-- name: RevokeAPIKey :exec
UPDATE api_keys SET revoked = true WHERE id = $1;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = $2 WHERE id = $1;
