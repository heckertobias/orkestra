package store

import "context"

const insertAPIKey = `-- name: InsertAPIKey :one
INSERT INTO api_keys (id, user_id, name, key_hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, user_id, name, key_hash, created_at, last_used_at, expires_at, revoked
`

type InsertAPIKeyParams struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	KeyHash   string  `json:"key_hash"`
	CreatedAt int64   `json:"created_at"`
	ExpiresAt *int64  `json:"expires_at"`
}

func (q *Queries) InsertAPIKey(ctx context.Context, arg InsertAPIKeyParams) (APIKey, error) {
	row := q.db.QueryRow(ctx, insertAPIKey,
		arg.ID, arg.UserID, arg.Name, arg.KeyHash, arg.CreatedAt, arg.ExpiresAt,
	)
	var i APIKey
	err := row.Scan(
		&i.ID, &i.UserID, &i.Name, &i.KeyHash,
		&i.CreatedAt, &i.LastUsedAt, &i.ExpiresAt, &i.Revoked,
	)
	return i, err
}

const getAPIKeyByHash = `-- name: GetAPIKeyByHash :one
SELECT id, user_id, name, key_hash, created_at, last_used_at, expires_at, revoked
FROM api_keys WHERE key_hash = $1 AND revoked = false
`

func (q *Queries) GetAPIKeyByHash(ctx context.Context, keyHash string) (APIKey, error) {
	row := q.db.QueryRow(ctx, getAPIKeyByHash, keyHash)
	var i APIKey
	err := row.Scan(
		&i.ID, &i.UserID, &i.Name, &i.KeyHash,
		&i.CreatedAt, &i.LastUsedAt, &i.ExpiresAt, &i.Revoked,
	)
	return i, err
}

const listAPIKeysByUser = `-- name: ListAPIKeysByUser :many
SELECT id, user_id, name, key_hash, created_at, last_used_at, expires_at, revoked
FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC
`

func (q *Queries) ListAPIKeysByUser(ctx context.Context, userID string) ([]APIKey, error) {
	return q.scanAPIKeys(ctx, listAPIKeysByUser, userID)
}

const listAllAPIKeys = `-- name: ListAllAPIKeys :many
SELECT id, user_id, name, key_hash, created_at, last_used_at, expires_at, revoked
FROM api_keys ORDER BY created_at DESC
`

func (q *Queries) ListAllAPIKeys(ctx context.Context) ([]APIKey, error) {
	return q.scanAPIKeys(ctx, listAllAPIKeys)
}

func (q *Queries) scanAPIKeys(ctx context.Context, query string, args ...interface{}) ([]APIKey, error) {
	rows, err := q.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []APIKey
	for rows.Next() {
		var i APIKey
		if err := rows.Scan(
			&i.ID, &i.UserID, &i.Name, &i.KeyHash,
			&i.CreatedAt, &i.LastUsedAt, &i.ExpiresAt, &i.Revoked,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const revokeAPIKey = `-- name: RevokeAPIKey :exec
UPDATE api_keys SET revoked = true WHERE id = $1
`

func (q *Queries) RevokeAPIKey(ctx context.Context, id string) error {
	_, err := q.db.Exec(ctx, revokeAPIKey, id)
	return err
}

const touchAPIKey = `-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = $2 WHERE id = $1
`

func (q *Queries) TouchAPIKey(ctx context.Context, id string, lastUsedAt int64) error {
	_, err := q.db.Exec(ctx, touchAPIKey, id, lastUsedAt)
	return err
}
