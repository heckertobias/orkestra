-- name: GetSMTPConfig :one
SELECT * FROM smtp_config LIMIT 1;

-- name: UpsertSMTPConfig :one
INSERT INTO smtp_config (id, enabled, host, port, username, password_enc, from_address, public_url, starttls, updated_at)
VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET
    enabled      = excluded.enabled,
    host         = excluded.host,
    port         = excluded.port,
    username     = excluded.username,
    password_enc = excluded.password_enc,
    from_address = excluded.from_address,
    public_url   = excluded.public_url,
    starttls     = excluded.starttls,
    updated_at   = excluded.updated_at
RETURNING *;

-- name: GetPasswordPolicy :one
SELECT * FROM password_policy LIMIT 1;

-- name: UpsertPasswordPolicy :one
INSERT INTO password_policy (id, min_length, special_min, special_max, digit_min, digit_max, upper_min, upper_max, lower_min, lower_max, updated_at)
VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE SET
    min_length  = excluded.min_length,
    special_min = excluded.special_min,
    special_max = excluded.special_max,
    digit_min   = excluded.digit_min,
    digit_max   = excluded.digit_max,
    upper_min   = excluded.upper_min,
    upper_max   = excluded.upper_max,
    lower_min   = excluded.lower_min,
    lower_max   = excluded.lower_max,
    updated_at  = excluded.updated_at
RETURNING *;

-- name: InsertPasswordResetToken :exec
INSERT INTO password_reset_tokens (id, user_id, token_hash, purpose, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetPasswordResetTokenByHash :one
SELECT * FROM password_reset_tokens WHERE token_hash = $1;

-- name: MarkPasswordResetTokenUsed :exec
UPDATE password_reset_tokens SET used_at = $2 WHERE id = $1;

-- name: InsertEmailChangeToken :exec
INSERT INTO password_reset_tokens (id, user_id, token_hash, purpose, new_email, expires_at, created_at)
VALUES ($1, $2, $3, 'email_change', $4, $5, $6);
