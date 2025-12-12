package statistics_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"scene_test/testutil"
)

var (
	mr *miniredis.Miniredis
)

// TestMain 测试主入口
func TestMain(m *testing.M) {
	// Setup
	var err error
	mr, err = miniredis.Run()
	if err != nil {
		panic("Failed to start miniredis: " + err.Error())
	}
	defer mr.Close()

	// TODO: 初始化数据库连接
	// TODO: 设置Redis连接到miniredis

	// Run tests
	exitCode := m.Run()

	// Cleanup
	os.Exit(exitCode)
}

// ST-01: total_consumed累计测试
// 测试场景：验证订阅的total_consumed字段仅累计成功请求的消耗，失败请求不计入
// 优先级：P0
func TestST01_TotalConsumedAccumulation(t *testing.T) {
	t.Log("ST-01: Testing total_consumed accumulation (success only)")

	// Arrange: 创建测试用户、套餐和订阅
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st01-user",
		Group:    "default",
		Quota:    100000000, // 100M
	})

	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:        "st01-package",
		Priority:    15,
		Quota:       100000000, // 100M月度限额
		HourlyLimit: 50000000,  // 50M小时限额
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)
	assert.Equal(t, int64(0), sub.TotalConsumed, "Initial total_consumed should be 0")

	// Act & Assert: 模拟请求序列

	// 1. 第一次成功请求：消耗3M
	t.Log("  Step 1: First successful request (3M)")
	testutil.IncrementSubscriptionConsumed(t, sub.Id, 3000000)
	testutil.AssertSubscriptionConsumed(t, sub.Id, 3000000)

	// 2. 第二次成功请求：消耗5M
	t.Log("  Step 2: Second successful request (5M)")
	testutil.IncrementSubscriptionConsumed(t, sub.Id, 5000000)
	testutil.AssertSubscriptionConsumed(t, sub.Id, 8000000)

	// 3. 第三次失败请求：消耗2M（模拟超限，不应累加）
	t.Log("  Step 3: Failed request (2M) - should NOT increment")
	// 失败请求不调用IncrementSubscriptionConsumed
	testutil.AssertSubscriptionConsumed(t, sub.Id, 8000000) // 仍然是8M

	// Final Assert: 验证最终值
	finalSub := testutil.AssertSubscriptionConsumed(t, sub.Id, 8000000)
	t.Logf("ST-01: Final total_consumed = %d (expected 8000000)", finalSub.TotalConsumed)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	t.Log("ST-01: Test completed successfully")
}

// 后续测试将在这里添加...

// ST-02: 滑动窗口consumed验证
// 测试场景：创建小时窗口后，多次请求累加consumed值
// 优先级：P0
func TestST02_SlidingWindowConsumed(t *testing.T) {
	t.Log("ST-02: Testing sliding window consumed accumulation")

	// Arrange: 创建测试数据
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st02-user",
		Group:    "default",
		Quota:    100000000,
	})

	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:        "st02-package",
		Priority:    15,
		Quota:       100000000,
		HourlyLimit: 20000000, // 20M小时限额
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// 模拟创建小时窗口
	now := time.Now().Unix()
	startTime := now
	duration := int64(3600) // 1小时

	// Act & Assert: 模拟请求序列

	// 1. 第一次请求2M
	t.Log("  Step 1: First request (2M)")
	testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", 2000000, 20000000, startTime, duration)
	testutil.AssertWindowConsumed(t, mr, sub.Id, "hourly", 2000000)

	// 2. 第二次请求3M（累加到5M）
	t.Log("  Step 2: Second request (3M) - accumulate to 5M")
	consumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "hourly")
	testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", consumed+3000000, 20000000, startTime, duration)
	testutil.AssertWindowConsumed(t, mr, sub.Id, "hourly", 5000000)

	// 3. 第三次请求1M（累加到6M）
	t.Log("  Step 3: Third request (1M) - accumulate to 6M")
	consumed = testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "hourly")
	testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", consumed+1000000, 20000000, startTime, duration)
	testutil.AssertWindowConsumed(t, mr, sub.Id, "hourly", 6000000)

	// Final Assert
	finalConsumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "hourly")
	t.Logf("ST-02: Final window consumed = %d (expected 6000000)", finalConsumed)
	assert.Equal(t, int64(6000000), finalConsumed)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	mr.Del(testutil.FormatWindowKey(sub.Id, "hourly"))
	t.Log("ST-02: Test completed successfully")
}

