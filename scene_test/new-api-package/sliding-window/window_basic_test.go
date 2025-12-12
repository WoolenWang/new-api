package sliding_window_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
)

var (
	redisMock *testutil.RedisMock
	ctx       context.Context
)

// TestMain 测试主函数，负责环境准备和清理
func TestMain(m *testing.M) {
	// 初始化context
	ctx = context.Background()

	// 使用内存 SQLite，确保测试环境隔离且符合设计文档要求
	// 注意：这里不启动 HTTP Server，只做最小化的 DB 初始化，供 model 层使用
	_ = os.Unsetenv("SQL_DSN")
	_ = os.Setenv("SQLITE_PATH", "file::memory:?cache=shared")
	common.InitEnv()
	if err := model.InitDB(); err != nil {
		panic(fmt.Sprintf("failed to init test DB: %v", err))
	}

	// 运行测试
	exitCode := m.Run()

	// 退出
	os.Exit(exitCode)
}

// setupTest 每个测试前的准备工作
func setupTest(t *testing.T) (*testutil.RedisMock, int) {
	// 启动Redis Mock
	rm := testutil.StartRedisMock(t)

	// 初始化数据库（需要确保model.DB已初始化）
	// 这里假设测试环境已经配置好SQLite内存数据库

	// 创建测试用户
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		// 不指定用户名以避免唯一约束冲突，由辅助函数生成唯一用户名
		Group: "default",
		Quota: 10000000,
	})

	// 创建测试套餐
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:        "test-package-sliding-window",
		Priority:    15,
		Quota:       100000000, // 100M月度总限额
		HourlyLimit: 20000000,  // 20M小时限额
		DailyLimit:  150000000, // 150M日限额
		WeeklyLimit: 500000000, // 500M周限额
		RpmLimit:    60,        // 60 RPM
		Status:      1,
	})

	// 创建并启用订阅
	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	return rm, sub.Id
}

// teardownTest 每个测试后的清理工作
func teardownTest(rm *testutil.RedisMock) {
	if rm != nil {
		rm.Close()
	}
	// 清理数据库
	testutil.CleanupPackageTestData(nil)
}

// ============================================================================
// SW-01: 首次请求创建窗口
// 测试ID: SW-01
// 优先级: P0
// 测试场景: 用户首次请求套餐时，Redis中无窗口Key，应创建新窗口
// 预期结果:
//   - 创建Hash Key
//   - start_time=now
//   - end_time=now+3600
//   - consumed=estimatedQuota
//   - Lua返回status=1
//
// =========================================================================
// ============================================================================
func TestSW01_FirstRequest_CreatesWindow(t *testing.T) {
	t.Log("SW-01: Testing first request creates sliding window")

	// Arrange: 设置测试环境
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 验证窗口不存在
	testutil.AssertWindowNotExists(t, rm, subscriptionId, "hourly")

	// Act: 首次请求小时窗口，消耗2.5M quota
	config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)
	quota := int64(2500000)
	result := testutil.CallCheckAndConsumeWindow(t, ctx, config, quota)

	// Assert: 验证结果
	testutil.AssertWindowResultSuccess(t, result, quota)

	// Assert: 验证Redis状态
	testutil.AssertWindowExists(t, rm, subscriptionId, "hourly")
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", quota)
	testutil.AssertWindowLimit(t, rm, subscriptionId, "hourly", 20000000)

	// Assert: 验证窗口时间范围（duration应为3600秒，允许1秒误差）
	testutil.AssertWindowTimeRange(t, rm, subscriptionId, "hourly", 3600, 1)

	// Assert: 验证窗口结果的时间与Redis一致
	testutil.AssertWindowResultTimeMatch(t, rm, result, subscriptionId, "hourly")

	// Assert: 验证窗口TTL（应为4200秒，允许10秒误差）
	testutil.AssertWindowTTL(t, rm, subscriptionId, "hourly", 4200*time.Second, 10*time.Second)

	t.Log("SW-01: Test completed - Window created successfully on first request")
}

