-- +goose Up
-- +goose StatementBegin

-- ─────────────────────────────────────────────────────────────────────────────
-- Users (no external FKs — must come first)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE users (
    id            TEXT    PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE,
    display_name  TEXT,
    password_hash TEXT,                    -- argon2id; NULL for OIDC-only users
    oidc_subject  TEXT    UNIQUE,
    disabled      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    BIGINT  NOT NULL,
    last_login_at BIGINT
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Roles (no external FKs)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT
);

INSERT INTO roles (id, name, description) VALUES
    ('role-admin',    'admin',    'Full access to all resources'),
    ('role-operator', 'operator', 'Deploy, control containers, manage stacks'),
    ('role-viewer',   'viewer',   'Read-only access');

-- ─────────────────────────────────────────────────────────────────────────────
-- Servers (no external FKs)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE servers (
    id             TEXT    PRIMARY KEY,
    name           TEXT    NOT NULL,
    hostname       TEXT    NOT NULL,
    arch           TEXT    NOT NULL,
    os             TEXT    NOT NULL,
    agent_version  TEXT,
    docker_version TEXT,
    labels         JSONB   NOT NULL DEFAULT '{}',
    status         TEXT    NOT NULL DEFAULT 'offline',
    last_seen_at   BIGINT,
    enrolled_at    BIGINT  NOT NULL,
    deleted_at     BIGINT
);

CREATE INDEX idx_servers_status ON servers(status);

-- ─────────────────────────────────────────────────────────────────────────────
-- PKI — single-row tables (no external FKs)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE ca (
    id         INTEGER PRIMARY KEY,
    cert_pem   TEXT    NOT NULL,
    key_enc    BYTEA   NOT NULL,  -- CA private key encrypted with KEK (never plaintext)
    created_at BIGINT  NOT NULL
);

