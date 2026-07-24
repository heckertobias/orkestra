-- name: GetServerConfig :one
SELECT * FROM server_config LIMIT 1;

-- name: UpsertServerConfig :one
INSERT INTO server_config (id, public_url, updated_at)
VALUES (1, $1, $2)
ON CONFLICT (id) DO UPDATE SET
    public_url = excluded.public_url,
    updated_at = excluded.updated_at
RETURNING *;
