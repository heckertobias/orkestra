-- name: InsertEnrollmentToken :one
INSERT INTO enrollment_tokens (id, token_hash, description, ttl_seconds, max_uses, created_by, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetEnrollmentTokenByHash :one
SELECT * FROM enrollment_tokens WHERE token_hash = ?;

-- name: IncrementTokenUsage :exec
UPDATE enrollment_tokens SET used_count = used_count + 1 WHERE id = ?;

-- name: RevokeEnrollmentToken :exec
UPDATE enrollment_tokens SET revoked = 1 WHERE id = ?;

-- name: ListEnrollmentTokens :many
SELECT * FROM enrollment_tokens ORDER BY created_at DESC;

-- name: InsertCertificate :exec
INSERT INTO certificates (serial, agent_id, fingerprint, cert_pem, not_before, not_after, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetCertificateByFingerprint :one
SELECT * FROM certificates WHERE fingerprint = ?;

-- name: RevokeCertificate :exec
UPDATE certificates SET revoked = 1, revoked_at = ? WHERE serial = ?;

-- name: GetActiveCertificateForAgent :one
SELECT * FROM certificates WHERE agent_id = ? AND revoked = 0 ORDER BY not_after DESC LIMIT 1;
