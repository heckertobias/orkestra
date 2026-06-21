-- +goose Up
-- +goose StatementBegin

-- ── OIDC: groups claim ────────────────────────────────────────────────────────
ALTER TABLE oidc_config ADD COLUMN groups_claim TEXT NOT NULL DEFAULT 'groups';

-- ── SMTP configuration (single-row, KEK-encrypted password) ──────────────────
CREATE TABLE smtp_config (
    id           INTEGER PRIMARY KEY,
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    host         TEXT    NOT NULL DEFAULT '',
    port         INT     NOT NULL DEFAULT 587,
    username     TEXT    NOT NULL DEFAULT '',
    password_enc TEXT    NOT NULL DEFAULT '',  -- encrypted with KEK
    from_address TEXT    NOT NULL DEFAULT '',
    public_url   TEXT    NOT NULL DEFAULT '',  -- base URL for email links
    starttls     BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at   BIGINT  NOT NULL
);

-- ── Password policy (single-row; 0 = no limit) ───────────────────────────────
CREATE TABLE password_policy (
    id          INTEGER PRIMARY KEY,
    min_length  INT NOT NULL DEFAULT 0,
    special_min INT NOT NULL DEFAULT 0,
    special_max INT NOT NULL DEFAULT 0,
    digit_min   INT NOT NULL DEFAULT 0,
    digit_max   INT NOT NULL DEFAULT 0,
    upper_min   INT NOT NULL DEFAULT 0,
    upper_max   INT NOT NULL DEFAULT 0,
    lower_min   INT NOT NULL DEFAULT 0,
    lower_max   INT NOT NULL DEFAULT 0,
    updated_at  BIGINT NOT NULL DEFAULT 0
);

-- ── Password reset / invite tokens ───────────────────────────────────────────
CREATE TABLE password_reset_tokens (
    id         TEXT   PRIMARY KEY,
    user_id    TEXT   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT   NOT NULL UNIQUE,
    purpose    TEXT   NOT NULL,  -- 'invite' | 'reset'
    expires_at BIGINT NOT NULL,
    used_at    BIGINT,
    created_at BIGINT NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS password_reset_tokens;
DROP TABLE IF EXISTS password_policy;
DROP TABLE IF EXISTS smtp_config;
ALTER TABLE oidc_config DROP COLUMN IF EXISTS groups_claim;

-- +goose StatementEnd
