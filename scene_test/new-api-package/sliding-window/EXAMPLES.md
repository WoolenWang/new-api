# 滑动窗口测试使用示例

## 快速开始

### 1. 基础测试示例

```go
package example_test

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

func TestBasicSlidingWindow(t *testing.T) {
	// 1. 启动Redis Mock
	rm := testutil.StartRedisMock(t)
	defer rm.Close()

	// 2. 创建测试用户和套餐
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user",
		Quota:    10000000,
	})

	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:        "test-package",
		HourlyLimit: 20000000, // 20M小时限额
		Priority:    15,
	})

	// 3. 创建并启用订阅
	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// 4. 调用滑动窗口检查
	ctx := context.Background()
	config := testutil.CreateHourlyWindowConfig(sub.Id, 20000000)
	result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2500000)

	// 5. 验证结果
	testutil.AssertWindowResultSuccess(t, result, 2500000)
	testutil.AssertWindowExists(t, rm, sub.Id, "hourly")
}
```

### 2. 验证窗口超限

```go
func TestWindowExceeded(t *testing.T) {
	rm := testutil.StartRedisMock(t)
	defer rm.Close()

	// 创建限额10M的窗口
	config := testutil.CreateHourlyWindowConfig(subscriptionId, 10000000)

	// 先请求8M（成功）
	result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 8000000)
	testutil.AssertWindowResultSuccess(t, result1, 8000000)

	// 再请求5M（超限，应失败）
	result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 5000000)
	testutil.AssertWindowResultFailed(t, result2, 8000000) // consumed保持8M
}
```

### 3. 验证窗口过期重建

```go
func TestWindowExpiration(t *testing.T) {
	rm := testutil.StartRedisMock(t)
	defer rm.Close()

	// 创建60秒窗口
	config := testutil.WindowTestConfig{
		SubscriptionId: subscriptionId,
		Period:         "hourly",
		Duration:       60,
		Limit:          20000000,
		TTL:            90,
	}

	// 创建窗口
	result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2000000)
	window1Start := result1.StartTime

	// 快进65秒（窗口过期）
	rm.FastForward(65 * time.Second)

	// 再次请求，应创建新窗口
	result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 3000000)

	// 验证新窗口
	assert.Greater(t, result2.StartTime, window1Start)
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 3000000) // 重新开始
}
```

### 4. RPM特殊处理

```go
func TestRPMWindow(t *testing.T) {
	rm := testutil.StartRedisMock(t)
	defer rm.Close()

	// RPM限制60
	config := testutil.CreateRPMWindowConfig(subscriptionId, 60)

	// 发起60次请求（每次quota不同，但RPM只计数请求数）
	for i := 1; i <= 60; i++ {
		result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 1) // RPM传1
		testutil.AssertWindowResultSuccess(t, result, int64(i))
	}

	// 第61次应失败
	result61 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 1)
	testutil.AssertWindowResultFailed(t, result61, 60)
}
```

### 5. 多维度独立窗口

```go
func TestMultiDimensionWindows(t *testing.T) {
	rm := testutil.StartRedisMock(t)
	defer rm.Close()

	// 创建多个维度的窗口配置
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

	// 同时创建所有窗口
	quota := int64(5000000)
	results := testutil.CreateMultipleWindows(t, ctx, subscriptionId, quota, configs)

	// 验证所有窗口都创建成功
	for i, result := range results {
		testutil.AssertWindowResultSuccess(t, result, quota)
		testutil.AssertWindowExists(t, rm, subscriptionId, configs[i].Period)
	}
}
```

### 6. 并发原子性测试

```go
func TestConcurrentAtomicity(t *testing.T) {
	rm := testutil.StartRedisMock(t)
	defer rm.Close()

	// 限额10M，每次0.2M，理论成功50次
	config := testutil.CreateHourlyWindowConfig(subscriptionId, 10000000)
	quotaPerRequest := int64(200000)

	// 100个并发请求
	concurrentRequests := 100
	results := make(chan bool, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func() {
			result := testutil.CallCheckAndConsumeWindow(t, ctx, config, quotaPerRequest)
			results <- result.Success
		}()
	}

	// 统计成功数
	successCount := 0
	for i := 0; i < concurrentRequests; i++ {
		if <-results {
			successCount++
		}
	}

	// 验证：成功数应为50
	assert.Equal(t, 50, successCount)

	// 验证：consumed精确为10M
	testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 10000000)
}
```

