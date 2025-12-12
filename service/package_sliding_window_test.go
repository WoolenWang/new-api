package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRedis 创建一个测试用的Redis实例
func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client, func()) {
	t.Helper()

	// 启动miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err, "failed to start miniredis")

	// 创建Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// 备份原有的Redis配置
	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled

	// 替换为测试用的Redis
	common.RDB = rdb
	common.RedisEnabled = true

	// 重置scriptSHA，强制重新加载
	scriptSHA = ""

	cleanup := func() {
		rdb.Close()
		mr.Close()
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
		scriptSHA = ""
	}

	return mr, rdb, cleanup
}

// TestLuaScript_FirstRequest 测试首次请求创建窗口
func TestLuaScript_FirstRequest(t *testing.T) {
	_, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	config := SlidingWindowConfig{
		Period:   "hourly",
		Duration: 3600,
		Limit:    20000000,
		TTL:      4200,
	}

	// 首次请求
	result, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 2500000)
	require.NoError(t, err)
	assert.True(t, result.Success, "first request should succeed")
	assert.Equal(t, int64(2500000), result.Consumed, "consumed should equal quota")
	assert.Greater(t, result.EndTime, result.StartTime, "end_time should be after start_time")
	assert.Equal(t, int64(3600), result.EndTime-result.StartTime, "window duration should be 1 hour")
}

// TestLuaScript_WithinWindow 测试窗口内扣减
func TestLuaScript_WithinWindow(t *testing.T) {
	_, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	config := SlidingWindowConfig{
		Period:   "hourly",
		Duration: 3600,
		Limit:    20000000,
		TTL:      4200,
	}

	// 第一次请求
	result1, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 2500000)
	require.NoError(t, err)
	assert.True(t, result1.Success)
	assert.Equal(t, int64(2500000), result1.Consumed)

	// 第二次请求（窗口内）
	result2, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 3000000)
	require.NoError(t, err)
	assert.True(t, result2.Success, "second request should succeed")
	assert.Equal(t, int64(5500000), result2.Consumed, "consumed should accumulate")
	assert.Equal(t, result1.StartTime, result2.StartTime, "start_time should not change")
	assert.Equal(t, result1.EndTime, result2.EndTime, "end_time should not change")
}

// TestLuaScript_Exceeded 测试超限拒绝
func TestLuaScript_Exceeded(t *testing.T) {
	_, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	config := SlidingWindowConfig{
		Period:   "hourly",
		Duration: 3600,
		Limit:    5000000, // 限额较小，方便测试
		TTL:      4200,
	}

	// 第一次请求，消耗大部分额度
	result1, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 4000000)
	require.NoError(t, err)
	assert.True(t, result1.Success)

	// 第二次请求，尝试超限
	result2, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 2000000)
	require.NoError(t, err)
	assert.False(t, result2.Success, "should reject when exceeding limit")
	assert.Equal(t, int64(4000000), result2.Consumed, "consumed should not change")
}

// TestLuaScript_Expired 测试窗口过期重建
func TestLuaScript_Expired(t *testing.T) {
	mr, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	config := SlidingWindowConfig{
		Period:   "hourly",
		Duration: 3600,
		Limit:    20000000,
		TTL:      4200,
	}

	// 创建窗口
	result1, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 2500000)
	require.NoError(t, err)
	assert.True(t, result1.Success)

	// 模拟时间流逝（快进超过窗口时长）
	mr.FastForward(3601 * time.Second)

	// 再次请求，窗口应该过期并重建
	result2, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 3000000)
	require.NoError(t, err)
	assert.True(t, result2.Success, "should succeed after window expired")
	assert.Equal(t, int64(3000000), result2.Consumed, "consumed should reset to new quota")
	assert.Greater(t, result2.StartTime, result1.StartTime, "start_time should be updated")
}

