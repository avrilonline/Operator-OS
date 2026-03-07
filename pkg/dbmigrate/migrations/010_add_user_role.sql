-- Add role column to users table for admin access control.
-- Use a safe approach: create temp, copy, swap — since SQLite doesn't support
-- ALTER TABLE ADD COLUMN IF NOT EXISTS before 3.35.
-- However, for simplicity and since our migrations are tracked and only run once,
-- a simple ALTER is sufficient here.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
