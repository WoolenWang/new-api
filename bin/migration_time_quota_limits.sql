-- Migration for Unified Time-based Quota Limits
-- This migration adds support for hourly/daily/weekly/monthly quota limits for all channel types
-- Created: 2025-12-07
-- Design Doc: docs/01-NewAPI数据面转发渠道粘性和限量问题解决方案.md (Task Set 2)

-- ============================================================
-- 1. Add Time-based Quota Limit Columns to Channels Table
-- ============================================================
-- These columns apply to ALL channel types (platform + P2P)
-- Unit: quota (same as used_quota), 0 means no limit

-- SQLite / MySQL compatible syntax
ALTER TABLE `channels` ADD COLUMN `hourly_quota_limit` BIGINT DEFAULT 0;
ALTER TABLE `channels` ADD COLUMN `daily_quota_limit` BIGINT DEFAULT 0;
ALTER TABLE `channels` ADD COLUMN `weekly_quota_limit` BIGINT DEFAULT 0;
ALTER TABLE `channels` ADD COLUMN `monthly_quota_limit` BIGINT DEFAULT 0;

-- ============================================================
-- 2. Add Comments (MySQL only, SQLite ignores)
-- ============================================================
-- ALTER TABLE `channels` MODIFY COLUMN `hourly_quota_limit` BIGINT DEFAULT 0 COMMENT '每小时额度限制(quota单位)，0表示不限制';
-- ALTER TABLE `channels` MODIFY COLUMN `daily_quota_limit` BIGINT DEFAULT 0 COMMENT '每日额度限制(quota单位)，0表示不限制';
-- ALTER TABLE `channels` MODIFY COLUMN `weekly_quota_limit` BIGINT DEFAULT 0 COMMENT '每周额度限制(quota单位)，0表示不限制';
-- ALTER TABLE `channels` MODIFY COLUMN `monthly_quota_limit` BIGINT DEFAULT 0 COMMENT '每月额度限制(quota单位)，0表示不限制';

-- ============================================================
-- 3. Add Comments to Legacy Fields (MySQL only)
-- ============================================================
-- Mark the old request-count-based fields as deprecated
-- ALTER TABLE `channels` MODIFY COLUMN `hourly_limit` INT DEFAULT 0 COMMENT '每小时请求数限制(已废弃，使用hourly_quota_limit代替)';
-- ALTER TABLE `channels` MODIFY COLUMN `daily_limit` INT DEFAULT 0 COMMENT '每日请求数限制(已废弃，使用daily_quota_limit代替)';

-- ============================================================
-- 4. PostgreSQL Specific Syntax (Use if deploying on PostgreSQL)
-- ============================================================
-- ALTER TABLE "channels" ADD COLUMN IF NOT EXISTS "hourly_quota_limit" BIGINT DEFAULT 0;
-- ALTER TABLE "channels" ADD COLUMN IF NOT EXISTS "daily_quota_limit" BIGINT DEFAULT 0;
-- ALTER TABLE "channels" ADD COLUMN IF NOT EXISTS "weekly_quota_limit" BIGINT DEFAULT 0;
-- ALTER TABLE "channels" ADD COLUMN IF NOT EXISTS "monthly_quota_limit" BIGINT DEFAULT 0;

-- COMMENT ON COLUMN "channels"."hourly_quota_limit" IS '每小时额度限制(quota单位)，0表示不限制';
-- COMMENT ON COLUMN "channels"."daily_quota_limit" IS '每日额度限制(quota单位)，0表示不限制';
-- COMMENT ON COLUMN "channels"."weekly_quota_limit" IS '每周额度限制(quota单位)，0表示不限制';
-- COMMENT ON COLUMN "channels"."monthly_quota_limit" IS '每月额度限制(quota单位)，0表示不限制';

-- ============================================================
-- 5. Data Migration (Optional)
-- ============================================================
-- If you want to preserve the old hourly_limit/daily_limit as quota-based limits,
-- you need a conversion ratio. This is left commented as it depends on your business logic.
--
-- Example: Assume 1000 requests ≈ 1,000,000 quota (adjust based on your average token usage)
-- UPDATE `channels` SET `hourly_quota_limit` = `hourly_limit` * 1000 WHERE `hourly_limit` > 0;
-- UPDATE `channels` SET `daily_quota_limit` = `daily_limit` * 1000 WHERE `daily_limit` > 0;

-- ============================================================
-- 6. Verification Queries
-- ============================================================
-- Verify columns were added
-- SELECT * FROM `channels` LIMIT 1;

-- Check channels with new quota limits
-- SELECT id, name, owner_user_id, hourly_quota_limit, daily_quota_limit, weekly_quota_limit, monthly_quota_limit FROM `channels` WHERE hourly_quota_limit > 0 OR daily_quota_limit > 0;