// TestGetSlidingWindowConfigs 测试窗口配置生成
func TestGetSlidingWindowConfigs(t *testing.T) {
	tests := []struct {
		name     string
		pkg      *model.Package
		expected int // 期望的配置数量
	}{
		{
			name: "all_limits_set",
			pkg: &model.Package{
				RpmLimit:        60,
				HourlyLimit:     10000000,
				FourHourlyLimit: 30000000,
				DailyLimit:      80000000,
				WeeklyLimit:     500000000,
			},
			expected: 5,
		},
		{
			name: "only_rpm_and_hourly",
			pkg: &model.Package{
				RpmLimit:        60,
				HourlyLimit:     10000000,
				FourHourlyLimit: 0,
				DailyLimit:      0,
				WeeklyLimit:     0,
			},
			expected: 2,
		},
		{
			name: "no_limits",
			pkg: &model.Package{
				RpmLimit:        0,
				HourlyLimit:     0,
				FourHourlyLimit: 0,
				DailyLimit:      0,
				WeeklyLimit:     0,
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs := GetSlidingWindowConfigs(tt.pkg)
			assert.Equal(t, tt.expected, len(configs), "config count mismatch")

			// 验证每个配置的Duration和TTL是否正确
			for _, config := range configs {
				assert.Greater(t, config.TTL, config.Duration, "TTL should be greater than Duration")
			}
		})
	}
}

// TestCheckAllSlidingWindows 测试批量窗口检查
func TestCheckAllSlidingWindows(t *testing.T) {
	_, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	pkg := &model.Package{
		RpmLimit:    2, // 限制为2次请求，方便测试
		HourlyLimit: 10000000,
	}

	subscription := &model.Subscription{Id: 100}

	// 第一次请求，应该成功
	err := CheckAllSlidingWindows(ctx, subscription, pkg, 1000000)
	assert.NoError(t, err, "first request should pass all windows")

	// 第二次请求，应该成功
	err = CheckAllSlidingWindows(ctx, subscription, pkg, 1000000)
	assert.NoError(t, err, "second request should pass all windows")

	// 第三次请求，应该被RPM限制拒绝
	err = CheckAllSlidingWindows(ctx, subscription, pkg, 1000000)
	assert.Error(t, err, "third request should be rejected by RPM limit")
	assert.Contains(t, err.Error(), "rpm", "error should mention rpm limit")
}

// TestGetSlidingWindowStatus 测试单个窗口状态查询
func TestGetSlidingWindowStatus(t *testing.T) {
	_, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	config := SlidingWindowConfig{
		Period:   "hourly",
		Duration: 3600,
		Limit:    20000000,
		TTL:      4200,
	}

	// 查询不存在的窗口
	status1, err := GetSlidingWindowStatus(ctx, 1, "hourly")
	require.NoError(t, err)
	assert.False(t, status1.IsActive, "window should not be active before first request")

	// 创建窗口
	_, err = CheckAndConsumeSlidingWindow(ctx, 1, config, 5000000)
	require.NoError(t, err)

	// 查询存在的窗口
	status2, err := GetSlidingWindowStatus(ctx, 1, "hourly")
	require.NoError(t, err)
	assert.True(t, status2.IsActive, "window should be active")
	assert.Equal(t, int64(5000000), status2.Consumed)
	assert.Equal(t, int64(20000000), status2.Limit)
	assert.Equal(t, int64(15000000), status2.Remaining)
	assert.Greater(t, status2.TimeLeft, int64(0), "time_left should be positive")
}

// TestGetAllSlidingWindowsStatus 测试Pipeline批量查询
func TestGetAllSlidingWindowsStatus(t *testing.T) {
	_, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	pkg := &model.Package{
		RpmLimit:    60,
		HourlyLimit: 10000000,
		DailyLimit:  50000000,
	}

	// 查询空窗口
	statuses1, err := GetAllSlidingWindowsStatus(ctx, 1, pkg)
	require.NoError(t, err)
	assert.Equal(t, 3, len(statuses1), "should return status for all configured windows")
	for _, status := range statuses1 {
		assert.False(t, status.IsActive, "all windows should be inactive initially")
	}

	// 创建一些窗口
	configs := GetSlidingWindowConfigs(pkg)
	for _, config := range configs {
		quota := int64(1000000)
		if config.Period == "rpm" {
			quota = 1
		}
		_, err := CheckAndConsumeSlidingWindow(ctx, 1, config, quota)
		require.NoError(t, err)
	}

	// 再次查询，应该显示活跃状态
	statuses2, err := GetAllSlidingWindowsStatus(ctx, 1, pkg)
	require.NoError(t, err)
	assert.Equal(t, 3, len(statuses2))

	activeCount := 0
	for _, status := range statuses2 {
		if status.IsActive {
			activeCount++
			assert.Greater(t, status.Consumed, int64(0), "consumed should be positive")
			assert.Greater(t, status.TimeLeft, int64(0), "time_left should be positive")
		}
	}
	assert.Equal(t, 3, activeCount, "all windows should be active after requests")
}

