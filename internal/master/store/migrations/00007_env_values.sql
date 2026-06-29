-- +goose Up
-- +goose StatementBegin

-- Env-var VALUES move from the stack version to the per-server assignment:
-- a stack version now declares only the required env-var NAMES, and the
-- concrete values are supplied at deploy time, per assignment.

ALTER TABLE assignments ADD COLUMN env_values JSONB NOT NULL DEFAULT '{}';

ALTER TABLE stack_versions RENAME COLUMN env_vars TO env_var_names;
ALTER TABLE stack_versions ALTER COLUMN env_var_names SET DEFAULT '[]';

-- Convert any existing {name: value} objects into ["name", ...] arrays,
-- preserving the declared names.
UPDATE stack_versions
SET env_var_names = COALESCE(
    (SELECT jsonb_agg(k) FROM jsonb_object_keys(env_var_names) AS k),
    '[]'::jsonb
)
WHERE jsonb_typeof(env_var_names) = 'object';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE stack_versions RENAME COLUMN env_var_names TO env_vars;
ALTER TABLE stack_versions ALTER COLUMN env_vars SET DEFAULT '{}';
-- Best-effort: arrays cannot be turned back into {name: value} objects with values.
UPDATE stack_versions SET env_vars = '{}'::jsonb WHERE jsonb_typeof(env_vars) = 'array';

ALTER TABLE assignments DROP COLUMN env_values;

-- +goose StatementEnd
