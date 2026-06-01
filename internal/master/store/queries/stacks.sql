-- name: ListStacks :many
SELECT * FROM stacks WHERE deleted_at IS NULL ORDER BY name;

-- name: GetStack :one
SELECT * FROM stacks WHERE id = ? AND deleted_at IS NULL;

-- name: InsertStack :one
INSERT INTO stacks (id, name, description, owner, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: SoftDeleteStack :exec
UPDATE stacks SET deleted_at = ? WHERE id = ?;

-- name: InsertStackVersion :one
INSERT INTO stack_versions (id, stack_id, version, compose_yaml, env_vars, secret_refs, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetStackVersion :one
SELECT * FROM stack_versions WHERE id = ?;

-- name: GetLatestStackVersion :one
SELECT * FROM stack_versions WHERE stack_id = ? ORDER BY version DESC LIMIT 1;

-- name: ListStackVersions :many
SELECT * FROM stack_versions WHERE stack_id = ? ORDER BY version DESC;

-- name: GetNextVersionNumber :one
SELECT COALESCE(MAX(version), 0) + 1 AS next_version FROM stack_versions WHERE stack_id = ?;

-- name: UpsertAssignment :one
INSERT INTO assignments (id, server_id, stack_id, stack_version_id, desired_status, assigned_by, assigned_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(server_id, stack_id) DO UPDATE SET
    stack_version_id = excluded.stack_version_id,
    desired_status   = excluded.desired_status,
    assigned_by      = excluded.assigned_by,
    assigned_at      = excluded.assigned_at
RETURNING *;

-- name: DeleteAssignment :exec
DELETE FROM assignments WHERE server_id = ? AND stack_id = ?;

-- name: ListAssignmentsForServer :many
SELECT a.*, sv.compose_yaml, sv.env_vars, sv.secret_refs
FROM assignments a
JOIN stack_versions sv ON sv.id = a.stack_version_id
WHERE a.server_id = ?;

-- name: ListAssignmentsForStack :many
SELECT * FROM assignments WHERE stack_id = ?;
