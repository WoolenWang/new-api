package sliding_window_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
)

// ============================================================================
// SW-10: Lua脚本原子性 - 并发扩展测试
// 本文件提供更详细的并发场景测试，验证Lua脚本的原子性和一致性
// ============================================================================

// TestSW10_Concurrency_WindowCreation 测试并发场景下窗口创建的原子性
// 验证点: 100个goroutine同时首次请求，应只创建1个窗口
func TestSW10_Concurrency_WindowCreation(t *testing.T) {
	t.Log("SW-10 Extended: Testing concurrent window creation atomicity")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)
	quotaPerRequest := int64(100000)

	// Act: 100个goroutine同时首次请求（窗口不存在）
	concurrentRequests := 100
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	startTimes := make([]int64, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func(index int) {
			defer wg.Done()
			result := testutil.CallCheckAndConsumeWindow(t, context.Background(), config, quotaPerRequest)
			if result.Success {
				startTimes[index] = result.StartTime
			}
		}(i)
	}

	wg.Wait()

	// Assert: 所有成功请求的start_time应该相同（同一个窗口）
	var firstNonZeroStartTime int64
	for _, st := range startTimes {
		if st != 0 {
			firstNonZeroStartTime = st
			break
		}
	}

	allSame := true
	for _, st := range startTimes {
		if st != 0 && st != firstNonZeroStartTime {
			allSame = false
			break
		}
	}

	assert.True(t, allSame, "All requests should use the same window (same start_time)")

	// Assert: Redis中只有一个窗口Key
	testutil.AssertWindowExists(t, rm, subscriptionId, "hourly")

	t.Log("SW-10 Extended: Concurrent window creation is atomic")
}

// TestSW10_Concurrency_WindowExpiredRebuild 测试并发场景下窗口过期重建的原子性
// 验证点: 窗口过期时，100个并发请求应正确重建1个新窗口
func TestSW10_Concurrency_WindowExpiredRebuild(t *testing.T) {
	t.Log("SW-10 Extended: Testing concurrent window rebuild after expiration")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 创建短duration窗口（60秒）
	config := testutil.WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         "hourly",
		Duration:       60,
		Limit:          20000000,
		TTL:            90,
	}

	// 先创建一个窗口
	result1 := testutil.CallCheckAndConsumeWindow(t, context.Background(), config, 1000000)
	testutil.AssertWindowResultSuccess(t, result1, 1000000)
	oldStartTime := result1.StartTime

	// 快进65秒（窗口过期）
	rm.FastForward(65 * 1000000000) // 纳秒

	// Act: 100个goroutine同时请求（窗口已过期）
	concurrentRequests := 100
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	newStartTimes := make([]int64, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func(index int) {
			defer wg.Done()
			result := testutil.CallCheckAndConsumeWindow(t, context.Background(), config, 100000)
			if result.Success {
				newStartTimes[index] = result.StartTime
			}
		}(i)
	}

	wg.Wait()

	// Assert: 所有请求的start_time应该相同（同一个新窗口）
	var newWindowStartTime int64
	for _, st := range newStartTimes {
		if st != 0 {
			newWindowStartTime = st
			break
		}
	}

	allSame := true
	for _, st := range newStartTimes {
		if st != 0 && st != newWindowStartTime {
			allSame = false
			break
		}
	}

	assert.True(t, allSame, "All requests should use the same rebuilt window")

	// Assert: 新窗口的start_time应大于旧窗口
	assert.Greater(t, newWindowStartTime, oldStartTime, "New window should have later start time")

	t.Log("SW-10 Extended: Concurrent window rebuild is atomic")
}

