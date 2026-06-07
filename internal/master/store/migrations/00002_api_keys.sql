-- +goose Up
-- +goose StatementBegin

CREATE TABLE api_keys (
    id           TEXT    PRIMARY KEY,
    user_id      TEXT    NOT NULL REFERENCES users(id),
    name         TEXT    NOT NULL,
    key_hash     TEXT    NOT NULL UNIQUE,  -- SHA-256 of raw key (raw never stored)
    created_at   BIGINT  NOT NULL,
    last_used_at BIGINT,
    expires_at   BIGINT,
    revoked      BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_api_keys_user ON api_keys(user_id);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS api_keys;
-- +goose StatementEnd
