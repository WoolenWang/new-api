package testutil

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

// ===== 统计测试相关辅助函数 =====

// SimulateWindowInRedis 在miniredis中模拟创建滑动窗口
func SimulateWindowInRedis(mr *miniredis.Miniredis, subscriptionId int, period string, consumed, limit, startTime, duration int64) {
	key := FormatWindowKey(subscriptionId, period)
	endTime := startTime + duration

	mr.HSet(key, "start_time", fmt.Sprintf("%d", startTime))
	mr.HSet(key, "end_time", fmt.Sprintf("%d", endTime))
	mr.HSet(key, "consumed", fmt.Sprintf("%d", consumed))
	mr.HSet(key, "limit", fmt.Sprintf("%d", limit))

	// 设置TTL
	var ttl time.Duration
	switch period {
	case "rpm":
		ttl = 90 * time.Second
	case "hourly":
		ttl = 4200 * time.Second
	case "4hourly":
		ttl = 18000 * time.Second
	case "daily":
		ttl = 93600 * time.Second
	case "weekly":
		ttl = 691200 * time.Second
	default:
		ttl = 3600 * time.Second
	}
	mr.SetTTL(key, ttl)
}

// GetWindowConsumedFromRedis 从Redis获取窗口consumed值
func GetWindowConsumedFromRedis(t *testing.T, mr *miniredis.Miniredis, subscriptionId int, period string) int64 {
	key := FormatWindowKey(subscriptionId, period)
	consumedStr := mr.HGet(key, "consumed")
	if consumedStr == "" {
		return 0
	}
	consumed, _ := strconv.ParseInt(consumedStr, 10, 64)
	return consumed
}

// GetWindowLimitFromRedis 从Redis获取窗口limit值
func GetWindowLimitFromRedis(t *testing.T, mr *miniredis.Miniredis, subscriptionId int, period string) int64 {
	key := FormatWindowKey(subscriptionId, period)
	limitStr := mr.HGet(key, "limit")
	if limitStr == "" {
		return 0
	}
	limit, _ := strconv.ParseInt(limitStr, 10, 64)
	return limit
}

// GetWindowTimeFromRedis 从Redis获取窗口时间信息
func GetWindowTimeFromRedis(t *testing.T, mr *miniredis.Miniredis, subscriptionId int, period string) (startTime, endTime int64) {
	key := FormatWindowKey(subscriptionId, period)

	startTimeStr := mr.HGet(key, "start_time")
	if startTimeStr != "" {
		startTime, _ = strconv.ParseInt(startTimeStr, 10, 64)
	}

	endTimeStr := mr.HGet(key, "end_time")
	if endTimeStr != "" {
		endTime, _ = strconv.ParseInt(endTimeStr, 10, 64)
	}

	return startTime, endTime
}

// CalculateWindowUtilizationRate 计算窗口使用率（百分比）
func CalculateWindowUtilizationRate(consumed, limit int64) float64 {
	if limit == 0 {
		return 0
	}
	return float64(consumed) / float64(limit) * 100
}

// CalculateRemainingTime 计算窗口剩余时间（秒）
func CalculateRemainingTime(endTime, currentTime int64) int64 {
	remaining := endTime - currentTime
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CalculateRemainingQuota 计算剩余额度
func CalculateRemainingQuota(quota, totalConsumed int64) int64 {
	remaining := quota - totalConsumed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// AssertWindowConsumed 断言窗口consumed值（支持 RedisMock 或 miniredis）
func AssertWindowConsumed(t *testing.T, miniRedis interface{}, subscriptionId int, period string, expectedConsumed int64) {
	switch r := miniRedis.(type) {
	case *RedisMock:
		key := FormatWindowKey(subscriptionId, period)
		r.AssertHashFieldInt64(t, key, "consumed", expectedConsumed)
	case *miniredis.Miniredis:
		consumed := GetWindowConsumedFromRedis(t, r, subscriptionId, period)
		assert.Equal(t, expectedConsumed, consumed,
			fmt.Sprintf("Window[%s] consumed should be %d, got %d", period, expectedConsumed, consumed))
	default:
		t.Fatalf("unsupported redis mock type %T", miniRedis)
	}
}

// AssertWindowUtilization 断言窗口使用率
func AssertWindowUtilization(t *testing.T, mr *miniredis.Miniredis, subscriptionId int, period string, expectedRate float64) {
	consumed := GetWindowConsumedFromRedis(t, mr, subscriptionId, period)
	limit := GetWindowLimitFromRedis(t, mr, subscriptionId, period)
	actualRate := CalculateWindowUtilizationRate(consumed, limit)

	assert.InDelta(t, expectedRate, actualRate, 0.01,
		fmt.Sprintf("Window[%s] utilization should be %.2f%%, got %.2f%%", period, expectedRate, actualRate))
}

// AssertWindowTimeLeft 断言窗口剩余时间
func AssertWindowTimeLeft(t *testing.T, mr *miniredis.Miniredis, subscriptionId int, period string, currentTime, expectedTimeLeft int64) {
	_, endTime := GetWindowTimeFromRedis(t, mr, subscriptionId, period)
	actualTimeLeft := CalculateRemainingTime(endTime, currentTime)

	assert.Equal(t, expectedTimeLeft, actualTimeLeft,
		fmt.Sprintf("Window[%s] time left should be %d seconds, got %d", period, expectedTimeLeft, actualTimeLeft))
}

// AssertRemainingQuota 断言剩余额度
func AssertRemainingQuota(t *testing.T, subscriptionId int, expectedRemaining int64) {
	sub, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Failed to get subscription")

	pkg, err := model.GetPackageByID(sub.PackageId)
	assert.Nil(t, err, "Failed to get package")

	actualRemaining := CalculateRemainingQuota(pkg.Quota, sub.TotalConsumed)
	assert.Equal(t, expectedRemaining, actualRemaining,
		fmt.Sprintf("Remaining quota should be %d, got %d", expectedRemaining, actualRemaining))
}

// UpdateSubscriptionConsumed 更新订阅的total_consumed（用于测试）
func UpdateSubscriptionConsumed(t *testing.T, subscriptionId int, consumed int64) {
	sub, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Failed to get subscription")

	sub.TotalConsumed = consumed
	err = model.DB.Save(sub).Error
	assert.Nil(t, err, "Failed to update subscription consumed")
}

// IncrementSubscriptionConsumed 增加订阅的total_consumed（用于测试）
func IncrementSubscriptionConsumed(t *testing.T, subscriptionId int, increment int64) {
	sub, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Failed to get subscription")

	sub.TotalConsumed += increment
	err = model.DB.Save(sub).Error
	assert.Nil(t, err, "Failed to increment subscription consumed")
}
