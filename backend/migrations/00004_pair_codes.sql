-- +goose Up
-- +goose StatementBegin
-- Pairing codes: a short-lived 6-digit code typed into the phone app instead
-- of scanning a QR code. Redeeming one issues the phone its own connect key
-- (so each paired phone can be revoked on its own).
CREATE TABLE pair_codes (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id),
    device_id     uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    code_hash     bytea NOT NULL,
    label         text NOT NULL DEFAULT '',
    created_by    uuid NOT NULL REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    uses          bigint NOT NULL DEFAULT 0
);
CREATE INDEX pair_codes_hash_idx ON pair_codes (code_hash) WHERE revoked_at IS NULL;
CREATE INDEX pair_codes_device_idx ON pair_codes (device_id, id DESC);

ALTER TABLE connect_keys ADD COLUMN pair_code_id uuid REFERENCES pair_codes(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE connect_keys DROP COLUMN IF EXISTS pair_code_id;
DROP TABLE IF EXISTS pair_codes;
-- +goose StatementEnd