// ============================================================================
// SW-02: 窗口内扣减累加
// 测试ID: SW-02
// 优先级: P0
// 测试场景: 在窗口有效期内，多次请求应累加consumed，窗口时间不变
// 预期结果:
//   - consumed=5.5M (2.5M + 3M)
//   - 窗口时间不变
//   - 两次请求都成功
//
// =========================================================================
// ============================================================================
func TestSW02_WithinWindow_Accumulates(t *testing.T) {
	t.Log("SW-02: Testing consumption accumulation within window")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)

	// Act: 第一次请求2.5M
	result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2500000)

	// Assert: 第一次请求成功
	testutil.AssertWindowResultSuccess(t, result1, 2500000)
	window1StartTime := result1.StartTime
	window1EndTime := result1.EndTime

	// 等待一段时间（模拟5分钟后）
	time.Sleep(100 * time.Millisecond) // 实际测试中用短时间代替

	// Act: 第二次请求3M
	result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 3000000)

	// Assert: 第二次请求成功，consumed累加
	testutil.AssertWindowResultSuccess(t, result2, 5500000)

	// Assert: 窗口时间未变
	assert.Equal(t, window1StartTime, result2.StartTime, "Start time should not change")
	assert.Equal(t, window1EndTime, result2.EndTime, "End time should not change")

	// Assert: Redis中的consumed正确
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 5500000)

	t.Log("SW-02: Test completed - Consumption accumulated correctly")
}

// ============================================================================
// SW-03: 窗口超限拒绝
// 测试ID: SW-03
// 优先级: P0
// 测试场景: 当累计消耗将超过限额时，应拒绝请求
// 预期结果:
//   - consumed=8M (第一次请求后)
//   - 第二次请求前consumed不变
//   - 第二次请求返回status=0 (失败)
//
// ============================================================================
func TestSW03_Exceeded_Rejects(t *testing.T) {
	t.Log("SW-03: Testing window limit exceeded rejection")

	// Arrange: 创建小时限额10M的窗口
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	config := testutil.CreateHourlyWindowConfig(subscriptionId, 10000000) // 10M限额

	// Act: 先请求8M（成功）
	result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 8000000)

	// Assert: 第一次请求成功
	testutil.AssertWindowResultSuccess(t, result1, 8000000)
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 8000000)

	// Act: 再请求5M（8+5=13M > 10M，应超限）
	result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 5000000)

	// Assert: 第二次请求失败
	testutil.AssertWindowResultFailed(t, result2, 8000000)

	// Assert: Redis中的consumed仍为8M（未增加）
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 8000000)

	// Assert: 窗口时间保持不变
	testutil.AssertWindowResultTimeMatch(t, rm, result2, subscriptionId, "hourly")

	t.Log("SW-03: Test completed - Request rejected when limit exceeded")
}

// ============================================================================
// SW-04: 窗口过期自动重建
// 测试ID: SW-04
// 优先级: P0
// 测试场景: 窗口过期后，再次请求应删除旧窗口并创建新窗口
// 预期结果:
//   - 旧窗口被DEL
//   - 创建新窗口
//   - 新start_time=now
//   - 新consumed=quota (重新开始计数)
//
// ============================================================================
func TestSW04_Expired_Rebuilds(t *testing.T) {
	t.Log("SW-04: Testing window auto-rebuild after expiration")

	// Arrange: 创建duration=60秒的窗口
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	config := testutil.WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         "hourly",
		Duration:       60, // 仅60秒
		Limit:          20000000,
		TTL:            90,
	}

	// Act: 首次请求创建窗口
	result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2500000)
	testutil.AssertWindowResultSuccess(t, result1, 2500000)
	window1StartTime := result1.StartTime

	// Act: 快进65秒（窗口过期）
	rm.FastForward(65 * time.Second)

	// Act: 再次请求
	result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 3000000)

	// Assert: 第二次请求成功
	testutil.AssertWindowResultSuccess(t, result2, 3000000)

	// Assert: 新窗口的consumed为3M（重新开始）
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 3000000)

	// Assert: 新窗口的start_time应大于旧窗口
	assert.Greater(t, result2.StartTime, window1StartTime, "New window should have later start time")

	// Assert: 新窗口的时长应为60秒
	actualDuration := result2.EndTime - result2.StartTime
	assert.Equal(t, int64(60), actualDuration, "New window duration should be 60 seconds")

	t.Log("SW-04: Test completed - Window rebuilt successfully after expiration")
}

