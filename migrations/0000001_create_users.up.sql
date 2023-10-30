CREATE TABLE IF NOT EXISTS users
(
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username       VARCHAR(100) UNIQUE NOT NULL,
    fullname       VARCHAR(1000) NOT NULL,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_address ON users(wallet_address);
CREATE INDEX idx_users_username ON users(username);