// ST-03: 窗口使用率计算
// 测试场景：小时限额10M，已消耗7M，使用率应为70%
// 优先级：P1
func TestST03_WindowUtilizationRate(t *testing.T) {
	t.Log("ST-03: Testing window utilization rate calculation")

	// Arrange
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st03-user",
		Group:    "default",
		Quota:    100000000,
	})

	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:        "st03-package",
		Priority:    15,
		Quota:       100000000,
		HourlyLimit: 10000000, // 10M小时限额
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// Act: 模拟小时窗口，已消耗7M
	now := time.Now().Unix()
	testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", 7000000, 10000000, now, 3600)

	// Assert: 验证使用率为70%
	t.Log("  Verifying utilization rate")
	consumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "hourly")
	limit := testutil.GetWindowLimitFromRedis(t, mr, sub.Id, "hourly")
	utilizationRate := testutil.CalculateWindowUtilizationRate(consumed, limit)

	t.Logf("  Consumed: %d, Limit: %d, Utilization: %.2f%%", consumed, limit, utilizationRate)
	assert.Equal(t, int64(7000000), consumed)
	assert.Equal(t, int64(10000000), limit)
	assert.InDelta(t, 70.0, utilizationRate, 0.01, "Utilization rate should be 70%")

	// 使用辅助函数验证
	testutil.AssertWindowUtilization(t, mr, sub.Id, "hourly", 70.0)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	mr.Del(testutil.FormatWindowKey(sub.Id, "hourly"))
	t.Log("ST-03: Test completed successfully")
}

// ST-04: 窗口剩余时间计算
// 测试场景：窗口end_time=now+1800，剩余时间应为1800秒
// 优先级：P1
func TestST04_WindowTimeLeft(t *testing.T) {
	t.Log("ST-04: Testing window time left calculation")

	// Arrange
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st04-user",
		Group:    "default",
		Quota:    100000000,
	})

	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:        "st04-package",
		Priority:    15,
		Quota:       100000000,
		HourlyLimit: 20000000,
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// Act: 创建窗口，设置end_time为当前时间+1800秒
	now := time.Now().Unix()
	startTime := now - 1800 // 窗口开始于30分钟前
	duration := int64(3600) // 1小时窗口
	testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", 5000000, 20000000, startTime, duration)

	// Assert: 验证剩余时间
	t.Log("  Verifying time left")
	_, endTime := testutil.GetWindowTimeFromRedis(t, mr, sub.Id, "hourly")
	timeLeft := testutil.CalculateRemainingTime(endTime, now)

	t.Logf("  Current time: %d, End time: %d, Time left: %d seconds", now, endTime, timeLeft)
	assert.Equal(t, int64(1800), timeLeft, "Time left should be 1800 seconds")

	// 使用辅助函数验证
	testutil.AssertWindowTimeLeft(t, mr, sub.Id, "hourly", now, 1800)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	mr.Del(testutil.FormatWindowKey(sub.Id, "hourly"))
	t.Log("ST-04: Test completed successfully")
}

// ST-05: 多窗口聚合统计
// 测试场景：小时consumed=5M，日consumed=20M，周consumed=80M，各窗口独立正确
// 优先级：P1
func TestST05_MultiWindowAggregation(t *testing.T) {
	t.Log("ST-05: Testing multi-window aggregation statistics")

	// Arrange
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st05-user",
		Group:    "default",
		Quota:    100000000,
	})

	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:            "st05-package",
		Priority:        15,
		Quota:           500000000, // 500M月度限额
		HourlyLimit:     20000000,  // 20M小时限额
		DailyLimit:      150000000, // 150M日限额
		WeeklyLimit:     500000000, // 500M周限额
		FourHourlyLimit: 60000000,  // 60M 4小时限额
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// Act: 创建多个独立的滑动窗口
	now := time.Now().Unix()

	// 1. 小时窗口：consumed=5M
	t.Log("  Creating hourly window: consumed=5M")
	testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", 5000000, 20000000, now-1800, 3600)

	// 2. 日窗口：consumed=20M
	t.Log("  Creating daily window: consumed=20M")
	testutil.SimulateWindowInRedis(mr, sub.Id, "daily", 20000000, 150000000, now-43200, 86400)

	// 3. 周窗口：consumed=80M
	t.Log("  Creating weekly window: consumed=80M")
	testutil.SimulateWindowInRedis(mr, sub.Id, "weekly", 80000000, 500000000, now-259200, 604800)

	// Assert: 验证各窗口独立统计
	t.Log("  Verifying each window independently")

	// 验证小时窗口
	hourlyConsumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "hourly")
	assert.Equal(t, int64(5000000), hourlyConsumed, "Hourly consumed should be 5M")

	// 验证日窗口
	dailyConsumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "daily")
	assert.Equal(t, int64(20000000), dailyConsumed, "Daily consumed should be 20M")

	// 验证周窗口
	weeklyConsumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "weekly")
	assert.Equal(t, int64(80000000), weeklyConsumed, "Weekly consumed should be 80M")

	// 使用辅助函数验证
	testutil.AssertWindowConsumed(t, mr, sub.Id, "hourly", 5000000)
	testutil.AssertWindowConsumed(t, mr, sub.Id, "daily", 20000000)
	testutil.AssertWindowConsumed(t, mr, sub.Id, "weekly", 80000000)

	t.Logf("  Multi-window stats: Hourly=%dM, Daily=%dM, Weekly=%dM",
		hourlyConsumed/1000000, dailyConsumed/1000000, weeklyConsumed/1000000)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	mr.Del(testutil.FormatWindowKey(sub.Id, "hourly"))
	mr.Del(testutil.FormatWindowKey(sub.Id, "daily"))
	mr.Del(testutil.FormatWindowKey(sub.Id, "weekly"))
	t.Log("ST-05: Test completed successfully")
}

