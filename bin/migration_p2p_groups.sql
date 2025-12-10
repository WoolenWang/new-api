-- Migration for P2P Group Feature
-- This migration adds support for user-managed P2P groups and decouples billing from routing

-- ============================================================
-- 1. Create P2P Groups Table
-- ============================================================
CREATE TABLE IF NOT EXISTS `groups` (
    `id` INTEGER PRIMARY KEY AUTOINCREMENT,
    `name` VARCHAR(50) NOT NULL,                -- Unique identifier/code
    `display_name` VARCHAR(100),                -- Display name
    `owner_id` INTEGER NOT NULL,                -- Owner user ID (NewAPI user_id)
    `type` INTEGER DEFAULT 1,                   -- 1=Private, 2=Shared
    `join_method` INTEGER DEFAULT 0,            -- 0=Invite, 1=Approval, 2=Password
    `join_key` VARCHAR(50),                     -- Password/Key for joining
    `description` TEXT,                         -- Group description
    `created_at` BIGINT,
    `updated_at` BIGINT,
    INDEX `idx_owner_id` (`owner_id`),
    INDEX `idx_type` (`type`)
);

-- ============================================================
-- 2. Create User-Group Association Table
-- ============================================================
CREATE TABLE IF NOT EXISTS `user_groups` (
    `id` INTEGER PRIMARY KEY AUTOINCREMENT,
    `user_id` INTEGER NOT NULL,                 -- Member user ID
    `group_id` INTEGER NOT NULL,                -- P2P group ID
    `role` INTEGER DEFAULT 0,                   -- 0=Member, 1=Admin
    `status` INTEGER DEFAULT 0,                 -- 0=Pending, 1=Active, 2=Rejected, 3=Banned, 4=Left
    `created_at` BIGINT,
    `updated_at` BIGINT,
    UNIQUE(`user_id`, `group_id`),              -- Prevent duplicate membership
    INDEX `idx_user_id` (`user_id`),
    INDEX `idx_group_id` (`group_id`),
    INDEX `idx_status` (`status`)
);

-- ============================================================
-- 3. Modify Channels Table (Add P2P Group Support)
-- ============================================================
-- Add allowed_groups column to store P2P group IDs (JSON array)
-- Note: This column stores P2P group IDs that can access the channel
-- The existing 'group' column continues to store system group names
ALTER TABLE `channels` ADD COLUMN `allowed_groups` TEXT;

-- ============================================================
-- 4. Modify Tokens Table (Add P2P Group Restriction)
-- ============================================================
-- Add allowed_p2p_groups column to restrict which P2P groups a token can use
ALTER TABLE `tokens` ADD COLUMN `allowed_p2p_groups` TEXT;

-- ============================================================
-- Migration Notes:
-- ============================================================
-- 1. The 'group' column in channels table remains unchanged and stores system groups
-- 2. The new 'allowed_groups' column in channels stores P2P group IDs as JSON array
-- 3. BillingGroup vs RoutingGroups:
--    - BillingGroup: Used for billing, locked to User.Group or Token.Group
--    - RoutingGroups: Used for channel routing, includes BillingGroup + Active P2P Groups
-- 4. Cache invalidation triggers:
--    - When user joins/leaves a P2P group: Invalidate user_groups:{user_id} cache
--    - When group is deleted: Invalidate all member caches
-- 5. Security:
--    - Billing always uses BillingGroup (prevents billing bypass via P2P groups)
--    - Token.allowed_p2p_groups restricts which P2P groups the token can access
-- ============================================================
