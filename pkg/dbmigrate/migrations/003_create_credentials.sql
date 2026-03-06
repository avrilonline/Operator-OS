-- Create encrypted credentials table.
-- Matches the schema from pkg/auth/sqlite_credential_store.go.

CREATE TABLE IF NOT EXISTS credentials (
    provider        TEXT PRIMARY KEY,
    encrypted_data  BLOB NOT NULL,
    encrypted       INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