// TestRedisDisabled 测试Redis不可用时的降级行为
func TestRedisDisabled(t *testing.T) {
	// 备份原有配置
	oldRedisEnabled := common.RedisEnabled
	defer func() {
		common.RedisEnabled = oldRedisEnabled
	}()

	// 禁用Redis
	common.RedisEnabled = false

	ctx := context.Background()

	config := SlidingWindowConfig{
		Period:   "hourly",
		Duration: 3600,
		Limit:    20000000,
		TTL:      4200,
	}

	// 应该直接返回成功（降级）
	result, err := CheckAndConsumeSlidingWindow(ctx, 1, config, 2500000)
	require.NoError(t, err)
	assert.True(t, result.Success, "should allow through when Redis is disabled")

	// 批量检查也应该成功
	pkg := &model.Package{
		RpmLimit:    60,
		HourlyLimit: 10000000,
	}
	subscription := &model.Subscription{Id: 1}

	err = CheckAllSlidingWindows(ctx, subscription, pkg, 1000000)
	assert.NoError(t, err, "should allow through when Redis is disabled")

	// 查询应该返回空状态
	status, err := GetSlidingWindowStatus(ctx, 1, "hourly")
	require.NoError(t, err)
	assert.False(t, status.IsActive, "should return inactive status when Redis is disabled")
}

// TestRPM_SpecialHandling 测试RPM窗口的特殊处理（quota=1）
func TestRPM_SpecialHandling(t *testing.T) {
	_, _, cleanup := setupTestRedis(t)
	defer cleanup()

	ctx := context.Background()

	pkg := &model.Package{
		RpmLimit:    3, // 限制为3次请求/分钟
		HourlyLimit: 100000000,
	}
	subscription := &model.Subscription{Id: 1}

	// 连续发起3次请求，应该都成功
	for i := 0; i < 3; i++ {
		err := CheckAllSlidingWindows(ctx, subscription, pkg, 1000000) // 大额quota
		assert.NoError(t, err, fmt.Sprintf("request %d should succeed", i+1))
	}

	// 第4次请求应该被RPM限制
	err := CheckAllSlidingWindows(ctx, subscription, pkg, 1000000)
	assert.Error(t, err, "4th request should be rejected by RPM")

	// 验证RPM窗口的consumed值
	status, err := GetSlidingWindowStatus(ctx, 1, "rpm")
	require.NoError(t, err)
	assert.True(t, status.IsActive)
	assert.Equal(t, int64(3), status.Consumed, "RPM window should count requests, not quota")
	assert.Equal(t, int64(3), status.Limit)
}

// BenchmarkCheckAndConsumeSlidingWindow 性能基准测试
func BenchmarkCheckAndConsumeSlidingWindow(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled
	defer func() {
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
	}()

	common.RDB = rdb
	common.RedisEnabled = true
	scriptSHA = ""

	ctx := context.Background()
	config := SlidingWindowConfig{
		Period:   "hourly",
		Duration: 3600,
		Limit:    100000000,
		TTL:      4200,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CheckAndConsumeSlidingWindow(ctx, 1, config, 1000)
	}
}

// BenchmarkGetAllSlidingWindowsStatus Pipeline批量查询性能测试
func BenchmarkGetAllSlidingWindowsStatus(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	oldRDB := common.RDB
	oldRedisEnabled := common.RedisEnabled
	defer func() {
		common.RDB = oldRDB
		common.RedisEnabled = oldRedisEnabled
	}()

	common.RDB = rdb
	common.RedisEnabled = true
	scriptSHA = ""

	ctx := context.Background()
	pkg := &model.Package{
		RpmLimit:        60,
		HourlyLimit:     10000000,
		FourHourlyLimit: 30000000,
		DailyLimit:      80000000,
		WeeklyLimit:     500000000,
	}

	// 预先创建所有窗口
	configs := GetSlidingWindowConfigs(pkg)
	for _, config := range configs {
		quota := int64(1000000)
		if config.Period == "rpm" {
			quota = 1
		}
		_, _ = CheckAndConsumeSlidingWindow(ctx, 1, config, quota)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetAllSlidingWindowsStatus(ctx, 1, pkg)
	}
}
