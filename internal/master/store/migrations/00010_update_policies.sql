-- +goose Up
-- +goose StatementBegin

-- Per-(agent, layer) update behaviour, plus fleet-wide defaults. A NULL server_id
-- row is the fleet default; an agent-specific row (server_id set) overrides it for
-- that layer. See docs/09-updates.md § "Policy model".
CREATE TABLE update_policies (
    server_id    TEXT    REFERENCES servers(id) ON DELETE CASCADE, -- NULL = fleet default
    layer        TEXT    NOT NULL CHECK (layer IN ('orkestra', 'images', 'os')),
    mode         TEXT    NOT NULL CHECK (mode IN ('manual', 'automatic')),
    window_cron  TEXT,
    auto_reboot  BOOLEAN NOT NULL DEFAULT false,
    updated_at   BIGINT  NOT NULL
);

-- One agent-specific policy per (server, layer)...
CREATE UNIQUE INDEX idx_update_policies_agent ON update_policies (server_id, layer) WHERE server_id IS NOT NULL;
-- ...and one fleet default per layer.
CREATE UNIQUE INDEX idx_update_policies_fleet ON update_policies (layer) WHERE server_id IS NULL;

-- Agent-reported "an update is available" for a given (server, layer). See
-- docs/09-updates.md § "Reported availability".
CREATE TABLE available_updates (
    server_id         TEXT   NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    layer             TEXT   NOT NULL,
    current_version   TEXT   NOT NULL DEFAULT '',
    candidate_version TEXT   NOT NULL DEFAULT '',
    detail            JSONB  NOT NULL DEFAULT '{}',
    detected_at       BIGINT NOT NULL,
    PRIMARY KEY (server_id, layer)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE available_updates;
DROP TABLE update_policies;

-- +goose StatementEnd
