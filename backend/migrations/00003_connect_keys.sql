-- +goose Up
-- +goose StatementBegin
-- Connect keys: shareable, revocable access to one exit-node device without a
-- user login (QR code / link). Many phones may use the same key at once.
CREATE TABLE connect_keys (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id),
    device_id     uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    key_hash      bytea NOT NULL UNIQUE,
    label         text NOT NULL DEFAULT '',
    created_by    uuid NOT NULL REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz,
    revoked_at    timestamptz,
    uses          bigint NOT NULL DEFAULT 0,
    last_used_at  timestamptz
);
CREATE INDEX connect_keys_device_idx ON connect_keys (device_id, id DESC);

ALTER TABLE remote_sessions ADD COLUMN connect_key_id uuid REFERENCES connect_keys(id) ON DELETE SET NULL;
CREATE INDEX remote_sessions_key_open_idx ON remote_sessions (connect_key_id) WHERE status <> 'ended';

-- Users can be removed from the UI (soft delete keeps audit/command history intact).
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled', 'deleted'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE users SET status = 'disabled' WHERE status = 'deleted';
ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'));
ALTER TABLE remote_sessions DROP COLUMN IF EXISTS connect_key_id;
DROP TABLE IF EXISTS connect_keys;
-- +goose StatementEnd
