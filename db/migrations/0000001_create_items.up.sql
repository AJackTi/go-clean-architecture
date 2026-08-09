CREATE TABLE items (
    id UUID PRIMARY KEY,
    name VARCHAR(120) NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_items_created_at_id
    ON items (created_at DESC, id DESC);
