-- +goose Up
-- +goose StatementBegin

-- ── Server configuration (single-row) ────────────────────────────────────────
-- Deployment-wide settings that are set in the UI and override startup env vars.
-- public_url is the browser-facing base URL (scheme + host); it takes precedence
-- over ORKESTRA_PUBLIC_URL for the OIDC redirect, email links, and the setup link.
CREATE TABLE server_config (
    id         INTEGER PRIMARY KEY,
    public_url TEXT    NOT NULL DEFAULT '',  -- browser-facing base URL, e.g. https://orkestra.example.com
    updated_at BIGINT  NOT NULL DEFAULT 0
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS server_config;

-- +goose StatementEnd
