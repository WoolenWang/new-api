-- Migration for Channel Statistics Feature (Phase 8.1)
-- This migration adds statistical fields to channels table and creates channel_statistics time-series table
-- Design Document: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
-- Phase: 8.1 阶段一：数据库与配置扩展

-- =====================================================
-- Part 1: Extend channels table with statistics fields
-- =====================================================

-- For MySQL
-- ALTER TABLE `channels`
-- ADD COLUMN IF NOT EXISTS `avg_response_time` INT DEFAULT 0 COMMENT '平均首字响应时间 (ms)',
-- ADD COLUMN IF NOT EXISTS `fail_rate` DOUBLE PRECISION DEFAULT 0.0 COMMENT '请求失败率 (%)',
-- ADD COLUMN IF NOT EXISTS `avg_cache_hit_rate` DOUBLE PRECISION DEFAULT 0.0 COMMENT '平均缓存命中率 (%)',
-- ADD COLUMN IF NOT EXISTS `stream_req_ratio` DOUBLE PRECISION DEFAULT 0.0 COMMENT '流式请求占比 (%)',
-- ADD COLUMN IF NOT EXISTS `tpm` INT DEFAULT 0 COMMENT '每分钟处理的Tokens数量',
-- ADD COLUMN IF NOT EXISTS `rpm` INT DEFAULT 0 COMMENT '每分钟请求数',
-- ADD COLUMN IF NOT EXISTS `quota_pm` BIGINT DEFAULT 0 COMMENT '每分钟消耗的额度',
-- ADD COLUMN IF NOT EXISTS `total_sessions` BIGINT DEFAULT 0 COMMENT '区间总服务session数',
-- ADD COLUMN IF NOT EXISTS `downtime_percentage` DOUBLE PRECISION DEFAULT 0.0 COMMENT '区间停止服务时间占比 (%)',
-- ADD COLUMN IF NOT EXISTS `unique_users` INT DEFAULT 0 COMMENT '区间服务用户数 (去重)',
-- ADD COLUMN IF NOT EXISTS `monitoring_config` TEXT COMMENT '模型智能监控策略 (JSON)';

-- For PostgreSQL
-- ALTER TABLE "channels"
-- ADD COLUMN IF NOT EXISTS "avg_response_time" INTEGER DEFAULT 0,
-- ADD COLUMN IF NOT EXISTS "fail_rate" DOUBLE PRECISION DEFAULT 0.0,
-- ADD COLUMN IF NOT EXISTS "avg_cache_hit_rate" DOUBLE PRECISION DEFAULT 0.0,
-- ADD COLUMN IF NOT EXISTS "stream_req_ratio" DOUBLE PRECISION DEFAULT 0.0,
-- ADD COLUMN IF NOT EXISTS "tpm" INTEGER DEFAULT 0,
-- ADD COLUMN IF NOT EXISTS "rpm" INTEGER DEFAULT 0,
-- ADD COLUMN IF NOT EXISTS "quota_pm" BIGINT DEFAULT 0,
-- ADD COLUMN IF NOT EXISTS "total_sessions" BIGINT DEFAULT 0,
-- ADD COLUMN IF NOT EXISTS "downtime_percentage" DOUBLE PRECISION DEFAULT 0.0,
-- ADD COLUMN IF NOT EXISTS "unique_users" INTEGER DEFAULT 0,
-- ADD COLUMN IF NOT EXISTS "monitoring_config" TEXT;

-- Note: SQLite will handle these additions through GORM AutoMigrate

-- =====================================================
-- Part 2: Create channel_statistics time-series table
-- =====================================================

-- For MySQL
-- CREATE TABLE IF NOT EXISTS `channel_statistics` (
--     `id` INT AUTO_INCREMENT PRIMARY KEY,
--     `channel_id` INT NOT NULL COMMENT '渠道ID',
--     `model_name` VARCHAR(255) NOT NULL COMMENT '模型名称',
--     `time_window_start` BIGINT NOT NULL COMMENT '统计窗口起始时间戳',
--     `request_count` INT DEFAULT 0 COMMENT '总请求数',
--     `fail_count` INT DEFAULT 0 COMMENT '失败请求数',
--     `total_tokens` BIGINT DEFAULT 0 COMMENT '总Token数',
--     `total_quota` BIGINT DEFAULT 0 COMMENT '总额度消耗',
--     `total_latency_ms` BIGINT DEFAULT 0 COMMENT '总首字延迟(ms)',
--     `stream_req_count` INT DEFAULT 0 COMMENT '流式请求数',
--     `cache_hit_count` INT DEFAULT 0 COMMENT '缓存命中数',
--     `downtime_seconds` INT DEFAULT 0 COMMENT '禁用时长(秒)',
--     `created_at` BIGINT COMMENT '创建时间',
--     `updated_at` BIGINT COMMENT '更新时间',
--     INDEX `idx_channel_model_time` (`channel_id`, `model_name`, `time_window_start`)
-- ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='渠道统计时序表';

-- For PostgreSQL
-- CREATE TABLE IF NOT EXISTS "channel_statistics" (
--     "id" SERIAL PRIMARY KEY,
--     "channel_id" INTEGER NOT NULL,
--     "model_name" VARCHAR(255) NOT NULL,
--     "time_window_start" BIGINT NOT NULL,
--     "request_count" INTEGER DEFAULT 0,
--     "fail_count" INTEGER DEFAULT 0,
--     "total_tokens" BIGINT DEFAULT 0,
--     "total_quota" BIGINT DEFAULT 0,
--     "total_latency_ms" BIGINT DEFAULT 0,
--     "stream_req_count" INTEGER DEFAULT 0,
--     "cache_hit_count" INTEGER DEFAULT 0,
--     "downtime_seconds" INTEGER DEFAULT 0,
--     "created_at" BIGINT,
--     "updated_at" BIGINT
-- );
-- CREATE INDEX IF NOT EXISTS "idx_channel_model_time" ON "channel_statistics" ("channel_id", "model_name", "time_window_start");

-- Note: SQLite and actual migration will be handled by GORM AutoMigrate
-- This SQL file is for reference and manual migration if needed

-- =====================================================
-- Migration Verification Queries
-- =====================================================

-- Check if new columns exist in channels table
-- SELECT COLUMN_NAME, DATA_TYPE, COLUMN_DEFAULT
-- FROM INFORMATION_SCHEMA.COLUMNS
-- WHERE TABLE_NAME = 'channels'
-- AND COLUMN_NAME IN ('avg_response_time', 'fail_rate', 'monitoring_config');

-- Check if channel_statistics table exists
-- SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = 'channel_statistics';

-- =====================================================
-- Rollback (if needed)
-- =====================================================

-- DROP TABLE IF EXISTS `channel_statistics`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `avg_response_time`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `fail_rate`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `avg_cache_hit_rate`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `stream_req_ratio`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `tpm`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `rpm`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `quota_pm`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `total_sessions`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `downtime_percentage`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `unique_users`;
-- ALTER TABLE `channels` DROP COLUMN IF EXISTS `monitoring_config`;
