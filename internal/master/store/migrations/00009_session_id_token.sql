-- +goose Up
-- +goose StatementBegin

-- Store the OIDC id_token of an SSO-authenticated session so logout can perform
-- RP-initiated (single) logout — passing it as id_token_hint to the provider's
-- end_session_endpoint. NULL for local (password) sessions.
ALTER TABLE sessions ADD COLUMN oidc_id_token TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE sessions DROP COLUMN oidc_id_token;

-- +goose StatementEnd
