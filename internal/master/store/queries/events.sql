-- name: InsertEvent :exec
INSERT INTO events (ts, server_id, stack_id, event_type, severity, message, detail_json)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListRecentEvents :many
SELECT * FROM events ORDER BY ts DESC LIMIT $1;

-- name: ListEventsFiltered :many
SELECT * FROM events
WHERE (@server_id::text IS NULL OR server_id = @server_id)
  AND (@stack_id::text IS NULL OR stack_id = @stack_id)
ORDER BY ts DESC LIMIT 100;

-- name: ListEventsAfter :many
SELECT * FROM events
WHERE id > @after_id
  AND (@server_id::text IS NULL OR server_id = @server_id)
  AND (@stack_id::text IS NULL OR stack_id = @stack_id)
ORDER BY id ASC;