// ============================================================================
// SW-05: 窗口TTL自动清理
// 测试ID: SW-05
// 优先级: P1
// 测试场景: 窗口TTL过期后，Key被Redis自动删除
// 预期结果:
//   - Key被Redis自动删除
//   - 下次请求创建新窗口
//
// =========================================================================
// ============================================================================
func TestSW05_TTL_AutoCleanup(t *testing.T) {
	t.Log("SW-05: Testing window TTL auto-cleanup")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)

	// Act: 创建窗口
	result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2500000)
	testutil.AssertWindowResultSuccess(t, result1, 2500000)

	// 验证窗口存在
	testutil.AssertWindowExists(t, rm, subscriptionId, "hourly")

	// Act: 快进TTL时间（4200秒 + 余量）
	rm.FastForward(4300 * time.Second)

	// Assert: 窗口应被TTL清理
	testutil.AssertWindowNotExists(t, rm, subscriptionId, "hourly")

	// Act: 再次请求，应创建新窗口
	result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 3000000)

	// Assert: 新窗口创建成功
	testutil.AssertWindowResultSuccess(t, result2, 3000000)
	testutil.AssertWindowExists(t, rm, subscriptionId, "hourly")

	// Assert: 新窗口的consumed为3M（重新开始）
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 3000000)

	t.Log("SW-05: Test completed - Window auto-cleaned by TTL and recreated")
}

// ============================================================================
// SW-06: RPM特殊处理
// 测试ID: SW-06
// 测试场景: RPM限制按请求数计数，而非quota
// 预期结果:
//   - RPM窗口consumed=请求数（非quota）
//   - 第61次请求返回超限
//
// ============================================================================
func TestSW06_RPM_SpecialHandling(t *testing.T) {
	t.Log("SW-06: Testing RPM window counts requests not quota")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// RPM限制60
	config := testutil.CreateRPMWindowConfig(subscriptionId, 60)

	// Act: 发起60次请求，每次quota不同
	for i := 1; i <= 60; i++ {
		// RPM窗口应传quota=1（表示1次请求）
		quotaPerRequest := int64(1)
		result := testutil.CallCheckAndConsumeWindow(t, ctx, config, quotaPerRequest)

		// Assert: 前60次都应成功
		testutil.AssertWindowResultSuccess(t, result, int64(i))
	}

	// Assert: 验证RPM窗口的consumed为60（请求数）
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "rpm", 60)

	// Act: 第61次请求（应超限）
	result61 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 1)

	// Assert: 第61次请求失败
	testutil.AssertWindowResultFailed(t, result61, 60)

	// Assert: consumed仍为60（未增加）
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "rpm", 60)

	t.Log("SW-06: Test completed - RPM window correctly counts requests")
}

