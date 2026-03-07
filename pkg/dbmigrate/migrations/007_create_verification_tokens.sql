CREATE TABLE IF NOT EXISTS verification_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    token       TEXT NOT NULL UNIQUE,
    used        INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_verification_tokens_user ON verification_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_verification_tokens_token ON verification_tokens(token);
