-- name: InsertEnrollmentToken :one
INSERT INTO enrollment_tokens (id, token_hash, description, ttl_seconds, max_uses, created_by, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetEnrollmentTokenByHash :one
SELECT * FROM enrollment_tokens WHERE token_hash = $1;

-- name: IncrementTokenUsage :exec
UPDATE enrollment_tokens SET used_count = used_count + 1 WHERE id = $1;

-- name: RevokeEnrollmentToken :exec
UPDATE enrollment_tokens SET revoked = TRUE WHERE id = $1;

-- name: ListEnrollmentTokens :many
SELECT * FROM enrollment_tokens ORDER BY created_at DESC;

-- name: InsertCertificate :exec
INSERT INTO certificates (serial, agent_id, fingerprint, cert_pem, not_before, not_after, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetCertificateByFingerprint :one
SELECT * FROM certificates WHERE fingerprint = $1;

-- name: RevokeCertificate :exec
UPDATE certificates SET revoked = TRUE, revoked_at = $1 WHERE serial = $2;

-- name: GetActiveCertificateForAgent :one
SELECT * FROM certificates WHERE agent_id = $1 AND revoked = FALSE ORDER BY not_after DESC LIMIT 1;