## 调试技巧

### 打印窗口信息

```go
// 打印窗口详细信息
testutil.DumpWindowInfo(t, rm, subscriptionId, "hourly")

// 输出示例:
// Redis key 'subscription:123:hourly:window' (TTL: 1h10m):
//   start_time = 1702388310
//   end_time = 1702391910
//   consumed = 8500000
//   limit = 20000000
```

### 检查Redis所有Key

```go
keys := rm.Server.Keys()
for _, key := range keys {
	t.Logf("Redis Key: %s", key)
	if strings.Contains(key, "window") {
		testutil.DumpWindowInfo(t, rm, subscriptionId, extractPeriod(key))
	}
}
```

### 时间快进测试

```go
// 创建窗口
result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2000000)

// 快进到窗口即将过期（59秒后）
rm.FastForward(59 * time.Second)

// 请求应仍然成功（窗口有效）
result2 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 1000000)
assert.True(t, result2.Success)

// 再快进2秒（窗口过期）
rm.FastForward(2 * time.Second)

// 请求应创建新窗口
result3 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 3000000)
assert.Equal(t, int64(3000000), result3.Consumed) // 新窗口从0开始
```

## 常见断言函数

### 窗口存在性
```go
testutil.AssertWindowExists(t, rm, subscriptionId, "hourly")
testutil.AssertWindowNotExists(t, rm, subscriptionId, "daily")
```

### 窗口值验证
```go
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 8500000)
testutil.AssertWindowLimit(t, rm, subscriptionId, "hourly", 20000000)
```

### 窗口时间验证
```go
// 验证窗口时长（允许1秒误差）
testutil.AssertWindowTimeRange(t, rm, subscriptionId, "hourly", 3600, 1)

// 验证TTL（允许10秒误差）
testutil.AssertWindowTTL(t, rm, subscriptionId, "hourly", 4200*time.Second, 10*time.Second)
```

### 窗口结果验证
```go
// 验证操作成功
testutil.AssertWindowResultSuccess(t, result, 2500000)

// 验证操作失败（超限）
testutil.AssertWindowResultFailed(t, result, 8000000)

// 验证结果时间与Redis一致
testutil.AssertWindowResultTimeMatch(t, rm, result, subscriptionId, "hourly")
```

## 测试数据创建

### 创建用户
```go
user := testutil.CreateTestUser(t, testutil.UserTestData{
	Username: "test-user",
	Group:    "vip",
	Quota:    10000000,
	Role:     common.RoleCommonUser,
})
```

### 创建套餐
```go
pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
	Name:            "Premium Package",
	Priority:        15,
	Quota:           500000000,  // 月度总额
	HourlyLimit:     20000000,   // 小时限额
	DailyLimit:      150000000,  // 日限额
	WeeklyLimit:     500000000,  // 周限额
	RpmLimit:        60,         // RPM
	FallbackToBalance: true,
})
```

### 创建订阅
```go
// 方式1: 创建并手动启用
sub := testutil.CreateTestSubscription(t, testutil.SubscriptionTestData{
	UserId:    user.Id,
	PackageId: pkg.Id,
	Status:    model.SubscriptionStatusInventory,
})

// 方式2: 创建并自动启用
sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)
```

## 高级用法

### 自定义窗口配置

```go
customConfig := testutil.WindowTestConfig{
	SubscriptionId: subscriptionId,
	Period:         "custom_2hour",
	Duration:       7200,  // 2小时
	Limit:          40000000,
	TTL:            9000,
}

result := testutil.CallCheckAndConsumeWindow(t, ctx, customConfig, 5000000)
```

### 批量创建窗口

```go
configs := []testutil.WindowTestConfig{
	testutil.CreateHourlyWindowConfig(subscriptionId, 20000000),
	testutil.CreateDailyWindowConfig(subscriptionId, 150000000),
	testutil.CreateRPMWindowConfig(subscriptionId, 60),
}

results := testutil.CreateMultipleWindows(t, ctx, subscriptionId, 5000000, configs)
```

