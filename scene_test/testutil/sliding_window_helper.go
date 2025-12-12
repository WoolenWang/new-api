package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

// WindowTestConfig 窗口测试配置
type WindowTestConfig struct {
	SubscriptionId int
	Period         string
	Duration       int64
	Limit          int64
	TTL            int64
}

// CreateWindowConfig 创建窗口配置
func CreateWindowConfig(subscriptionId int, period string, duration int64, limit int64, ttl int64) WindowTestConfig {
	return WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         period,
		Duration:       duration,
		Limit:          limit,
		TTL:            ttl,
	}
}

// CreateHourlyWindowConfig 创建小时窗口配置（常用）
func CreateHourlyWindowConfig(subscriptionId int, limit int64) WindowTestConfig {
	return WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         "hourly",
		Duration:       3600,
		Limit:          limit,
		TTL:            4200,
	}
}

// CreateRPMWindowConfig 创建RPM窗口配置（常用）
func CreateRPMWindowConfig(subscriptionId int, limit int64) WindowTestConfig {
	return WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         "rpm",
		Duration:       60,
		Limit:          limit,
		TTL:            90,
	}
}

// CreateDailyWindowConfig 创建每日窗口配置（常用）
func CreateDailyWindowConfig(subscriptionId int, limit int64) WindowTestConfig {
	return WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         "daily",
		Duration:       86400,
		Limit:          limit,
		TTL:            93600,
	}
}

// GetWindowKey 生成窗口的Redis Key
func GetWindowKey(subscriptionId int, period string) string {
	return fmt.Sprintf("subscription:%d:%s:window", subscriptionId, period)
}

// AssertWindowExists 断言窗口存在
func AssertWindowExists(t *testing.T, redisInstance interface{}, subscriptionId int, period string) bool {
	key := GetWindowKey(subscriptionId, period)

	switch v := redisInstance.(type) {
	case *RedisMock:
		v.AssertKeyExists(t, key)
		return true
	case *miniredis.Miniredis:
		exists := v.Exists(key)
		assert.True(t, exists, fmt.Sprintf("Redis key '%s' should exist", key))
		return exists
	default:
		t.Fatalf("AssertWindowExists: unsupported redis instance type %T", redisInstance)
		return false
	}
}

// AssertWindowNotExists 断言窗口不存在
func AssertWindowNotExists(t *testing.T, rm *RedisMock, subscriptionId int, period string) {
	key := GetWindowKey(subscriptionId, period)
	rm.AssertKeyNotExists(t, key)
}

// AssertWindowLimit 断言窗口limit值
func AssertWindowLimit(t *testing.T, rm *RedisMock, subscriptionId int, period string, expectedLimit int64) {
	key := GetWindowKey(subscriptionId, period)
	rm.AssertHashFieldInt64(t, key, "limit", expectedLimit)
}

// AssertWindowTime 断言窗口时间范围
func AssertWindowTime(t *testing.T, rm *RedisMock, subscriptionId int, period string, startTime int64, endTime int64) {
	key := GetWindowKey(subscriptionId, period)
	rm.AssertHashFieldInt64(t, key, "start_time", startTime)
	rm.AssertHashFieldInt64(t, key, "end_time", endTime)
}

// AssertWindowTimeRange 断言窗口时间范围（允许误差）
func AssertWindowTimeRange(t *testing.T, rm *RedisMock, subscriptionId int, period string, expectedDuration int64, delta int64) {
	key := GetWindowKey(subscriptionId, period)
	startTime, err := rm.GetHashFieldInt64(key, "start_time")
	assert.Nil(t, err, "Failed to get start_time")
	endTime, err := rm.GetHashFieldInt64(key, "end_time")
	assert.Nil(t, err, "Failed to get end_time")

	actualDuration := endTime - startTime
	assert.InDelta(t, expectedDuration, actualDuration, float64(delta),
		fmt.Sprintf("Window duration should be around %d seconds (actual: %d)", expectedDuration, actualDuration))
}

// GetWindowConsumed 获取窗口consumed值
func GetWindowConsumed(t *testing.T, rm *RedisMock, subscriptionId int, period string) int64 {
	key := GetWindowKey(subscriptionId, period)
	consumed, err := rm.GetHashFieldInt64(key, "consumed")
	assert.Nil(t, err, "Failed to get consumed")
	return consumed
}

// GetWindowStartTime 获取窗口开始时间
func GetWindowStartTime(t *testing.T, rm *RedisMock, subscriptionId int, period string) int64 {
	key := GetWindowKey(subscriptionId, period)
	startTime, err := rm.GetHashFieldInt64(key, "start_time")
	assert.Nil(t, err, "Failed to get start_time")
	return startTime
}

// GetWindowEndTime 获取窗口结束时间
func GetWindowEndTime(t *testing.T, rm *RedisMock, subscriptionId int, period string) int64 {
	key := GetWindowKey(subscriptionId, period)
	endTime, err := rm.GetHashFieldInt64(key, "end_time")
	assert.Nil(t, err, "Failed to get end_time")
	return endTime
}