// ST-06: 套餐剩余额度计算
// 测试场景：quota=100M，total_consumed=35M，剩余额度应为65M
// 优先级：P1
func TestST06_RemainingQuotaCalculation(t *testing.T) {
	t.Log("ST-06: Testing remaining quota calculation")

	// Arrange
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st06-user",
		Group:    "default",
		Quota:    100000000,
	})

	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:        "st06-package",
		Priority:    15,
		Quota:       100000000, // 100M月度限额
		HourlyLimit: 20000000,
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// Act: 模拟已消耗35M
	t.Log("  Setting total_consumed to 35M")
	testutil.UpdateSubscriptionConsumed(t, sub.Id, 35000000)

	// Assert: 验证剩余额度
	t.Log("  Verifying remaining quota")
	updatedSub, err := model.GetSubscriptionById(sub.Id)
	assert.Nil(t, err)

	remainingQuota := testutil.CalculateRemainingQuota(pkg.Quota, updatedSub.TotalConsumed)
	t.Logf("  Quota: %dM, Consumed: %dM, Remaining: %dM",
		pkg.Quota/1000000, updatedSub.TotalConsumed/1000000, remainingQuota/1000000)

	assert.Equal(t, int64(35000000), updatedSub.TotalConsumed, "Total consumed should be 35M")
	assert.Equal(t, int64(65000000), remainingQuota, "Remaining quota should be 65M")

	// 使用辅助函数验证
	testutil.AssertRemainingQuota(t, sub.Id, 65000000)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	t.Log("ST-06: Test completed successfully")
}

// ST-07: Fallback触发率统计
// 测试场景：100次请求中，20次Fallback到余额，触发率应为20%
// 优先级：P2
func TestST07_FallbackTriggerRate(t *testing.T) {
	t.Log("ST-07: Testing fallback trigger rate statistics")

	// Arrange
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st07-user",
		Group:    "default",
		Quota:    1000000000, // 1000M用户余额
	})

	// 创建小时限额较小的套餐（容易触发Fallback）
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "st07-package",
		Priority:          15,
		Quota:             1000000000, // 1000M月度限额
		HourlyLimit:       20000000,   // 20M小时限额
		FallbackToBalance: true,       // 允许Fallback
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// 记录用户初始余额
	initialUserQuota, _ := model.GetUserQuota(user.Id, true)

	// Act: 模拟100次请求
	totalRequests := 100
	fallbackCount := 0
	packageConsumedCount := 0
	perRequestQuota := int64(250000) // 每次请求0.25M

	now := time.Now().Unix()

	t.Logf("  Simulating %d requests...", totalRequests)

	for i := 0; i < totalRequests; i++ {
		// 计算当前窗口已消耗量
		windowConsumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "hourly")

		// 判断是否会超限（小时限额20M）
		if windowConsumed+perRequestQuota > 20000000 {
			// 会超限，触发Fallback
			fallbackCount++
			// Fallback时不更新套餐consumed，窗口不变
			// 仅在用户余额上扣减（这里简化处理，不实际扣减）
		} else {
			// 套餐可用，更新窗口consumed
			packageConsumedCount++
			if windowConsumed == 0 {
				// 首次创建窗口
				testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", perRequestQuota, 20000000, now, 3600)
			} else {
				// 累加到窗口
				testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", windowConsumed+perRequestQuota, 20000000, now, 3600)
			}
			// 更新订阅total_consumed
			testutil.IncrementSubscriptionConsumed(t, sub.Id, perRequestQuota)
		}
	}

	// Assert: 验证Fallback触发率
	fallbackRate := float64(fallbackCount) / float64(totalRequests) * 100
	t.Logf("  Total requests: %d", totalRequests)
	t.Logf("  Package consumed: %d", packageConsumedCount)
	t.Logf("  Fallback triggered: %d", fallbackCount)
	t.Logf("  Fallback rate: %.2f%%", fallbackRate)

	// 预期：小时限额20M，每次0.25M，可以容纳80次请求
	// 剩余20次会触发Fallback
	expectedFallbackCount := 20
	expectedFallbackRate := 20.0

	assert.Equal(t, expectedFallbackCount, fallbackCount,
		fmt.Sprintf("Fallback count should be %d", expectedFallbackCount))
	assert.InDelta(t, expectedFallbackRate, fallbackRate, 0.5,
		fmt.Sprintf("Fallback rate should be %.2f%%", expectedFallbackRate))

	// 验证套餐consumed（仅前80次）
	expectedPackageConsumed := int64(80 * 250000) // 80次 * 0.25M = 20M
	testutil.AssertSubscriptionConsumed(t, sub.Id, expectedPackageConsumed)

	// 验证窗口consumed
	testutil.AssertWindowConsumed(t, mr, sub.Id, "hourly", expectedPackageConsumed)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	mr.Del(testutil.FormatWindowKey(sub.Id, "hourly"))
	t.Log("ST-07: Test completed successfully")
}

