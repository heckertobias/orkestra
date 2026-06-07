package store

import (
	"context"
	"fmt"
)

const getOIDCConfig = `-- name: GetOIDCConfig :one
SELECT id, issuer_url, client_id, client_secret_enc, scopes, claim_mapping, enabled, updated_at
FROM oidc_config LIMIT 1
`

func (q *Queries) GetOIDCConfig(ctx context.Context) (OidcConfig, error) {
	row := q.db.QueryRow(ctx, getOIDCConfig)
	var i OidcConfig
	err := row.Scan(
		&i.ID, &i.IssuerUrl, &i.ClientID, &i.ClientSecretEnc,
		&i.Scopes, &i.ClaimMapping, &i.Enabled, &i.UpdatedAt,
	)
	return i, err
}

const upsertOIDCConfig = `-- name: UpsertOIDCConfig :one
INSERT INTO oidc_config (id, issuer_url, client_id, client_secret_enc, scopes, claim_mapping, enabled, updated_at)
VALUES (1, $1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE SET
    issuer_url        = excluded.issuer_url,
    client_id         = excluded.client_id,
    client_secret_enc = excluded.client_secret_enc,
    scopes            = excluded.scopes,
    claim_mapping     = excluded.claim_mapping,
    enabled           = excluded.enabled,
    updated_at        = excluded.updated_at
RETURNING id, issuer_url, client_id, client_secret_enc, scopes, claim_mapping, enabled, updated_at
`

type UpsertOIDCConfigParams struct {
	IssuerUrl       string `json:"issuer_url"`
	ClientID        string `json:"client_id"`
	ClientSecretEnc string `json:"client_secret_enc"`
	Scopes          []byte `json:"scopes"`
	ClaimMapping    []byte `json:"claim_mapping"`
	Enabled         bool   `json:"enabled"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (q *Queries) UpsertOIDCConfig(ctx context.Context, arg UpsertOIDCConfigParams) (OidcConfig, error) {
	row := q.db.QueryRow(ctx, upsertOIDCConfig,
		arg.IssuerUrl, arg.ClientID, arg.ClientSecretEnc,
		arg.Scopes, arg.ClaimMapping, arg.Enabled, arg.UpdatedAt,
	)
	var i OidcConfig
	err := row.Scan(
		&i.ID, &i.IssuerUrl, &i.ClientID, &i.ClientSecretEnc,
		&i.Scopes, &i.ClaimMapping, &i.Enabled, &i.UpdatedAt,
	)
	if err != nil {
		return OidcConfig{}, fmt.Errorf("upsert oidc config: %w", err)
	}
	return i, nil
}