// TestSW10_Concurrency_MixedOperations 测试混合并发操作
// 验证点: 在高并发场景下，成功和失败的请求数量准确，无数据不一致
func TestSW10_Concurrency_MixedOperations(t *testing.T) {
	t.Log("SW-10 Extended: Testing mixed concurrent operations with precise counting")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 小时限额10M，每次请求0.2M，理论上最多50次成功
	config := testutil.CreateHourlyWindowConfig(subscriptionId, 10000000)
	quotaPerRequest := int64(200000) // 0.2M

	// 并发请求数量
	concurrentRequests := 100

	// 使用atomic保证计数准确性
	var successCount int32
	var failureCount int32

	// Act: 发起100个并发请求
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func(index int) {
			defer wg.Done()
			result := testutil.CallCheckAndConsumeWindow(t, context.Background(), config, quotaPerRequest)
			if result.Success {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&failureCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Assert: 验证成功请求数量（10M / 0.2M = 50）
	expectedSuccessCount := int32(50)
	actualSuccessCount := atomic.LoadInt32(&successCount)
	assert.Equal(t, expectedSuccessCount, actualSuccessCount,
		fmt.Sprintf("Should have exactly %d successful requests, got %d", expectedSuccessCount, actualSuccessCount))

	// Assert: 验证失败请求数量
	expectedFailureCount := int32(concurrentRequests) - expectedSuccessCount
	actualFailureCount := atomic.LoadInt32(&failureCount)
	assert.Equal(t, expectedFailureCount, actualFailureCount,
		fmt.Sprintf("Should have exactly %d failed requests, got %d", expectedFailureCount, actualFailureCount))

	// Assert: 验证Redis中的consumed精确等于成功请求数×quota
	expectedConsumed := int64(expectedSuccessCount) * quotaPerRequest
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", expectedConsumed)

	// Assert: 验证无超额消耗（consumed应严格≤limit）
	actualConsumed := testutil.GetWindowConsumed(t, rm, subscriptionId, "hourly")
	assert.LessOrEqual(t, actualConsumed, int64(10000000), "Consumed should not exceed limit")

	// Assert: 验证consumed精确性（无TOCTOU竞态导致的误差）
	assert.Equal(t, expectedConsumed, actualConsumed,
		"Consumed should be exact (expected=%d, actual=%d), no race condition", expectedConsumed, actualConsumed)

	t.Logf("SW-10 Extended: Success=%d, Failure=%d, Consumed=%d, Expected=%d",
		actualSuccessCount, actualFailureCount, actualConsumed, expectedConsumed)
	t.Log("SW-10 Extended: Lua script atomicity verified under concurrent load")
}

// TestSW10_Concurrency_MultipleWindows 测试多窗口并发场景
// 验证点: 多个时间维度的窗口在并发场景下互不干扰
func TestSW10_Concurrency_MultipleWindows(t *testing.T) {
	t.Log("SW-10 Extended: Testing multiple windows under concurrent load")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 配置三个不同维度的窗口
	hourlyConfig := testutil.CreateHourlyWindowConfig(subscriptionId, 10000000) // 10M
	dailyConfig := testutil.CreateDailyWindowConfig(subscriptionId, 50000000)   // 50M
	rpmConfig := testutil.CreateRPMWindowConfig(subscriptionId, 100)            // 100 RPM

	// 并发请求数量
	concurrentRequests := 200
	quotaPerRequest := int64(100000) // 0.1M

	// 计数器
	var hourlySuccess, dailySuccess, rpmSuccess int32
	var hourlyFail, dailyFail, rpmFail int32

	// Act: 发起200个并发请求，每个请求检查所有三个窗口
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func(index int) {
			defer wg.Done()
			ctx := context.Background()

			// 检查小时窗口
			hourlyResult := testutil.CallCheckAndConsumeWindow(t, ctx, hourlyConfig, quotaPerRequest)
			if hourlyResult.Success {
				atomic.AddInt32(&hourlySuccess, 1)
			} else {
				atomic.AddInt32(&hourlyFail, 1)
			}

			// 检查日窗口
			dailyResult := testutil.CallCheckAndConsumeWindow(t, ctx, dailyConfig, quotaPerRequest)
			if dailyResult.Success {
				atomic.AddInt32(&dailySuccess, 1)
			} else {
				atomic.AddInt32(&dailyFail, 1)
			}

			// 检查RPM窗口（传1表示1次请求）
			rpmResult := testutil.CallCheckAndConsumeWindow(t, ctx, rpmConfig, 1)
			if rpmResult.Success {
				atomic.AddInt32(&rpmSuccess, 1)
			} else {
				atomic.AddInt32(&rpmFail, 1)
			}
		}(i)
	}

	wg.Wait()

	// Assert: 验证小时窗口（10M / 0.1M = 100次）
	assert.Equal(t, int32(100), atomic.LoadInt32(&hourlySuccess), "Hourly window should allow 100 requests")
	assert.Equal(t, int32(100), atomic.LoadInt32(&hourlyFail), "Hourly window should reject 100 requests")

	// Assert: 验证日窗口（50M / 0.1M = 500次，但总请求只有200次）
	assert.Equal(t, int32(200), atomic.LoadInt32(&dailySuccess), "Daily window should allow all 200 requests")
	assert.Equal(t, int32(0), atomic.LoadInt32(&dailyFail), "Daily window should reject 0 requests")

	// Assert: 验证RPM窗口（100 RPM）
	assert.Equal(t, int32(100), atomic.LoadInt32(&rpmSuccess), "RPM window should allow 100 requests")
	assert.Equal(t, int32(100), atomic.LoadInt32(&rpmFail), "RPM window should reject 100 requests")

	// Assert: 验证Redis中各窗口的consumed值
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 10000000) // 100 × 0.1M
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "daily", 20000000)  // 200 × 0.1M
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "rpm", 100)         // 100次请求

	t.Logf("SW-10 Extended: Hourly(S=%d,F=%d), Daily(S=%d,F=%d), RPM(S=%d,F=%d)",
		atomic.LoadInt32(&hourlySuccess), atomic.LoadInt32(&hourlyFail),
		atomic.LoadInt32(&dailySuccess), atomic.LoadInt32(&dailyFail),
		atomic.LoadInt32(&rpmSuccess), atomic.LoadInt32(&rpmFail))

	t.Log("SW-10 Extended: Multiple windows maintain independence under concurrent load")
}

