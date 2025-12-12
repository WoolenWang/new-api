# 滑动窗口测试快速参考

## 🚀 一分钟快速开始

```go
// 1. 导入
import "github.com/QuantumNous/new-api/scene_test/testutil"

// 2. 启动Redis Mock
rm := testutil.StartRedisMock(t)
defer rm.Close()

// 3. 创建测试配置
config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)

// 4. 调用滑动窗口
result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2500000)

// 5. 验证结果
testutil.AssertWindowResultSuccess(t, result, 2500000)
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 2500000)
```

## 📦 常用配置函数

```go
// 小时窗口（3600秒，4200秒TTL）
config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)

// RPM窗口（60秒，90秒TTL）
config := testutil.CreateRPMWindowConfig(subscriptionId, 60)

// 日窗口（86400秒，93600秒TTL）
config := testutil.CreateDailyWindowConfig(subscriptionId, 150000000)

// 自定义窗口
config := testutil.WindowTestConfig{
	SubscriptionId: subscriptionId,
	Period:         "custom",
	Duration:       7200,  // 2小时
	Limit:          40000000,
	TTL:            9000,
}
```

## ✅ 常用断言函数

### 窗口存在性
```go
testutil.AssertWindowExists(t, rm, subscriptionId, "hourly")
testutil.AssertWindowNotExists(t, rm, subscriptionId, "daily")
```

### 窗口值
```go
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 8500000)
testutil.AssertWindowLimit(t, rm, subscriptionId, "hourly", 20000000)
```

### 窗口结果
```go
// 成功
testutil.AssertWindowResultSuccess(t, result, 2500000)

// 失败
testutil.AssertWindowResultFailed(t, result, 8000000)

// 时间匹配
testutil.AssertWindowResultTimeMatch(t, rm, result, subscriptionId, "hourly")
```

### 时间和TTL
```go
// 时间范围（允许1秒误差）
testutil.AssertWindowTimeRange(t, rm, subscriptionId, "hourly", 3600, 1)

// TTL（允许10秒误差）
testutil.AssertWindowTTL(t, rm, subscriptionId, "hourly", 4200*time.Second, 10*time.Second)
```

## 🔧 Redis操作

### 基础操作
```go
// 检查Key存在
exists := rm.CheckKeyExists(t, key)

// 获取Hash字段
value, _ := rm.GetHashField(key, "consumed")
valueInt64, _ := rm.GetHashFieldInt64(key, "consumed")

// 获取所有字段
fields, _ := rm.GetHashAllFields(key)

// 获取TTL
ttl := rm.GetTTL(key)
```

### 时间控制
```go
// 快进时间
rm.FastForward(65 * time.Second)

// 重置Redis数据
rm.Reset()
```

### 调试工具
```go
// 打印Key详细信息
rm.DumpKey(t, "subscription:123:hourly:window")

// 打印窗口信息
testutil.DumpWindowInfo(t, rm, subscriptionId, "hourly")
```

## 🧪 测试数据创建

### 用户
```go
user := testutil.CreateTestUser(t, testutil.UserTestData{
	Username: "test-user",
	Group:    "default",
	Quota:    10000000,
})
```

### 套餐
```go
pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
	Name:        "test-package",
	Priority:    15,
	Quota:       100000000,
	HourlyLimit: 20000000,
	DailyLimit:  150000000,
	RpmLimit:    60,
})
```

### 订阅
```go
// 直接创建已激活的订阅
sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

// 或分步创建
sub := testutil.CreateTestSubscription(t, testutil.SubscriptionTestData{
	UserId:    user.Id,
	PackageId: pkg.Id,
	Status:    model.SubscriptionStatusInventory,
})
```

## 🎭 测试模式

### AAA模式
```go
// Arrange: 准备
rm, subscriptionId := setupTest(t)
defer teardownTest(rm)
config := testutil.CreateHourlyWindowConfig(subscriptionId, 20000000)

// Act: 执行
result := testutil.CallCheckAndConsumeWindow(t, ctx, config, 2500000)

// Assert: 验证
testutil.AssertWindowResultSuccess(t, result, 2500000)
```

### Table-Driven
```go
testCases := []struct {
	name     string
	quota    int64
	expected bool
}{
	{"under limit", 2000000, true},
	{"exceed limit", 15000000, false},
}

for _, tc := range testCases {
	t.Run(tc.name, func(t *testing.T) {
		result := testutil.CallCheckAndConsumeWindow(t, ctx, config, tc.quota)
		assert.Equal(t, tc.expected, result.Success)
	})
}
```

### 并发测试
```go
var wg sync.WaitGroup
var successCount int32

for i := 0; i < 100; i++ {
	wg.Add(1)
	go func() {
		defer wg.Done()
		result := testutil.CallCheckAndConsumeWindow(t, ctx, config, quota)
		if result.Success {
			atomic.AddInt32(&successCount, 1)
		}
	}()
}

wg.Wait()
assert.Equal(t, int32(50), atomic.LoadInt32(&successCount))
```

## 🐛 常见问题速查

| 错误 | 原因 | 解决方案 |
|------|------|----------|
| `miniredis启动失败` | 包未安装 | `go get github.com/alicebob/miniredis/v2` |
| `时间误差断言失败` | 时间计算误差 | 使用`assert.InDelta()`允许误差 |
| `并发测试不稳定` | 样本量不足 | 增加并发数到100+ |
| `Lua脚本未加载` | embed文件不存在 | 检查`.lua`文件路径 |
| `窗口Key不存在` | 未调用创建函数 | 确认调用`CallCheckAndConsumeWindow` |

## 📞 支持

### 查看测试日志
```bash
go test -v -run TestSW01
```

### 生成覆盖率
```bash
go test -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### 性能分析
```bash
go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

---

**版本**: v1.0
**最后更新**: 2025-12-12
