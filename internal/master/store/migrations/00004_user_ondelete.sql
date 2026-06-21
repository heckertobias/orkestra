-- +goose Up
-- +goose StatementBegin

-- sessions.user_id: cascade deletion so deleting a user removes their sessions
ALTER TABLE sessions DROP CONSTRAINT sessions_user_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- role_bindings.user_id: cascade so deleting a user removes their role bindings
ALTER TABLE role_bindings DROP CONSTRAINT role_bindings_user_id_fkey;
ALTER TABLE role_bindings ADD CONSTRAINT role_bindings_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- stacks.owner: set null so stacks survive user deletion
ALTER TABLE stacks DROP CONSTRAINT stacks_owner_fkey;
ALTER TABLE stacks ADD CONSTRAINT stacks_owner_fkey
    FOREIGN KEY (owner) REFERENCES users(id) ON DELETE SET NULL;

-- enrollment_tokens.created_by: set null
ALTER TABLE enrollment_tokens DROP CONSTRAINT enrollment_tokens_created_by_fkey;
ALTER TABLE enrollment_tokens ADD CONSTRAINT enrollment_tokens_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

-- stack_versions.created_by: set null
ALTER TABLE stack_versions DROP CONSTRAINT stack_versions_created_by_fkey;
ALTER TABLE stack_versions ADD CONSTRAINT stack_versions_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

-- secrets.created_by: set null
ALTER TABLE secrets DROP CONSTRAINT secrets_created_by_fkey;
ALTER TABLE secrets ADD CONSTRAINT secrets_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

-- assignments.assigned_by: set null
ALTER TABLE assignments DROP CONSTRAINT assignments_assigned_by_fkey;
ALTER TABLE assignments ADD CONSTRAINT assignments_assigned_by_fkey
    FOREIGN KEY (assigned_by) REFERENCES users(id) ON DELETE SET NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE sessions DROP CONSTRAINT sessions_user_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE role_bindings DROP CONSTRAINT role_bindings_user_id_fkey;
ALTER TABLE role_bindings ADD CONSTRAINT role_bindings_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE stacks DROP CONSTRAINT stacks_owner_fkey;
ALTER TABLE stacks ADD CONSTRAINT stacks_owner_fkey
    FOREIGN KEY (owner) REFERENCES users(id);

ALTER TABLE enrollment_tokens DROP CONSTRAINT enrollment_tokens_created_by_fkey;
ALTER TABLE enrollment_tokens ADD CONSTRAINT enrollment_tokens_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id);

ALTER TABLE stack_versions DROP CONSTRAINT stack_versions_created_by_fkey;
ALTER TABLE stack_versions ADD CONSTRAINT stack_versions_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id);

ALTER TABLE secrets DROP CONSTRAINT secrets_created_by_fkey;
ALTER TABLE secrets ADD CONSTRAINT secrets_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id);

ALTER TABLE assignments DROP CONSTRAINT assignments_assigned_by_fkey;
ALTER TABLE assignments ADD CONSTRAINT assignments_assigned_by_fkey
    FOREIGN KEY (assigned_by) REFERENCES users(id);

-- +goose StatementEnd
