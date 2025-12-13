-- 套餐监控与日志字段扩展迁移脚本
-- 相关设计：docs/NewAPI-支持多种包月套餐-优化版.md 第 11.2 节
-- 用途：在 logs 表中增加套餐相关字段，用于区分计费来源

-- ============================================
-- 1. 为 logs 表添加套餐相关字段
-- ============================================

-- billing_type: 计费类型
-- 值域: "balance"（用户余额） | "package"（套餐）
ALTER TABLE logs
ADD COLUMN billing_type VARCHAR(20) DEFAULT 'balance' COMMENT '计费类型: balance（余额）| package（套餐）';

-- package_id: 使用的套餐模板 ID
-- 为 0 表示使用用户余额计费
ALTER TABLE logs
ADD COLUMN package_id INT DEFAULT 0 COMMENT '使用的套餐模板ID（0=使用余额）';

-- subscription_id: 使用的订阅 ID
-- 为 0 表示使用用户余额计费
ALTER TABLE logs
ADD COLUMN subscription_id INT DEFAULT 0 COMMENT '使用的订阅ID（0=使用余额）';

-- ============================================
-- 2. 创建索引（用于监控指标查询）
-- ============================================

-- 按计费类型查询索引
CREATE INDEX idx_billing_type ON logs(billing_type);

-- 按套餐 ID 查询索引（用于分析套餐使用情况）
CREATE INDEX idx_package_id ON logs(package_id);

-- 按订阅 ID 查询索引（用于分析订阅消耗详情）
CREATE INDEX idx_subscription_id ON logs(subscription_id);

-- 联合索引：计费类型 + 创建时间（用于监控指标统计）
CREATE INDEX idx_billing_type_created_at ON logs(billing_type, created_at);

-- 联合索引：套餐 ID + 创建时间（用于套餐使用率分析）
CREATE INDEX idx_package_created_at ON logs(package_id, created_at);

-- ============================================
-- 3. 验证迁移结果
-- ============================================

-- 查看表结构（MySQL）
-- SHOW COLUMNS FROM logs WHERE Field IN ('billing_type', 'package_id', 'subscription_id');

-- 查看索引（MySQL）
-- SHOW INDEX FROM logs WHERE Key_name LIKE 'idx_billing%' OR Key_name LIKE 'idx_package%' OR Key_name LIKE 'idx_subscription%';

-- ============================================
-- 4. 回滚脚本（如需回退）
-- ============================================

/*
-- 删除索引
DROP INDEX idx_billing_type ON logs;
DROP INDEX idx_package_id ON logs;
DROP INDEX idx_subscription_id ON logs;
DROP INDEX idx_billing_type_created_at ON logs;
DROP INDEX idx_package_created_at ON logs;

-- 删除列
ALTER TABLE logs DROP COLUMN billing_type;
ALTER TABLE logs DROP COLUMN package_id;
ALTER TABLE logs DROP COLUMN subscription_id;
*/

-- ============================================
-- 5. 使用说明
-- ============================================

/*
执行迁移：
  mysql -u root -p database_name < bin/migration_log_package_fields.sql

验证字段：
  SELECT billing_type, package_id, subscription_id, quota, model_name
  FROM logs
  WHERE created_at > UNIX_TIMESTAMP(NOW()) - 3600
  LIMIT 10;

示例查询（统计套餐使用情况）：
  SELECT
    billing_type,
    COUNT(*) as request_count,
    SUM(quota) as total_quota
  FROM logs
  WHERE type = 2 AND created_at > UNIX_TIMESTAMP(NOW()) - 86400
  GROUP BY billing_type;
*/
