-- +goose Up
-- +goose StatementBegin

-- Per-user "SSO-only" flag. When set, the user may authenticate only via OIDC:
-- no invite email is sent, and every path that sets or uses a local password is
-- rejected server-side. The password_hash is left intact (dormant) so toggling
-- the flag off restores local login without data loss.
ALTER TABLE users ADD COLUMN sso_only BOOLEAN NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE users DROP COLUMN sso_only;

-- +goose StatementEnd
