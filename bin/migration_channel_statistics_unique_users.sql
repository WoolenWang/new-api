-- Migration: Add unique_users column to channel_statistics table
-- Phase: 10.4 P2P Group Statistics - GS4-1
-- Date: 2025-12-10
-- Description: 添加 unique_users 字段用于存储区间服务用户数（去重统计）

-- For MySQL
ALTER TABLE `channel_statistics`
ADD COLUMN `unique_users` INT NOT NULL DEFAULT 0 COMMENT '区间服务用户数(去重)' AFTER `downtime_seconds`;

-- For PostgreSQL
-- ALTER TABLE channel_statistics
-- ADD COLUMN unique_users INT NOT NULL DEFAULT 0;
-- COMMENT ON COLUMN channel_statistics.unique_users IS '区间服务用户数(去重)';

-- For SQLite
-- SQLite does not support adding columns with comments directly,
-- and ALTER TABLE has limited functionality.
-- You may need to recreate the table or add the column without comment:
-- ALTER TABLE channel_statistics ADD COLUMN unique_users INTEGER NOT NULL DEFAULT 0;

-- Verify the migration
-- SELECT column_name, data_type, column_default, is_nullable
-- FROM information_schema.columns
-- WHERE table_name = 'channel_statistics' AND column_name = 'unique_users';
