-- name: DeleteEventsBefore :execrows
-- Deletes at most batch_size events older than cutoff. Bounded so the retention job
-- never holds a long lock; the caller loops until a batch comes back short.
DELETE FROM events WHERE id IN (
    SELECT id FROM events WHERE ts < @cutoff::bigint LIMIT sqlc.arg(batch_size)
);

-- name: DeleteAuditLogBefore :execrows
-- Deletes at most batch_size audit entries older than cutoff. Only ever called when an
-- admin has explicitly configured an audit retention window.
DELETE FROM audit_log WHERE id IN (
    SELECT id FROM audit_log WHERE ts < @cutoff::bigint LIMIT sqlc.arg(batch_size)
);

-- name: DeleteExpiredSessions :execrows
-- Deletes at most batch_size sessions whose expiry has passed. Expired rows can no longer
-- authenticate anyone (GetSession filters on expires_at), so they are pure ballast.
DELETE FROM sessions WHERE id IN (
    SELECT id FROM sessions WHERE expires_at < @cutoff::bigint LIMIT sqlc.arg(batch_size)
);