// CallCheckAndConsumeWindow 调用滑动窗口检查并消耗（测试用封装）
func CallCheckAndConsumeWindow(
	t *testing.T,
	ctx context.Context,
	config WindowTestConfig,
	quota int64,
) *service.WindowResult {
	svcConfig := service.SlidingWindowConfig{
		Period:   config.Period,
		Duration: config.Duration,
		Limit:    config.Limit,
		TTL:      config.TTL,
	}

	result, err := service.CheckAndConsumeSlidingWindow(ctx, config.SubscriptionId, svcConfig, quota)
	assert.Nil(t, err, "CheckAndConsumeSlidingWindow should not return error")
	return result
}

// AssertWindowResultSuccess 断言窗口操作成功
func AssertWindowResultSuccess(t *testing.T, result *service.WindowResult, expectedConsumed int64) {
	assert.True(t, result.Success, "Window operation should succeed")
	assert.Equal(t, expectedConsumed, result.Consumed, fmt.Sprintf("Consumed should be %d", expectedConsumed))
	assert.Greater(t, result.StartTime, int64(0), "Start time should be set")
	assert.Greater(t, result.EndTime, result.StartTime, "End time should be after start time")
}

// AssertWindowResultFailed 断言窗口操作失败（超限）
func AssertWindowResultFailed(t *testing.T, result *service.WindowResult, currentConsumed int64) {
	assert.False(t, result.Success, "Window operation should fail due to limit exceeded")
	assert.Equal(t, currentConsumed, result.Consumed, fmt.Sprintf("Consumed should remain %d", currentConsumed))
}

// AssertWindowResultTimeMatch 断言窗口结果的时间与Redis一致
func AssertWindowResultTimeMatch(t *testing.T, rm *RedisMock, result *service.WindowResult, subscriptionId int, period string) {
	key := GetWindowKey(subscriptionId, period)
	redisStartTime, _ := rm.GetHashFieldInt64(key, "start_time")
	redisEndTime, _ := rm.GetHashFieldInt64(key, "end_time")

	assert.Equal(t, redisStartTime, result.StartTime, "Result start_time should match Redis")
	assert.Equal(t, redisEndTime, result.EndTime, "Result end_time should match Redis")
}

// DumpWindowInfo 打印窗口详细信息（用于调试）
func DumpWindowInfo(t *testing.T, rm *RedisMock, subscriptionId int, period string) {
	key := GetWindowKey(subscriptionId, period)
	rm.DumpKey(t, key)
}

// AssertAllWindowsNotExist 断言所有窗口都不存在（用于测试无请求不创建Key）
func AssertAllWindowsNotExist(t *testing.T, rm *RedisMock, subscriptionId int, periods []string) {
	for _, period := range periods {
		AssertWindowNotExists(t, rm, subscriptionId, period)
	}
}

// AssertWindowTTL 断言窗口的TTL（允许误差）
func AssertWindowTTL(t *testing.T, rm *RedisMock, subscriptionId int, period string, expectedTTL time.Duration, delta time.Duration) {
	key := GetWindowKey(subscriptionId, period)
	rm.AssertTTL(t, key, expectedTTL, delta)
}

// WaitAndCheckWindowExpired 等待窗口过期并检查
func WaitAndCheckWindowExpired(t *testing.T, rm *RedisMock, subscriptionId int, period string, waitDuration time.Duration) {
	// 快进时间
	rm.FastForward(waitDuration)

	// 检查窗口是否仍然存在（可能被TTL清理）
	key := GetWindowKey(subscriptionId, period)
	exists := rm.CheckKeyExists(t, key)

	if exists {
		// 窗口仍存在，检查end_time是否已过期
		endTime := GetWindowEndTime(t, rm, subscriptionId, period)
		now := time.Now().Unix()
		assert.LessOrEqual(t, endTime, now, "Window should be expired")
	}
}

// CreateMultipleWindows 创建多个不同维度的窗口（用于测试多维度独立滑动）
func CreateMultipleWindows(
	t *testing.T,
	ctx context.Context,
	subscriptionId int,
	quotaToConsume int64,
	configs []WindowTestConfig,
) []*service.WindowResult {
	results := make([]*service.WindowResult, len(configs))

	for i, config := range configs {
		result := CallCheckAndConsumeWindow(t, ctx, config, quotaToConsume)
		results[i] = result
	}

	return results
}

// AssertSubscriptionTotalConsumed 断言订阅的总消耗
func AssertSubscriptionTotalConsumed(t *testing.T, subscriptionId int, expectedConsumed int64) {
	sub, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Failed to get subscription")
	assert.Equal(t, expectedConsumed, sub.TotalConsumed,
		fmt.Sprintf("Subscription total_consumed should be %d", expectedConsumed))
}

// GetUserQuotaFromDB 从数据库获取用户余额（辅助函数）
func GetUserQuotaFromDB(userId int) (int, error) {
	return model.GetUserQuota(userId, true) // true表示强制从DB读取
}
