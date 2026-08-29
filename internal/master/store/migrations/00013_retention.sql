-- +goose Up
-- +goose StatementBegin

-- ── Retention windows (deployment-wide) ──────────────────────────────────────
-- How long the Master keeps rows in the append-only tables, in days. Three states:
--   -1  unset — inherit the startup default (ORKESTRA_EVENTS_RETENTION_DAYS /
--       ORKESTRA_AUDIT_RETENTION_DAYS); this is what an empty field in the UI means
--    0  keep forever — never delete from this table
--   >0  delete rows older than this many days
-- audit_log defaults to "keep forever" at every layer: the audit trail is the
-- compliance surface and must never shrink without someone asking for it.
ALTER TABLE server_config
    ADD COLUMN events_retention_days INTEGER NOT NULL DEFAULT -1,
    ADD COLUMN audit_retention_days  INTEGER NOT NULL DEFAULT -1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE server_config
    DROP COLUMN events_retention_days,
    DROP COLUMN audit_retention_days;

-- +goose StatementEnd