### 验证所有窗口不存在

```go
periods := []string{"rpm", "hourly", "4hourly", "daily", "weekly"}
testutil.AssertAllWindowsNotExist(t, rm, subscriptionId, periods)
```

## 错误处理示例

### Redis降级测试

```go
func TestRedisUnavailable(t *testing.T) {
	// 关闭Redis
	rm.Close()
	common.RedisEnabled = false

	// 请求应降级成功（跳过滑动窗口检查）
	result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2000000)
	assert.True(t, result.Success, "Should succeed with Redis unavailable")
}
```

### Lua脚本失败处理

```go
// Lua脚本返回异常格式时，应降级处理
// 系统会自动捕获异常并允许请求通过
```

## 性能测试示例

### 基准测试

```bash
go test -bench=BenchmarkSW10_SlidingWindow_Performance -benchmem

# 预期输出:
# BenchmarkSW10_SlidingWindow_Performance-8   50000   25000 ns/op   1200 B/op   15 allocs/op
```

### 压力测试

```bash
go test -v -run TestSW10_Concurrency_StressTest

# 验证1000个并发请求的数据一致性
```

## 故障模拟

### 模拟窗口过期

```go
// 创建窗口
result1 := testutil.CallCheckAndConsumeWindow(t, ctx, config, 1000000)

// 快进时间
rm.FastForward(3700 * time.Second) // 超过小时窗口

// 验证窗口过期
testutil.WaitAndCheckWindowExpired(t, rm, subscriptionId, "hourly", 0)
```

### 模拟TTL清理

```go
// 创建窗口
result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 1000000)

// 快进超过TTL
rm.FastForward(4300 * time.Second)

// 验证Key被清理
testutil.AssertWindowNotExists(t, rm, subscriptionId, "hourly")
```

## 常见测试模式

### AAA模式（Arrange-Act-Assert）

```go
func TestExample(t *testing.T) {
	// Arrange: 准备测试数据
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)
	config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)

	// Act: 执行操作
	result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2500000)

	// Assert: 验证结果
	testutil.AssertWindowResultSuccess(t, result, 2500000)
	testutil.AssertWindowExists(t, rm, subscriptionId, "hourly")
}
```

### Table-Driven测试

```go
func TestWindowLimits(t *testing.T) {
	testCases := []struct {
		name          string
		limit         int64
		requests      []int64  // 每次请求的quota
		expectedSuccess []bool // 每次请求是否成功
	}{
		{
			name:          "Under limit",
			limit:         10000000,
			requests:      []int64{3000000, 4000000, 2000000},
			expectedSuccess: []bool{true, true, true},
		},
		{
			name:          "Exceed limit",
			limit:         10000000,
			requests:      []int64{8000000, 5000000},
			expectedSuccess: []bool{true, false},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rm, subscriptionId := setupTest(t)
			defer teardownTest(rm)

			config := testutil.CreateHourlyWindowConfig(subscriptionId, tc.limit)

			for i, quota := range tc.requests {
				result := testutil.CallCheckAndConsumeWindow(t, ctx, config, quota)
				assert.Equal(t, tc.expectedSuccess[i], result.Success)
			}
		})
	}
}
```

## 最佳实践

### 1. 测试隔离
- ✅ 每个测试使用独立的Redis Mock
- ✅ 使用defer确保资源清理
- ✅ 不依赖测试执行顺序

### 2. 时间处理
- ✅ 使用`rm.FastForward()`而非`time.Sleep()`
- ✅ 允许合理的时间误差（±1秒）
- ✅ 记录关键时间戳用于调试

### 3. 并发测试
- ✅ 使用`sync/atomic`保证计数准确
- ✅ 使用`sync.WaitGroup`同步goroutine
- ✅ 验证最终一致性而非中间状态

### 4. 断言充分性
- ✅ 同时验证返回值和Redis状态
- ✅ 验证成功和失败两种路径
- ✅ 边界条件单独测试

## 参考资料

- [滑动窗口测试README](./README.md)
- [测试方案文档](../../../docs/NewAPI-支持多种包月套餐-优化版-测试方案.md)
- [设计文档](../../../docs/NewAPI-支持多种包月套餐-优化版.md)

---

**最后更新**: 2025-12-12
