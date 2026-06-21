-- name: GetOIDCConfig :one
SELECT * FROM oidc_config LIMIT 1;

-- name: UpsertOIDCConfig :one
INSERT INTO oidc_config (id, issuer_url, client_id, client_secret_enc, scopes, claim_mapping, enabled, groups_claim, updated_at)
VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    issuer_url        = excluded.issuer_url,
    client_id         = excluded.client_id,
    client_secret_enc = excluded.client_secret_enc,
    scopes            = excluded.scopes,
    claim_mapping     = excluded.claim_mapping,
    enabled           = excluded.enabled,
    groups_claim      = excluded.groups_claim,
    updated_at        = excluded.updated_at
RETURNING *;
