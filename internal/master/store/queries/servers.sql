-- name: GetServer :one
SELECT * FROM servers WHERE id = $1 AND deleted_at IS NULL;

-- name: ListServers :many
SELECT * FROM servers WHERE deleted_at IS NULL ORDER BY name;

-- name: InsertServer :one
INSERT INTO servers (id, name, hostname, arch, os, agent_version, docker_version, labels, status, enrolled_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'offline', $9)
RETURNING *;

-- name: UpdateServerStatus :exec
UPDATE servers SET status = $1, last_seen_at = $2 WHERE id = $3;

-- name: UpdateServer :one
UPDATE servers SET name = $1, labels = $2 WHERE id = $3 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteServer :exec
UPDATE servers SET deleted_at = $1 WHERE id = $2;

-- name: MarkOfflineServers :exec
UPDATE servers SET status = 'offline'
WHERE status = 'online' AND last_seen_at < $1;
