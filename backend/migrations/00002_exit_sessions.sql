-- +goose Up
-- +goose StatementBegin
INSERT INTO permissions (key, description) VALUES
    ('sessions.exit_node', 'Route own traffic through a device acting as exit node');

INSERT INTO role_permissions (role_id, permission_key) VALUES
    ('00000000-0000-7000-8000-00000000a001', 'sessions.exit_node'),  -- super_admin
    ('00000000-0000-7000-8000-00000000a002', 'sessions.exit_node');  -- admin

-- Relayed data sessions between an operator client and an agent.
CREATE TABLE remote_sessions (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations(id),
    device_id       uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id),
    kind            text NOT NULL CHECK (kind IN ('exit_node')),
    status          text NOT NULL CHECK (status IN ('pending', 'active', 'ended')),
    client_ip       text NOT NULL DEFAULT '',
    client_label    text NOT NULL DEFAULT '',
    relay_id        text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    join_deadline   timestamptz NOT NULL,
    max_ends_at     timestamptz NOT NULL,
    started_at      timestamptz,
    ended_at        timestamptz,
    end_reason      text,
    bytes_up        bigint NOT NULL DEFAULT 0,   -- client → agent
    bytes_down      bigint NOT NULL DEFAULT 0    -- agent → client
);
CREATE INDEX remote_sessions_device_idx ON remote_sessions (device_id, id DESC);
CREATE INDEX remote_sessions_open_idx ON remote_sessions (status) WHERE status <> 'ended';
CREATE INDEX remote_sessions_user_idx ON remote_sessions (user_id, id DESC);

-- Single-use join tickets, one per side of a session.
CREATE TABLE relay_tickets (
    ticket_hash  bytea PRIMARY KEY,
    session_id   uuid NOT NULL REFERENCES remote_sessions(id) ON DELETE CASCADE,
    side         text NOT NULL CHECK (side IN ('client', 'agent')),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX relay_tickets_expires_idx ON relay_tickets (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS relay_tickets, remote_sessions;
DELETE FROM role_permissions WHERE permission_key = 'sessions.exit_node';
DELETE FROM permissions WHERE key = 'sessions.exit_node';
-- +goose StatementEnd
