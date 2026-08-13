-- name: UpsertAgentState :exec
INSERT INTO agent_state (server_id, state_json, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (server_id) DO UPDATE SET
    state_json = EXCLUDED.state_json,
    updated_at = EXCLUDED.updated_at;

-- name: GetAgentState :one
SELECT * FROM agent_state WHERE server_id = $1;
