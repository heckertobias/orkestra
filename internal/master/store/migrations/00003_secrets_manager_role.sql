-- +goose Up
-- +goose StatementBegin
INSERT INTO roles (id, name, description)
VALUES ('role-secrets-manager', 'secrets-manager', 'Create, update, delete, and reveal secrets');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM roles WHERE id = 'role-secrets-manager';
-- +goose StatementEnd
