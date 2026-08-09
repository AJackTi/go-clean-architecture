-- Store only fixed-size digests. Raw client keys, scopes, and request bodies
-- must never become durable database data.
CREATE TABLE item_idempotency_keys (
    key_hash BYTEA PRIMARY KEY CHECK (octet_length(key_hash) = 32),
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at)
);

CREATE INDEX idx_item_idempotency_keys_expires_at
    ON item_idempotency_keys (expires_at);
