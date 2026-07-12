-- name: UpsertAgentUpdatePolicy :one
INSERT INTO update_policies (server_id, layer, mode, window_cron, auto_reboot, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (server_id, layer) WHERE server_id IS NOT NULL
DO UPDATE SET mode = EXCLUDED.mode, window_cron = EXCLUDED.window_cron,
              auto_reboot = EXCLUDED.auto_reboot, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: UpsertFleetUpdatePolicy :one
INSERT INTO update_policies (server_id, layer, mode, window_cron, auto_reboot, updated_at)
VALUES (NULL, $1, $2, $3, $4, $5)
ON CONFLICT (layer) WHERE server_id IS NULL
DO UPDATE SET mode = EXCLUDED.mode, window_cron = EXCLUDED.window_cron,
              auto_reboot = EXCLUDED.auto_reboot, updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: ResolveUpdatePolicy :one
SELECT * FROM update_policies
WHERE layer = $2 AND (server_id = $1 OR server_id IS NULL)
ORDER BY (server_id IS NULL)
LIMIT 1;

-- name: ListUpdatePolicies :many
SELECT * FROM update_policies ORDER BY server_id NULLS FIRST, layer;

-- name: UpsertAvailableUpdate :one
INSERT INTO available_updates (server_id, layer, current_version, candidate_version, detail, detected_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (server_id, layer)
DO UPDATE SET current_version = EXCLUDED.current_version,
              candidate_version = EXCLUDED.candidate_version,
              detail = EXCLUDED.detail, detected_at = EXCLUDED.detected_at
RETURNING *;

-- name: ListAvailableUpdatesForServer :many
SELECT * FROM available_updates WHERE server_id = $1 ORDER BY layer;

-- name: DeleteAvailableUpdate :exec
DELETE FROM available_updates WHERE server_id = $1 AND layer = $2;
