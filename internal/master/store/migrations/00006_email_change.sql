-- +goose Up
-- +goose StatementBegin

-- Add new_email to password_reset_tokens so that 'email_change' tokens
-- carry the desired new address without touching the user row until confirmed.
ALTER TABLE password_reset_tokens ADD COLUMN new_email TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE password_reset_tokens DROP COLUMN IF EXISTS new_email;

-- +goose StatementEnd