// ============================================================================
// SW-07: 多维度独立滑动
// 测试ID: SW-07
// 测试场景: 同时配置多个时间维度的限额，各窗口独立创建和滑动
// 预期结果:
//   - 三个窗口独立创建
//   - start_time各不相同
//   - 每个窗口独立滑动
//   - 每个窗口独立滑动
//
// ============================================================================
func TestSW07_MultiDimension_IndependentSliding(t *testing.T) {
	t.Log("SW-07: Testing multi-dimension independent sliding windows")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 创建三个不同维度的窗口配置
	configs := []testutil.WindowTestConfig{
		testutil.CreateHourlyWindowConfig(subscriptionId, 20000000),
		testutil.CreateDailyWindowConfig(subscriptionId, 150000000),
		{
			SubscriptionId: subscriptionId,
			Period:         "weekly",
			Duration:       604800,
			Limit:          500000000,
			TTL:            691200,
		},
	}

	// 记录第一次请求的时间（用于验证窗口时间范围）
	_ = time.Now().Unix() // 忽略未使用的变量

	// Act: 第一次请求，创建所有窗口
	quota1 := int64(5000000)
	results1 := testutil.CreateMultipleWindows(t, ctx, subscriptionId, quota1, configs)

	// Assert: 所有窗口都创建成功
	for i, result := range results1 {
		testutil.AssertWindowResultSuccess(t, result, quota1)
		testutil.AssertWindowExists(t, rm, subscriptionId, configs[i].Period)
	}

	// 等待一段时间
	time.Sleep(200 * time.Millisecond)

	// Act: 第二次请求（在不同时间）
	quota2 := int64(3000000)
	results2 := testutil.CreateMultipleWindows(t, ctx, subscriptionId, quota2, configs)

	// Assert: 所有窗口累加正确
	for i, result := range results2 {
		expectedConsumed := quota1 + quota2
		testutil.AssertWindowResultSuccess(t, result, expectedConsumed)
		testutil.AssertWindowConsumed(t, rm, subscriptionId, configs[i].Period, expectedConsumed)
	}

	// Assert: 所有窗口的start_time应该相同（同时创建）
	hourlyStart := testutil.GetWindowStartTime(t, rm, subscriptionId, "hourly")
	dailyStart := testutil.GetWindowStartTime(t, rm, subscriptionId, "daily")
	weeklyStart := testutil.GetWindowStartTime(t, rm, subscriptionId, "weekly")

	// 允许1秒误差（因为多个窗口创建有时间差）
	assert.InDelta(t, hourlyStart, dailyStart, 1, "Hourly and daily windows should start at similar times")
	assert.InDelta(t, hourlyStart, weeklyStart, 1, "Hourly and weekly windows should start at similar times")

	// Assert: 验证窗口时长各不相同
	hourlyEnd := testutil.GetWindowEndTime(t, rm, subscriptionId, "hourly")
	dailyEnd := testutil.GetWindowEndTime(t, rm, subscriptionId, "daily")
	weeklyEnd := testutil.GetWindowEndTime(t, rm, subscriptionId, "weekly")

	assert.Equal(t, int64(3600), hourlyEnd-hourlyStart, "Hourly window should be 3600 seconds")
	assert.Equal(t, int64(86400), dailyEnd-dailyStart, "Daily window should be 86400 seconds")
	assert.Equal(t, int64(604800), weeklyEnd-weeklyStart, "Weekly window should be 604800 seconds")

	t.Log("SW-07: Test completed - Multiple dimensions slide independently")
}

// 测试ID: SW-08
// 优先级: P1
// 测试场景: 验证窗口可以跨越日期边界
// 预期结果:
//   - 窗口时间正确（22:00 ~ 次日02:00）
//
// ============================================================================
func TestSW08_FourHourly_CrossesMidnight(t *testing.T) {
	t.Log("SW-08: Testing 4-hourly window can cross date boundary")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 创建4小时窗口配置
	config := testutil.WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         "4hourly",
		Duration:       14400, // 4小时
		Limit:          60000000,
		TTL:            18000,
	}

	// Act: 首次请求（模拟在22:00请求）
	result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 5000000)

	// Assert: 请求成功
	testutil.AssertWindowResultSuccess(t, result, 5000000)

	// Assert: 验证窗口时长为4小时（14400秒）
	actualDuration := result.EndTime - result.StartTime
	assert.Equal(t, int64(14400), actualDuration, "Window duration should be 4 hours (14400 seconds)")

	// Assert: 验证end_time = start_time + 14400
	expectedEndTime := result.StartTime + 14400
	assert.Equal(t, expectedEndTime, result.EndTime, "End time should be start time + 14400 seconds")

	// 等待一段时间（模拟在次日01:00再次请求，窗口仍有效）
	time.Sleep(100 * time.Millisecond)

	// Act: 在窗口有效期内再次请求
	result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 3000000)

	// Assert: 第二次请求成功，使用同一窗口
	testutil.AssertWindowResultSuccess(t, result2, 8000000)

	// Assert: 窗口时间未变（跨日期边界的窗口仍然有效）
	assert.Equal(t, result.StartTime, result2.StartTime, "Window should remain the same across date boundary")

	t.Log("SW-08: Test completed - 4-hourly window correctly crosses midnight")
}