CREATE TABLE oidc_config (
    id                INTEGER PRIMARY KEY,
    issuer_url        TEXT    NOT NULL,
    client_id         TEXT    NOT NULL,
    client_secret_enc TEXT    NOT NULL,  -- encrypted with KEK
    scopes            JSONB   NOT NULL DEFAULT '["openid","profile","email"]',
    claim_mapping     JSONB   NOT NULL DEFAULT '{}',
    enabled           BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at        BIGINT  NOT NULL
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Stacks (references users)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE stacks (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT,
    owner       TEXT    REFERENCES users(id),
    created_at  BIGINT  NOT NULL,
    deleted_at  BIGINT
);

-- ─────────────────────────────────────────────────────────────────────────────
-- PKI — enrollment tokens and certificates
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE enrollment_tokens (
    id          TEXT    PRIMARY KEY,   -- UUID
    token_hash  TEXT    NOT NULL UNIQUE, -- SHA-256 of raw token
    description TEXT,
    ttl_seconds BIGINT  NOT NULL,
    max_uses    BIGINT  NOT NULL DEFAULT 1,
    used_count  BIGINT  NOT NULL DEFAULT 0,
    created_by  TEXT    REFERENCES users(id),
    created_at  BIGINT  NOT NULL,
    expires_at  BIGINT  NOT NULL,
    revoked     BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE certificates (
    serial      TEXT    PRIMARY KEY,   -- hex serial
    agent_id    TEXT    NOT NULL REFERENCES servers(id),
    fingerprint TEXT    NOT NULL UNIQUE,
    cert_pem    TEXT    NOT NULL,
    not_before  BIGINT  NOT NULL,
    not_after   BIGINT  NOT NULL,
    revoked     BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at  BIGINT,
    created_at  BIGINT  NOT NULL
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Sessions (references users)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE sessions (
    id         TEXT    PRIMARY KEY,  -- SHA-256 of raw session token (raw never stored)
    user_id    TEXT    NOT NULL REFERENCES users(id),
    created_at BIGINT  NOT NULL,
    expires_at BIGINT  NOT NULL,
    last_seen  BIGINT  NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    revoked    BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_sessions_expires ON sessions(expires_at);
CREATE INDEX idx_sessions_user    ON sessions(user_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- Agent state (references servers)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE agent_state (
    server_id  TEXT    PRIMARY KEY REFERENCES servers(id),
    state_json JSONB   NOT NULL,
    updated_at BIGINT  NOT NULL
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Role bindings (references users, roles, servers, stacks)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE role_bindings (
    id         TEXT    PRIMARY KEY,
    user_id    TEXT    NOT NULL REFERENCES users(id),
    role_id    TEXT    NOT NULL REFERENCES roles(id),
    server_id  TEXT    REFERENCES servers(id),   -- optional scope
    stack_id   TEXT    REFERENCES stacks(id),    -- optional scope
    created_at BIGINT  NOT NULL,
    UNIQUE (user_id, role_id, server_id, stack_id)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Stack versions (references stacks, users)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE stack_versions (
    id           TEXT    PRIMARY KEY,
    stack_id     TEXT    NOT NULL REFERENCES stacks(id),
    version      BIGINT  NOT NULL,
    compose_yaml TEXT    NOT NULL,
    env_vars     JSONB   NOT NULL DEFAULT '{}',
    secret_refs  JSONB   NOT NULL DEFAULT '[]',
    created_by   TEXT    REFERENCES users(id),
    created_at   BIGINT  NOT NULL,
    UNIQUE (stack_id, version)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Secrets (references users)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE secrets (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT,
    provider    TEXT    NOT NULL,   -- 'builtin' | 'openbao'
    ciphertext  BYTEA,              -- encrypted value (builtin only)
    version     BIGINT  NOT NULL DEFAULT 1,
    bao_mount   TEXT,
    bao_path    TEXT,
    bao_key     TEXT,
    created_by  TEXT    REFERENCES users(id),
    created_at  BIGINT  NOT NULL,
    updated_at  BIGINT  NOT NULL
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Assignments (references servers, stacks, stack_versions, users)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE assignments (
    id               TEXT    PRIMARY KEY,
    server_id        TEXT    NOT NULL REFERENCES servers(id),
    stack_id         TEXT    NOT NULL REFERENCES stacks(id),
    stack_version_id TEXT    NOT NULL REFERENCES stack_versions(id),
    desired_status   TEXT    NOT NULL DEFAULT 'running',
    assigned_by      TEXT    REFERENCES users(id),
    assigned_at      BIGINT  NOT NULL,
    UNIQUE (server_id, stack_id)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Secret bindings (references stack_versions, secrets)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE secret_bindings (
    id               TEXT PRIMARY KEY,
    stack_version_id TEXT NOT NULL REFERENCES stack_versions(id),
    secret_id        TEXT NOT NULL REFERENCES secrets(id),
    service_name     TEXT NOT NULL DEFAULT '',  -- empty = all services
    binding_name     TEXT NOT NULL,
    target           TEXT NOT NULL,  -- 'env' | 'file' | 'docker_secret'
    env_key          TEXT,
    file_path        TEXT,
    UNIQUE (stack_version_id, service_name, binding_name)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Audit & Events
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE audit_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ts          BIGINT  NOT NULL,
    actor_id    TEXT,
    actor_name  TEXT,
    action      TEXT    NOT NULL,
    target_type TEXT    NOT NULL,
    target_id   TEXT,
    before_json JSONB,
    after_json  JSONB,
    ip_address  TEXT,
    error       TEXT
);

CREATE INDEX idx_audit_ts     ON audit_log(ts DESC);
CREATE INDEX idx_audit_actor  ON audit_log(actor_id);
CREATE INDEX idx_audit_target ON audit_log(target_type, target_id);

CREATE TABLE events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ts          BIGINT  NOT NULL,
    server_id   TEXT    REFERENCES servers(id),
    stack_id    TEXT    REFERENCES stacks(id),
    event_type  TEXT    NOT NULL,  -- 'docker' | 'deploy' | 'reconcile' | 'agent'
    severity    TEXT    NOT NULL DEFAULT 'info',
    message     TEXT    NOT NULL,
    detail_json JSONB
);

CREATE INDEX idx_events_ts     ON events(ts DESC);
CREATE INDEX idx_events_server ON events(server_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS secret_bindings;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS assignments;
DROP TABLE IF EXISTS stack_versions;
DROP TABLE IF EXISTS role_bindings;
DROP TABLE IF EXISTS agent_state;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS certificates;
DROP TABLE IF EXISTS enrollment_tokens;
DROP TABLE IF EXISTS stacks;
DROP TABLE IF EXISTS oidc_config;
DROP TABLE IF EXISTS ca;
DROP TABLE IF EXISTS servers;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
