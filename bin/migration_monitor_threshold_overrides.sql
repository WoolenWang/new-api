-- Migration: Add threshold_overrides field to monitor_policies table
-- Purpose: Support configurable evaluation thresholds per policy
-- Related: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md (Section 9.x)
-- Date: 2025-12-10

-- ==================================================
-- MySQL / MariaDB Migration
-- ==================================================

-- Add threshold_overrides column to monitor_policies table
ALTER TABLE `monitor_policies`
ADD COLUMN `threshold_overrides` TEXT COMMENT '阈值覆盖配置(JSON Object: {"strict":95.0,"standard":85.0,"lenient":70.0}); 为空则使用全局默认值';

-- ==================================================
-- PostgreSQL Migration
-- ==================================================

-- For PostgreSQL, use TEXT type for JSON storage
-- ALTER TABLE monitor_policies
-- ADD COLUMN threshold_overrides TEXT;
--
-- COMMENT ON COLUMN monitor_policies.threshold_overrides IS '阈值覆盖配置(JSON Object: {"strict":95.0,"standard":85.0,"lenient":70.0}); 为空则使用全局默认值';

-- ==================================================
-- SQLite Migration
-- ==================================================

-- SQLite doesn't support ALTER TABLE ADD COLUMN with COMMENT directly
-- But it will work without the COMMENT part:
-- ALTER TABLE monitor_policies
-- ADD COLUMN threshold_overrides TEXT;

-- ==================================================
-- Verification Query
-- ==================================================

-- Verify the column was added successfully:
-- SELECT * FROM monitor_policies LIMIT 1;

-- ==================================================
-- Rollback (if needed)
-- ==================================================

-- To rollback this migration:
-- ALTER TABLE monitor_policies DROP COLUMN threshold_overrides;

-- ==================================================
-- Example Usage
-- ==================================================

-- Example 1: Set custom thresholds for a specific policy (strict policy)
-- UPDATE monitor_policies
-- SET threshold_overrides = '{"strict":98.0,"standard":90.0,"lenient":80.0}'
-- WHERE id = 1;

-- Example 2: Remove custom thresholds (use global defaults)
-- UPDATE monitor_policies
-- SET threshold_overrides = NULL
-- WHERE id = 2;

-- Example 3: Set only specific thresholds (others use global defaults)
-- UPDATE monitor_policies
-- SET threshold_overrides = '{"strict":97.0}'
-- WHERE name = 'Critical Models Monitoring';
