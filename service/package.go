package service

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

func HandlePackage(c *gin.Context, channel *model.Channel) bool {
	userId := c.GetInt("id")
	var p2pGroupId *int
	// 从上下文中解析 Token 限制的 P2P 分组（当前实现仅支持单一 p2p_group_id）
	if raw, exists := c.Get(string(constant.ContextKeyTokenAllowedP2PGroups)); exists && raw != nil {
		if ids, ok := raw.([]int); ok && len(ids) > 0 {
			gid := ids[0]
			p2pGroupId = &gid
		}
	}

	subs, err := model.GetUserActiveSubscriptions(userId, p2pGroupId)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("get user active subscriptions failed: %v", err))
		return false
	}

	if len(subs) == 0 {
		return false // No active subscriptions, fallback to normal billing
	}

	now := time.Now()
	// TODO: Get pre-consumed quota from request
	preConsumedQuota := 1000 // Placeholder

	for _, sub := range subs {
		pkg, err := model.GetPackageByID(sub.PackageId)
		if err != nil {
			logger.LogError(c, fmt.Sprintf("get package by id failed: %v", err))
			continue
		}

		// Check RPM
		if pkg.RpmLimit > 0 && !checkRateLimit(c, sub.Id, "rpm", time.Minute, pkg.RpmLimit) {
			continue
		}

		// Check hourly limit
		if pkg.HourlyLimit > 0 && !checkQuotaLimit(c, sub.Id, "hourly", time.Hour, pkg.HourlyLimit, int64(preConsumedQuota)) {
			continue
		}

		// Check 4-hourly limit
		if pkg.FourHourlyLimit > 0 && !checkQuotaLimit(c, sub.Id, "4hourly", 4*time.Hour, pkg.FourHourlyLimit, int64(preConsumedQuota)) {
			continue
		}

		// Check daily limit
		if pkg.DailyLimit > 0 && !checkQuotaLimit(c, sub.Id, "daily", 24*time.Hour, pkg.DailyLimit, int64(preConsumedQuota)) {
			continue
		}

		// Check weekly limit
		if pkg.WeeklyLimit > 0 && !checkQuotaLimit(c, sub.Id, "weekly", 7*24*time.Hour, pkg.WeeklyLimit, int64(preConsumedQuota)) {
			continue
		}

		// Check total quota
		if sub.TotalConsumed+int64(preConsumedQuota) > pkg.Quota {
			continue
		}

		// All checks passed, consume from this subscription
		c.Set("subscription", sub)
		c.Set("package", pkg)
		c.Set("pre_consumed_quota", preConsumedQuota)

		// Record consumption in Redis
		recordConsumption(c, sub.Id, int64(preConsumedQuota), now)

		c.Next()
		return true
	}

	// All subscriptions are over limit, check fallback
	if len(subs) > 0 {
		lastPkg, _ := model.GetPackageByID(subs[len(subs)-1].PackageId)
		if lastPkg != nil && !lastPkg.FallbackToBalance {
			common.ApiError(c, fmt.Errorf("all subscription quotas exceeded"))
			c.AbortWithStatus(http.StatusTooManyRequests)
			return true // Handled, stop processing
		}
	}

	return false // Fallback to normal billing
}

func checkRateLimit(c *gin.Context, subId int, windowType string, d time.Duration, limit int) bool {
	key := fmt.Sprintf("sub:%d:%s:%d", subId, windowType, time.Now().Truncate(d).Unix())
	count, err := common.RDB.Incr(c, key).Result()
	if err != nil {
		logger.LogError(c, fmt.Sprintf("redis incr failed: %v", err))
		return true // Allow if redis fails
	}
	if count == 1 {
		common.RDB.Expire(c, key, d)
	}
	return count <= int64(limit)
}

func checkQuotaLimit(c *gin.Context, subId int, windowType string, d time.Duration, limit int64, amount int64) bool {
	key := fmt.Sprintf("sub_quota:%d:%s:%d", subId, windowType, time.Now().Truncate(d).Unix())
	val, err := common.RDB.Get(c, key).Result()
	if err != nil && err != redis.Nil {
		logger.LogError(c, fmt.Sprintf("redis get failed: %v", err))
		return true // Allow if redis fails
	}
	current, _ := strconv.ParseInt(val, 10, 64)
	return current+amount <= limit
}

func recordConsumption(c *gin.Context, subId int, amount int64, now time.Time) {
	windows := map[string]time.Duration{
		"rpm":     time.Minute,
		"hourly":  time.Hour,
		"4hourly": 4 * time.Hour,
		"daily":   24 * time.Hour,
		"weekly":  7 * 24 * time.Hour,
		"monthly": 30 * 24 * time.Hour,
	}

	pipe := common.RDB.TxPipeline()
	for key, d := range windows {
		if key == "rpm" {
			continue
		}
		redisKey := fmt.Sprintf("sub_quota:%d:%s:%d", subId, key, now.Truncate(d).Unix())
		pipe.IncrBy(c, redisKey, amount)
		pipe.Expire(c, redisKey, d+time.Hour) // Add a buffer
	}
	_, err := pipe.Exec(c)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("redis pipeline exec failed: %v", err))
	}
}