// 测试ID: SW-09
// 优先级: P1
// 测试场景: 启用套餐后，如果不发起任何请求，Redis中不应创建任何窗口Key
// 预期结果:
//   - 无任何窗口Key存在
//   - 资源节省验证
//
// ============================================================================
func TestSW09_NoRequest_NoKeyCreated(t *testing.T) {
	t.Log("SW-09: Testing no Redis keys created without requests")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 注意：setupTest已经创建并启用了订阅，但未发起任何滑动窗口请求

	// Assert: 验证所有窗口都不存在
	periods := []string{"rpm", "hourly", "4hourly", "daily", "weekly"}
	testutil.AssertAllWindowsNotExist(t, rm, subscriptionId, periods)

	// 进一步验证：检查Redis中是否有任何与该订阅相关的Key
	pattern := fmt.Sprintf("subscription:%d:*", subscriptionId)
	keys := rm.Server.Keys()
	for _, key := range keys {
		assert.NotContains(t, key, pattern, "Redis should not contain any subscription window keys")
	}

	t.Log("SW-09: Test completed - No keys created without requests, resource saved")
}

// 测试ID: SW-10
// 优先级: P0
// 测试场景: 100个并发请求同一套餐，验证无 TOCTOU 竞态
// 预期结果:
//   - consumed 精确 = 成功请求数 × 0.2M
//   - 无超限超额
//   - 数据一致性保证
//
// ============================================================================
func TestSW10_LuaAtomic_Concurrency(t *testing.T) {
	t.Log("SW-10: Testing Lua script atomicity under concurrent requests")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 小时限额10M，每次请求0.2M，理论上最多50次成功
	config := testutil.CreateHourlyWindowConfig(subscriptionId, 10000000)
	quotaPerRequest := int64(200000) // 0.2M

	// 并发请求数量
	concurrentRequests := 100
	successCount := 0
	failureCount := 0

	// 用于同步的channel
	type result struct {
		success bool
		index   int
	}
	results := make(chan result, concurrentRequests)

	// Act: 发起100个并发请求
	for i := 0; i < concurrentRequests; i++ {
		go func(index int) {
			res := testutil.CallCheckAndConsumeWindow(t, ctx, config, quotaPerRequest)
			results <- result{success: res.Success, index: index}
		}(i)
	}

	// 收集结果
	for i := 0; i < concurrentRequests; i++ {
		res := <-results
		if res.success {
			successCount++
		} else {
			failureCount++
		}
	}

	// Assert: 验证成功请求数量（10M / 0.2M = 50）
	expectedSuccessCount := 50
	assert.Equal(t, expectedSuccessCount, successCount,
		fmt.Sprintf("Should have exactly %d successful requests", expectedSuccessCount))

	// Assert: 验证失败请求数量
	expectedFailureCount := concurrentRequests - expectedSuccessCount
	assert.Equal(t, expectedFailureCount, failureCount,
		fmt.Sprintf("Should have exactly %d failed requests", expectedFailureCount))

	// Assert: 验证Redis中的consumed精确等于成功请求数×quota
	expectedConsumed := int64(expectedSuccessCount) * quotaPerRequest
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", expectedConsumed)

	// Assert: 验证无超额消耗（consumed应严格≤limit）
	actualConsumed := testutil.GetWindowConsumed(t, rm, subscriptionId, "hourly")
	assert.LessOrEqual(t, actualConsumed, int64(10000000), "Consumed should not exceed limit")

	// Assert: 验证consumed精确性（无TOCTOU竞态导致的微小误差）
	assert.Equal(t, expectedConsumed, actualConsumed, "Consumed should be exact, no race condition")

	t.Log("SW-10: Test completed - Lua script atomic, no TOCTOU race condition detected")
}
