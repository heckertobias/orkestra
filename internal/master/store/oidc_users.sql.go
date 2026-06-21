package store

import "context"

const getUserByOIDCSubject = `-- name: GetUserByOIDCSubject :one
SELECT id, username, display_name, password_hash, oidc_subject, disabled, created_at, last_login_at
FROM users WHERE oidc_subject = $1 AND disabled = false LIMIT 1
`

func (q *Queries) GetUserByOIDCSubject(ctx context.Context, sub string) (User, error) {
	row := q.db.QueryRow(ctx, getUserByOIDCSubject, sub)
	var i User
	err := row.Scan(
		&i.ID, &i.Username, &i.DisplayName, &i.PasswordHash,
		&i.OidcSubject, &i.Disabled, &i.CreatedAt, &i.LastLoginAt,
	)
	return i, err
}

const setOIDCSubject = `-- name: SetOIDCSubject :exec
UPDATE users SET oidc_subject = $2 WHERE id = $1
`

// SetOIDCSubject links an existing user account to an OIDC subject on first login.
func (q *Queries) SetOIDCSubject(ctx context.Context, id, sub string) error {
	_, err := q.db.Exec(ctx, setOIDCSubject, id, sub)
	return err
}

// InsertOIDCUser is kept for reference but should no longer be called.
// New flow: only pre-created users may log in via OIDC (resolveOIDCUser).
const insertOIDCUser = `-- name: InsertOIDCUser :one
INSERT INTO users (id, username, display_name, oidc_subject, disabled, created_at)
VALUES (gen_random_uuid()::text, $1, $2, $3, false, $4)
RETURNING id, username, display_name, password_hash, oidc_subject, disabled, created_at, last_login_at
`

type InsertOIDCUserParams struct {
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	OIDCSubject string  `json:"oidc_subject"`
	CreatedAt   int64   `json:"created_at"`
}

func (q *Queries) InsertOIDCUser(ctx context.Context, arg InsertOIDCUserParams) (User, error) {
	row := q.db.QueryRow(ctx, insertOIDCUser,
		arg.Username, arg.DisplayName, arg.OIDCSubject, arg.CreatedAt,
	)
	var i User
	err := row.Scan(
		&i.ID, &i.Username, &i.DisplayName, &i.PasswordHash,
		&i.OidcSubject, &i.Disabled, &i.CreatedAt, &i.LastLoginAt,
	)
	return i, err
}

