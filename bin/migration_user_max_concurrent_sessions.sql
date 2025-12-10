-- Migration for user max_concurrent_sessions support (Task Set 1)
-- Adds max_concurrent_sessions column to users table to enforce per-user session caps.

-- SQLite / MySQL compatible syntax
ALTER TABLE `users` ADD COLUMN `max_concurrent_sessions` INT DEFAULT 0;

-- PostgreSQL syntax (use IF NOT EXISTS to avoid errors when rerun)
-- ALTER TABLE "users" ADD COLUMN IF NOT EXISTS "max_concurrent_sessions" INT DEFAULT 0;

-- Verification
-- PRAGMA table_info(users); -- SQLite
-- DESCRIBE `users`;        -- MySQL
-- \d+ users;               -- PostgreSQL
