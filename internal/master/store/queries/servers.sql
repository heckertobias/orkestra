-- name: GetServer :one
SELECT * FROM servers WHERE id = ? AND deleted_at IS NULL;

-- name: ListServers :many
SELECT * FROM servers WHERE deleted_at IS NULL ORDER BY name;

-- name: InsertServer :one
INSERT INTO servers (id, name, hostname, arch, os, agent_version, docker_version, labels, status, enrolled_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'offline', ?)
RETURNING *;

-- name: UpdateServerStatus :exec
UPDATE servers SET status = ?, last_seen_at = ? WHERE id = ?;

-- name: UpdateServer :one
UPDATE servers SET name = ?, labels = ? WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteServer :exec
UPDATE servers SET deleted_at = ? WHERE id = ?;

-- name: MarkOfflineServers :exec
UPDATE servers SET status = 'offline'
WHERE status = 'online' AND last_seen_at < ?;
