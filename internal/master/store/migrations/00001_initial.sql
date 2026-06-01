-- +goose Up
-- +goose StatementBegin
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- ─────────────────────────────────────────────────────────────────────────────
-- PKI
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE ca (
    id         INTEGER PRIMARY KEY,
    cert_pem   TEXT    NOT NULL,
    key_enc    BLOB    NOT NULL,  -- CA private key encrypted with KEK (never plaintext)
    created_at INTEGER NOT NULL  -- Unix ms
);

CREATE TABLE enrollment_tokens (
    id          TEXT    PRIMARY KEY,   -- UUID
    token_hash  TEXT    NOT NULL UNIQUE, -- SHA-256 of raw token
    description TEXT,
    ttl_seconds INTEGER NOT NULL,
    max_uses    INTEGER NOT NULL DEFAULT 1,
    used_count  INTEGER NOT NULL DEFAULT 0,
    created_by  TEXT    REFERENCES users(id),
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    revoked     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE certificates (
    serial      TEXT    PRIMARY KEY,   -- hex serial
    agent_id    TEXT    NOT NULL REFERENCES servers(id),
    fingerprint TEXT    NOT NULL UNIQUE,
    cert_pem    TEXT    NOT NULL,
    not_before  INTEGER NOT NULL,
    not_after   INTEGER NOT NULL,
    revoked     INTEGER NOT NULL DEFAULT 0,
    revoked_at  INTEGER,
    created_at  INTEGER NOT NULL
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Users (SQLite resolves FK references at runtime, not at DDL time)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE users (
    id            TEXT    PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE,
    display_name  TEXT,
    password_hash TEXT,                   -- argon2id; NULL for OIDC-only users
    oidc_subject  TEXT    UNIQUE,
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER
);

CREATE TABLE oidc_config (
    id                INTEGER PRIMARY KEY,
    issuer_url        TEXT    NOT NULL,
    client_id         TEXT    NOT NULL,
    client_secret_enc TEXT    NOT NULL,  -- encrypted with KEK
    scopes            TEXT    NOT NULL DEFAULT '["openid","profile","email"]',
    claim_mapping     TEXT    NOT NULL DEFAULT '{}',
    enabled           INTEGER NOT NULL DEFAULT 0,
    updated_at        INTEGER NOT NULL
);

CREATE TABLE roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT
);

INSERT INTO roles (id, name, description) VALUES
    ('role-admin',    'admin',    'Full access to all resources'),
    ('role-operator', 'operator', 'Deploy, control containers, manage stacks'),
    ('role-viewer',   'viewer',   'Read-only access');

CREATE TABLE role_bindings (
    id         TEXT    PRIMARY KEY,
    user_id    TEXT    NOT NULL REFERENCES users(id),
    role_id    TEXT    NOT NULL REFERENCES roles(id),
    server_id  TEXT,   -- optional scope
    stack_id   TEXT,   -- optional scope
    created_at INTEGER NOT NULL,
    UNIQUE (user_id, role_id, server_id, stack_id)
);

CREATE TABLE sessions (
    id         TEXT    PRIMARY KEY,  -- SHA-256 of raw session token (raw never stored)
    user_id    TEXT    NOT NULL REFERENCES users(id),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    revoked    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_sessions_expires ON sessions(expires_at);
CREATE INDEX idx_sessions_user    ON sessions(user_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- Servers
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE servers (
    id             TEXT    PRIMARY KEY,
    name           TEXT    NOT NULL,
    hostname       TEXT    NOT NULL,
    arch           TEXT    NOT NULL,
    os             TEXT    NOT NULL,
    agent_version  TEXT,
    docker_version TEXT,
    labels         TEXT    NOT NULL DEFAULT '{}',  -- JSON object
    status         TEXT    NOT NULL DEFAULT 'offline',
    last_seen_at   INTEGER,
    enrolled_at    INTEGER NOT NULL,
    deleted_at     INTEGER
);

CREATE INDEX idx_servers_status ON servers(status);

CREATE TABLE agent_state (
    server_id  TEXT    PRIMARY KEY REFERENCES servers(id),
    state_json TEXT    NOT NULL,
    updated_at INTEGER NOT NULL
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Stacks
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE stacks (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT,
    owner       TEXT    REFERENCES users(id),
    created_at  INTEGER NOT NULL,
    deleted_at  INTEGER
);

CREATE TABLE stack_versions (
    id           TEXT    PRIMARY KEY,
    stack_id     TEXT    NOT NULL REFERENCES stacks(id),
    version      INTEGER NOT NULL,
    compose_yaml TEXT    NOT NULL,
    env_vars     TEXT    NOT NULL DEFAULT '{}',
    secret_refs  TEXT    NOT NULL DEFAULT '[]',
    created_by   TEXT    REFERENCES users(id),
    created_at   INTEGER NOT NULL,
    UNIQUE (stack_id, version)
);

CREATE TABLE assignments (
    id               TEXT    PRIMARY KEY,
    server_id        TEXT    NOT NULL REFERENCES servers(id),
    stack_id         TEXT    NOT NULL REFERENCES stacks(id),
    stack_version_id TEXT    NOT NULL REFERENCES stack_versions(id),
    desired_status   TEXT    NOT NULL DEFAULT 'running',
    assigned_by      TEXT    REFERENCES users(id),
    assigned_at      INTEGER NOT NULL,
    UNIQUE (server_id, stack_id)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Secrets
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE secrets (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT,
    provider    TEXT    NOT NULL,   -- 'builtin' | 'openbao'
    ciphertext  BLOB,               -- encrypted value (builtin only)
    version     INTEGER NOT NULL DEFAULT 1,
    bao_mount   TEXT,
    bao_path    TEXT,
    bao_key     TEXT,
    created_by  TEXT    REFERENCES users(id),
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

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
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    actor_id    TEXT,
    actor_name  TEXT,
    action      TEXT    NOT NULL,
    target_type TEXT    NOT NULL,
    target_id   TEXT,
    before_json TEXT,
    after_json  TEXT,
    ip_address  TEXT,
    error       TEXT
);

CREATE INDEX idx_audit_ts     ON audit_log(ts DESC);
CREATE INDEX idx_audit_actor  ON audit_log(actor_id);
CREATE INDEX idx_audit_target ON audit_log(target_type, target_id);

CREATE TABLE events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    server_id   TEXT    REFERENCES servers(id),
    stack_id    TEXT    REFERENCES stacks(id),
    event_type  TEXT    NOT NULL,  -- 'docker' | 'deploy' | 'reconcile' | 'agent'
    severity    TEXT    NOT NULL DEFAULT 'info',
    message     TEXT    NOT NULL,
    detail_json TEXT
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
DROP TABLE IF EXISTS stacks;
DROP TABLE IF EXISTS agent_state;
DROP TABLE IF EXISTS servers;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS role_bindings;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS oidc_config;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS certificates;
DROP TABLE IF EXISTS enrollment_tokens;
DROP TABLE IF EXISTS ca;
-- +goose StatementEnd
