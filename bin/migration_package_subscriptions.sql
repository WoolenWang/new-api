-- Packages Table
CREATE TABLE `packages` (
    `id` int(11) NOT NULL AUTO_INCREMENT,
    `name` varchar(100) NOT NULL,
    `description` text,
    `status` int(11) DEFAULT 1,
    `priority` int(11) DEFAULT 10,
    `quota` bigint(20) NOT NULL,
    `duration_type` varchar(20) NOT NULL,
    `duration` int(11) NOT NULL,
    `rpm_limit` int(11) DEFAULT 0,
    `hourly_limit` bigint(20) DEFAULT 0,
    `four_hourly_limit` bigint(20) DEFAULT 0,
    `daily_limit` bigint(20) DEFAULT 0,
    `weekly_limit` bigint(20) DEFAULT 0,
    `fallback_to_balance` tinyint(1) DEFAULT 0,
    `creator_id` int(11) DEFAULT 0,
    `p2p_group_id` int(11) DEFAULT 0,
    `created_at` bigint(20) NOT NULL,
    `updated_at` bigint(20) NOT NULL,
    PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Subscriptions Table
CREATE TABLE `subscriptions` (
    `id` int(11) NOT NULL AUTO_INCREMENT,
    `user_id` int(11) NOT NULL,
    `package_id` int(11) NOT NULL,
    `status` varchar(20) NOT NULL DEFAULT 'inventory',
    `total_consumed` bigint(20) DEFAULT 0,
    `start_time` bigint(20),
    `end_time` bigint(20),
    `subscribed_at` bigint(20) NOT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_user_id` (`user_id`),
    INDEX `idx_package_id` (`package_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