// BenchmarkSW10_SlidingWindow_Performance 性能基准测试
// 验证点: Lua脚本执行性能应在可接受范围内
func BenchmarkSW10_SlidingWindow_Performance(b *testing.B) {
	// Setup (convert b to t for testutil functions)
	t := &testing.T{}
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	config := testutil.CreateHourlyWindowConfig(subscriptionId, 100000000000) // 超大限额
	quota := int64(100000)
	ctx := context.Background()

	// Reset timer
	b.ResetTimer()

	// Benchmark
	for i := 0; i < b.N; i++ {
		testutil.CallCheckAndConsumeWindow(t, ctx, config, quota)
	}

	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N), "ns/op")
}

// TestSW10_Concurrency_StressTest 压力测试
// 验证点: 在极高并发下（1000个请求），系统仍能保持数据一致性
func TestSW10_Concurrency_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Log("SW-10 Extended: Stress testing with 1000 concurrent requests")

	// Arrange
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// 限额20M，每次0.1M，理论上200次成功
	config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)
	quotaPerRequest := int64(100000) // 0.1M

	// 高并发请求
	concurrentRequests := 1000

	// 使用atomic计数
	var successCount int32
	var failureCount int32

	// Act
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func() {
			defer wg.Done()
			result := testutil.CallCheckAndConsumeWindow(t, context.Background(), config, quotaPerRequest)
			if result.Success {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&failureCount, 1)
			}
		}()
	}

	wg.Wait()

	// Assert
	expectedSuccess := int32(200)
	actualSuccess := atomic.LoadInt32(&successCount)
	actualFailure := atomic.LoadInt32(&failureCount)

	assert.Equal(t, expectedSuccess, actualSuccess,
		fmt.Sprintf("Expected %d successes, got %d", expectedSuccess, actualSuccess))
	assert.Equal(t, int32(concurrentRequests)-expectedSuccess, actualFailure,
		fmt.Sprintf("Expected %d failures, got %d", concurrentRequests-int(expectedSuccess), actualFailure))

	// Verify consumed
	expectedConsumed := int64(expectedSuccess) * quotaPerRequest
	actualConsumed := testutil.GetWindowConsumed(t, rm, subscriptionId, "hourly")
	assert.Equal(t, expectedConsumed, actualConsumed,
		"Consumed should be exact even under 1000 concurrent requests")

	t.Logf("SW-10 Stress Test: 1000 requests -> Success=%d, Failure=%d, Consumed=%d (Expected=%d)",
		actualSuccess, actualFailure, actualConsumed, expectedConsumed)
	t.Log("SW-10 Extended: System maintains consistency under high concurrent load")
}
