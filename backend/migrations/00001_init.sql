-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS citext;

-- ------------------------------------------------------------- tenancy
CREATE TABLE organizations (
    id          uuid PRIMARY KEY,
    name        text NOT NULL,
    slug        text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO organizations (id, name, slug)
VALUES ('00000000-0000-7000-8000-000000000001', 'Default', 'default');

-- ------------------------------------------------------------- RBAC
CREATE TABLE permissions (
    key          text PRIMARY KEY,
    description  text NOT NULL
);

CREATE TABLE roles (
    id           uuid PRIMARY KEY,
    org_id       uuid REFERENCES organizations(id) ON DELETE CASCADE, -- NULL = built-in
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    is_builtin   boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX roles_name_uniq
    ON roles (coalesce(org_id, '00000000-0000-0000-0000-000000000000'::uuid), name);

CREATE TABLE role_permissions (
    role_id         uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_key  text NOT NULL REFERENCES permissions(key) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_key)
);

INSERT INTO permissions (key, description) VALUES
    ('devices.read',            'View devices, inventory and status'),
    ('devices.manage',          'Approve, rename, disable, enable and revoke devices'),
    ('commands.read',           'View commands and their results'),
    ('commands.execute.action', 'Run predefined safe actions on devices'),
    ('commands.execute.exec',   'Run arbitrary commands on devices'),
    ('enrollment.manage',       'Create and revoke enrollment tokens'),
    ('users.manage',            'Create and modify users'),
    ('audit.read',              'Read the audit log'),
    ('platform.admin',          'Full platform administration');

INSERT INTO roles (id, name, description, is_builtin) VALUES
    ('00000000-0000-7000-8000-00000000a001', 'super_admin', 'Full platform administration', true),
    ('00000000-0000-7000-8000-00000000a002', 'admin',       'Manage devices, users and run any command', true),
    ('00000000-0000-7000-8000-00000000a003', 'operator',    'View everything and run approved actions', true),
    ('00000000-0000-7000-8000-00000000a004', 'viewer',      'Read-only access to devices, metrics and logs', true);

INSERT INTO role_permissions (role_id, permission_key)
SELECT '00000000-0000-7000-8000-00000000a001'::uuid, key FROM permissions;

INSERT INTO role_permissions (role_id, permission_key)
SELECT '00000000-0000-7000-8000-00000000a002'::uuid, key FROM permissions
WHERE key IN ('devices.read', 'devices.manage', 'commands.read', 'commands.execute.action',
              'commands.execute.exec', 'enrollment.manage', 'users.manage', 'audit.read');

INSERT INTO role_permissions (role_id, permission_key)
SELECT '00000000-0000-7000-8000-00000000a003'::uuid, key FROM permissions
WHERE key IN ('devices.read', 'commands.read', 'commands.execute.action');

INSERT INTO role_permissions (role_id, permission_key)
SELECT '00000000-0000-7000-8000-00000000a004'::uuid, key FROM permissions
WHERE key IN ('devices.read', 'commands.read');

-- ------------------------------------------------------------- users
CREATE TABLE users (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL REFERENCES organizations(id),
    email          citext NOT NULL UNIQUE,
    password_hash  text NOT NULL,
    display_name   text NOT NULL DEFAULT '',
    status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    last_login_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX users_org_idx ON users (org_id, id);

CREATE TABLE user_roles (
    user_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id  uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    PRIMARY KEY (user_id, role_id)
);

-- One row per refresh token. Rotation marks the old row rotated and inserts a
-- new row in the same family; presenting a rotated token revokes the family.
CREATE TABLE user_sessions (
    id                   uuid PRIMARY KEY,
    family_id            uuid NOT NULL,
    user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash   bytea NOT NULL UNIQUE,
    client               text NOT NULL CHECK (client IN ('web', 'mobile')),
    user_agent           text NOT NULL DEFAULT '',
    ip                   text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    expires_at           timestamptz NOT NULL,
    absolute_expires_at  timestamptz NOT NULL,
    rotated_at           timestamptz,
    revoked_at           timestamptz
);
CREATE INDEX user_sessions_family_idx ON user_sessions (family_id);
CREATE INDEX user_sessions_expires_idx ON user_sessions (absolute_expires_at);

-- ------------------------------------------------------------- enrollment & devices
CREATE TABLE enrollment_tokens (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id),
    token_hash    bytea NOT NULL UNIQUE,
    description   text NOT NULL DEFAULT '',
    auto_approve  boolean NOT NULL DEFAULT false,
    max_uses      integer CHECK (max_uses IS NULL OR max_uses > 0),
    uses          integer NOT NULL DEFAULT 0,
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    created_by    uuid NOT NULL REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX enrollment_tokens_org_idx ON enrollment_tokens (org_id, id DESC);

CREATE TABLE devices (
    id                    uuid PRIMARY KEY,
    org_id                uuid NOT NULL REFERENCES organizations(id),
    name                  text NOT NULL,
    hostname              text NOT NULL DEFAULT '',
    status                text NOT NULL CHECK (status IN ('pending', 'active', 'disabled', 'revoked')),
    connectivity          text NOT NULL DEFAULT 'offline' CHECK (connectivity IN ('online', 'offline')),
    platform              text NOT NULL DEFAULT '',
    arch                  text NOT NULL DEFAULT '',
    os_name               text NOT NULL DEFAULT '',
    os_version            text NOT NULL DEFAULT '',
    os_build              text NOT NULL DEFAULT '',
    kernel_version        text NOT NULL DEFAULT '',
    agent_version         text NOT NULL DEFAULT '',
    machine_id_hash       text NOT NULL DEFAULT '',
    capabilities          text[] NOT NULL DEFAULT '{}',
    inventory             jsonb,
    enrolled_via          uuid REFERENCES enrollment_tokens(id),
    replaces_device_id    uuid REFERENCES devices(id),
    approved_by           uuid REFERENCES users(id),
    approved_at           timestamptz,
    last_seen_at          timestamptz,
    last_ip               text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX devices_org_name_idx ON devices (org_id, name, id);
CREATE INDEX devices_org_status_idx ON devices (org_id, status);
CREATE INDEX devices_online_idx ON devices (last_seen_at) WHERE connectivity = 'online';
CREATE INDEX devices_machine_idx ON devices (org_id, machine_id_hash) WHERE machine_id_hash <> '';

CREATE TABLE device_credentials (
    id           uuid PRIMARY KEY,
    device_id    uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    public_key   bytea NOT NULL,            -- PKIX (SPKI) DER
    algorithm    text NOT NULL,             -- 'ecdsa-p256'
    fingerprint  text NOT NULL UNIQUE,      -- sha256 hex of public_key
    created_at   timestamptz NOT NULL DEFAULT now(),
    revoked_at   timestamptz
);
CREATE INDEX device_credentials_device_idx ON device_credentials (device_id) WHERE revoked_at IS NULL;

-- Single-use nonces for device proof-of-possession.
CREATE TABLE auth_challenges (
    nonce_hash  bytea PRIMARY KEY,
    device_id   uuid NOT NULL,
    expires_at  timestamptz NOT NULL
);
CREATE INDEX auth_challenges_expires_idx ON auth_challenges (expires_at);

-- ------------------------------------------------------------- presence
CREATE TABLE device_sessions (
    id                 uuid PRIMARY KEY,
    device_id          uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    gateway_id         text NOT NULL,
    remote_ip          text NOT NULL DEFAULT '',
    agent_version      text NOT NULL DEFAULT '',
    protocol_version   integer NOT NULL,
    connected_at       timestamptz NOT NULL DEFAULT now(),
    last_seen_at       timestamptz NOT NULL DEFAULT now(),
    disconnected_at    timestamptz,
    disconnect_reason  text
);
CREATE UNIQUE INDEX device_sessions_live_uniq ON device_sessions (device_id) WHERE disconnected_at IS NULL;
CREATE INDEX device_sessions_live_seen_idx ON device_sessions (last_seen_at) WHERE disconnected_at IS NULL;

-- Sampled heartbeat history; daily partitions are managed by the worker.
CREATE TABLE device_heartbeats (
    device_id        uuid NOT NULL,
    ts               timestamptz NOT NULL,
    cpu_percent      real,
    mem_used_bytes   bigint,
    mem_total_bytes  bigint,
    load1            real,
    uptime_s         bigint
) PARTITION BY RANGE (ts);
CREATE INDEX device_heartbeats_device_ts_idx ON device_heartbeats (device_id, ts DESC);
CREATE TABLE device_heartbeats_default PARTITION OF device_heartbeats DEFAULT;

-- ------------------------------------------------------------- commands
CREATE TABLE device_commands (
    id                uuid PRIMARY KEY,
    org_id            uuid NOT NULL REFERENCES organizations(id),
    device_id         uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    requested_by      uuid NOT NULL REFERENCES users(id),
    user_session_id   uuid,
    idempotency_key   text,
    request_id        text NOT NULL DEFAULT '',
    client_ip         text NOT NULL DEFAULT '',
    kind              text NOT NULL CHECK (kind IN ('exec', 'action')),
    action            text,
    argv              text[],
    shell             boolean NOT NULL DEFAULT false,
    timeout_s         integer NOT NULL CHECK (timeout_s BETWEEN 1 AND 3600),
    status            text NOT NULL CHECK (status IN ('queued', 'sent', 'acked', 'running', 'succeeded',
                                                      'failed', 'timed_out', 'canceled', 'expired', 'rejected')),
    spec              bytea NOT NULL,   -- serialized agentv1.CommandSpec (exactly what was signed)
    signature         bytea NOT NULL,
    signing_key_id    text NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    expires_at        timestamptz NOT NULL,
    sent_at           timestamptz,
    started_at        timestamptz,
    finished_at       timestamptz,
    UNIQUE (requested_by, idempotency_key)
);
CREATE INDEX device_commands_device_idx ON device_commands (device_id, id DESC);
CREATE INDEX device_commands_open_idx ON device_commands (device_id)
    WHERE status IN ('queued', 'sent', 'acked', 'running');

CREATE TABLE command_results (
    command_id   uuid PRIMARY KEY REFERENCES device_commands(id) ON DELETE CASCADE,
    exit_code    integer,
    stdout       text NOT NULL DEFAULT '',
    stderr       text NOT NULL DEFAULT '',
    truncated    boolean NOT NULL DEFAULT false,
    error        text,
    duration_ms  bigint NOT NULL DEFAULT 0
);

-- ------------------------------------------------------------- audit (append-only, hash-chained per org)
CREATE TABLE audit_logs (
    id           bigserial PRIMARY KEY,
    org_id       uuid NOT NULL,
    ts           timestamptz NOT NULL,
    request_id   text,
    actor_type   text NOT NULL CHECK (actor_type IN ('user', 'device', 'system')),
    actor_id     uuid,
    actor_label  text,
    actor_ip     text,
    session_id   uuid,
    action       text NOT NULL,
    target_type  text,
    target_id    uuid,
    outcome      text NOT NULL CHECK (outcome IN ('success', 'denied', 'error')),
    details      jsonb NOT NULL DEFAULT '{}'::jsonb,
    prev_hash    bytea NOT NULL,
    hash         bytea NOT NULL
);
CREATE INDEX audit_logs_org_idx ON audit_logs (org_id, id DESC);
CREATE INDEX audit_logs_target_idx ON audit_logs (target_id, id DESC) WHERE target_id IS NOT NULL;
CREATE INDEX audit_logs_actor_idx ON audit_logs (actor_id, id DESC) WHERE actor_id IS NOT NULL;
CREATE INDEX audit_logs_action_idx ON audit_logs (org_id, action text_pattern_ops, id DESC);

CREATE FUNCTION audit_logs_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only';
END;
$$;
CREATE TRIGGER audit_logs_no_update BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();
CREATE TRIGGER audit_logs_no_truncate BEFORE TRUNCATE ON audit_logs
    FOR EACH STATEMENT EXECUTE FUNCTION audit_logs_immutable();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_logs, command_results, device_commands, device_heartbeats, device_sessions,
    auth_challenges, device_credentials, devices, enrollment_tokens, user_sessions, user_roles, users,
    role_permissions, roles, permissions, organizations CASCADE;
DROP FUNCTION IF EXISTS audit_logs_immutable();
-- +goose StatementEnd