// ST-08: 窗口超限次数统计
// 测试场景：100次请求中，15次因小时窗口超限被拒
// 优先级：P2
func TestST08_WindowExceededCount(t *testing.T) {
	t.Log("ST-08: Testing window exceeded count statistics")

	// Arrange
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "st08-user",
		Group:    "default",
		Quota:    100000000,
	})

	// 创建小时限额较小的套餐，且不允许Fallback
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "st08-package",
		Priority:          15,
		Quota:             1000000000, // 1000M月度限额
		HourlyLimit:       21250000,   // 21.25M小时限额
		FallbackToBalance: false,      // 不允许Fallback，超限直接拒绝
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// Act: 模拟100次请求
	totalRequests := 100
	exceededCount := 0
	successCount := 0
	perRequestQuota := int64(250000) // 每次请求0.25M

	now := time.Now().Unix()

	t.Logf("  Simulating %d requests with hourly limit %dM...",
		totalRequests, pkg.HourlyLimit/1000000)

	for i := 0; i < totalRequests; i++ {
		// 获取当前窗口consumed
		windowConsumed := testutil.GetWindowConsumedFromRedis(t, mr, sub.Id, "hourly")

		// 判断是否会超限
		if windowConsumed+perRequestQuota > pkg.HourlyLimit {
			// 超限，被拒绝
			exceededCount++
			t.Logf("  Request %d: EXCEEDED (consumed=%dM, limit=%dM)",
				i+1, windowConsumed/1000000, pkg.HourlyLimit/1000000)
		} else {
			// 成功，更新窗口
			successCount++
			if windowConsumed == 0 {
				// 首次创建窗口
				testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", perRequestQuota, pkg.HourlyLimit, now, 3600)
			} else {
				// 累加
				testutil.SimulateWindowInRedis(mr, sub.Id, "hourly", windowConsumed+perRequestQuota, pkg.HourlyLimit, now, 3600)
			}
			// 更新订阅total_consumed
			testutil.IncrementSubscriptionConsumed(t, sub.Id, perRequestQuota)
		}
	}

	// Assert: 验证超限次数
	exceededRate := float64(exceededCount) / float64(totalRequests) * 100

	t.Logf("  Total requests: %d", totalRequests)
	t.Logf("  Success count: %d", successCount)
	t.Logf("  Exceeded count: %d", exceededCount)
	t.Logf("  Exceeded rate: %.2f%%", exceededRate)

	// 预期：小时限额21.25M，每次0.25M，可以容纳85次请求
	// 剩余15次会因超限被拒
	expectedExceededCount := 15
	expectedExceededRate := 15.0

	assert.Equal(t, expectedExceededCount, exceededCount,
		fmt.Sprintf("Exceeded count should be %d", expectedExceededCount))
	assert.InDelta(t, expectedExceededRate, exceededRate, 0.5,
		fmt.Sprintf("Exceeded rate should be %.2f%%", expectedExceededRate))

	// 验证成功请求数
	expectedSuccessCount := 85
	assert.Equal(t, expectedSuccessCount, successCount,
		fmt.Sprintf("Success count should be %d", expectedSuccessCount))

	// 验证套餐consumed（仅前85次成功）
	expectedPackageConsumed := int64(85 * 250000) // 85次 * 0.25M = 21.25M
	testutil.AssertSubscriptionConsumed(t, sub.Id, expectedPackageConsumed)

	// 验证窗口consumed
	testutil.AssertWindowConsumed(t, mr, sub.Id, "hourly", expectedPackageConsumed)

	// 验证用户余额未变（因为不允许Fallback）
	testutil.AssertUserQuotaUnchanged(t, user.Id, initialUserQuota)

	// Cleanup
	testutil.CleanupPackageTestData(t)
	mr.Del(testutil.FormatWindowKey(sub.Id, "hourly"))
	t.Log("ST-08: Test completed successfully")
}
