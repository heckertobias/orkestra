-- name: InsertEvent :exec
INSERT INTO events (ts, server_id, stack_id, event_type, severity, message, detail_json)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListRecentEvents :many
SELECT * FROM events ORDER BY ts DESC LIMIT $1;

-- name: ListEventsFiltered :many
SELECT * FROM events
WHERE ($1::text IS NULL OR server_id = $1)
  AND ($2::text IS NULL OR stack_id = $2)
ORDER BY ts DESC LIMIT 100;
