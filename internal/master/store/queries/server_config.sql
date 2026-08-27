-- name: GetServerConfig :one
SELECT * FROM server_config LIMIT 1;

-- name: UpsertServerConfig :one
INSERT INTO server_config (id, public_url, events_retention_days, audit_retention_days, updated_at)
VALUES (1, $1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET
    public_url            = excluded.public_url,
    events_retention_days = excluded.events_retention_days,
    audit_retention_days  = excluded.audit_retention_days,
    updated_at            = excluded.updated_at
RETURNING *;
