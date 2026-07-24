-- +goose Up
-- +goose StatementBegin

-- The browser-facing public URL is now a single deployment-wide setting
-- (server_config.public_url). Retire the email-specific override: promote any
-- existing smtp_config.public_url to the global setting (unless already set),
-- then drop the column.
INSERT INTO server_config (id, public_url, updated_at)
SELECT 1, public_url, updated_at FROM smtp_config WHERE public_url <> ''
ON CONFLICT (id) DO UPDATE SET
    public_url = EXCLUDED.public_url,
    updated_at = EXCLUDED.updated_at
WHERE server_config.public_url = '';

ALTER TABLE smtp_config DROP COLUMN public_url;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE smtp_config ADD COLUMN public_url TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd
