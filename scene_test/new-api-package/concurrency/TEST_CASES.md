# 并发测试用例索引

## 测试用例清单

### CR-01: Lua脚本原子性验证 ⭐️⭐️⭐️

**测试函数**: `TestCR01_LuaScriptAtomicity_ConcurrentDeduction`

**测试场景**:
```
并发配置:
├── Goroutine数量: 100
├── 每次请求quota: 0.15M (150,000)
├── 小时限额: 10M (10,000,000)
└── 理论最大成功数: 66 (10M / 0.15M)
```

**预期行为**:
1. ✅ 成功请求数 ≤ 66
2. ✅ consumed = 成功请求数 × 0.15M（精确匹配）
3. ✅ Redis consumed ≤ 10M（严格不超限）
4. ✅ 无TOCTOU竞态

**验证点**:
```go
// 原子性验证（零容忍）
assert.Equal(t, successCount * 150000, totalConsumed)

// 严格限制验证
assert.LessOrEqual(t, redisConsumed, 10000000)

// 竞态检测
tolerance := int64(0) // 不允许任何误差
assertAtomicIncrement(t, expected, actual, tolerance)
```

**关键技术点**:
- Redis Lua脚本的原子性（EXISTS + HINCRBY在单个脚本中执行）
- TOCTOU（Time-of-check to Time-of-use）竞态防护
- 严格限额执行（不允许超额1 quota）

---

### CR-02: 窗口创建并发竞争 ⭐️⭐️⭐️

**测试函数**: `TestCR02_WindowCreation_ConcurrentRace`

**测试场景**:
```
并发配置:
├── Goroutine数量: 100
├── 每次请求quota: 0.1M (100,000)
├── 小时限额: 50M (50,000,000) - 足够所有请求成功
└── 窗口状态: 不存在（首次请求）
```

**预期行为**:
1. ✅ 仅创建1个窗口
2. ✅ 所有请求看到相同的start_time
3. ✅ consumed = 100 × 0.1M = 10M

**验证点**:
```go
// 窗口数量验证
windowCount := countWindowKeys("subscription:*:hourly:*")
assert.Equal(t, 1, windowCount)

// start_time一致性验证
allSame := true
for _, st := range startTimes {
    if st != firstStartTime {
        allSame = false
    }
}
assert.True(t, allSame)
```

**关键技术点**:
- Lua脚本串行化（多个goroutine同时发现窗口不存在，但只创建一个）
- Redis EXISTS + HSET的原子性
- 窗口元数据一致性（所有请求看到同一个窗口）

---

### CR-03: 窗口过期并发重建 ⭐️⭐️⭐️

**测试函数**: `TestCR03_WindowExpired_ConcurrentRebuild`

**测试场景**:
```
初始状态:
├── 已存在过期窗口
│   ├── end_time: now - 10秒（已过期）
│   ├── consumed: 5M
│   └── limit: 50M
│
并发请求:
├── Goroutine数量: 100
├── 每次请求quota: 0.1M
└── 新窗口限额: 50M
```

**预期行为**:
1. ✅ 旧窗口被删除（只删除一次）
2. ✅ 新窗口被创建（只创建一次）
3. ✅ 新窗口consumed = 10M（不包含旧窗口的5M）
4. ✅ 所有请求看到相同的新start_time

**验证点**:
```go
// 窗口重建验证
assert.Greater(t, newStartTime, oldEndTime)

// 旧consumed被丢弃
assert.NotEqual(t, oldConsumed, redisConsumed)
assert.Equal(t, 10000000, redisConsumed) // 仅新请求的消耗

// 窗口时长验证
assert.Equal(t, 3600, endTime - startTime)
```

**关键技术点**:
- Lua脚本中的过期检测逻辑（检查end_time < now）
- DEL + HSET的原子性（删除旧窗口并创建新窗口）
- 窗口重建时consumed从0开始（不继承旧值）

**边界条件**:
- 如果多个线程同时发现过期，Lua脚本确保只执行一次DEL+HSET
- TTL清理和Lua脚本检测的双重保险

---

### CR-04: 多套餐并发扣减 ⭐️⭐️

**测试函数**: `TestCR04_MultiPackage_ConcurrentDeduction`

**测试场景**:
```
套餐配置:
├── 套餐A
│   ├── 优先级: 15 (高)
│   ├── 小时限额: 3M
│   └── 最大请求数: 15
│
├── 套餐B
│   ├── 优先级: 5 (低)
│   ├── 小时限额: 20M
│   └── 最大请求数: 100
│
并发请求:
├── Goroutine数量: 50
└── 每次请求quota: 0.2M
```

**预期行为**:
1. ✅ 前15个请求使用套餐A（高优先级）
2. ✅ 后35个请求使用套餐B（A超限后降级）
3. ✅ usedA + usedB = 50
4. ✅ 总consumed = 10M

**验证点**:
```go
// 优先级验证
assert.GreaterOrEqual(t, usedPackageA, 15)

// 总数验证
assert.Equal(t, 50, usedPackageA + usedPackageB)

// 总消耗验证
assert.Equal(t, 10000000, pkgAConsumed + pkgBConsumed)
```

**关键技术点**:
- 套餐选择器的优先级遍历逻辑
- 高优先级套餐超限时的自动降级
- 多套餐并发访问的一致性

**并发特性**:
- 套餐A和套餐B有独立的Redis窗口
- 优先级选择逻辑在应用层，每个请求独立执行
- 需要验证降级时机的准确性

---

### CR-05: 订阅启用并发冲突 ⭐️⭐️

**测试函数**: `TestCR05_SubscriptionActivation_ConcurrentConflict`

**测试场景**:
```
初始状态:
└── 订阅状态: inventory (未启用)

并发操作:
├── Goroutine数量: 2
└── 操作: 同时调用激活接口
```

**预期行为**:
1. ✅ 只有1个请求成功
2. ✅ 另1个请求返回"invalid status"
3. ✅ 最终状态为active
4. ✅ start_time和end_time只被设置一次

**验证点**:
```go
// 成功数验证
assert.Equal(t, 1, successCount)

// 状态冲突错误验证
assert.Equal(t, 1, statusConflictErrors)

// 最终状态验证
assert.Equal(t, "active", finalSub.Status)
assert.NotNil(t, finalSub.StartTime)
assert.NotNil(t, finalSub.EndTime)
```

**关键技术点**:
- DB状态机的原子转换
- WHERE子句保证的原子更新

**实现方案**:
```sql
-- 方案1: 条件更新（推荐）
UPDATE subscriptions
SET status = 'active', start_time = ?, end_time = ?
WHERE id = ? AND status = 'inventory'
-- 检查affected_rows，如果为0则表示状态已被修改

-- 方案2: 乐观锁
UPDATE subscriptions
SET status = 'active', start_time = ?, end_time = ?, version = version + 1
WHERE id = ? AND status = 'inventory' AND version = ?
```

---

### CR-06: total_consumed并发更新 ⭐️⭐️⭐️

**测试函数**: `TestCR06_TotalConsumed_ConcurrentUpdate`

**测试场景**:
```
并发配置:
├── Goroutine数量: 100
├── 每次更新quota: 0.1M (100,000)
└── 初始total_consumed: 0
```

**预期行为**:
1. ✅ 所有更新都成功
2. ✅ 最终total_consumed = 100 × 0.1M = 10M
3. ✅ 无lost update（累加丢失）
4. ✅ 无over-counting（重复计费）

**验证点**:
```go
// 精确匹配验证（零容忍）
assert.Equal(t, 10000000, actualTotalConsumed)

// lost update检测
diff := actualTotalConsumed - expectedTotalConsumed
assert.Equal(t, 0, diff)

// over-counting检测
assert.LessOrEqual(t, actualTotalConsumed, expectedTotalConsumed)
```

**关键技术点**:
- GORM Expr("total_consumed + ?", quota)的原子递增
- 避免read-modify-write竞态

**错误示例（会导致lost update）**:
```go
// ❌ 错误做法
sub := getSubscription(id)
newConsumed := sub.TotalConsumed + 100000
updateSubscription(id, newConsumed)

// 并发问题：
// T1: 读取0, 计算100000, 写入100000
// T2: 读取0, 计算100000, 写入100000
// 结果: 100000 (丢失了一次更新)

// ✅ 正确做法
DB.Model(&Subscription{}).
   Where("id = ?", id).
   Update("total_consumed", gorm.Expr("total_consumed + ?", 100000))
```

---

## 测试执行流程

### 标准执行流程
```bash
# 1. 运行所有并发测试
go test -v ./scene_test/new-api-package/concurrency/

# 2. 仅运行P0级别测试
go test -v -run "CR01|CR02|CR03|CR06" ./scene_test/new-api-package/concurrency/

# 3. 使用竞态检测器
go test -v -race ./scene_test/new-api-package/concurrency/
```

### 性能基准测试
```bash
# 运行性能基准
go test -bench=. -benchmem ./scene_test/new-api-package/concurrency/

# 生成性能分析
go test -cpuprofile=cpu.prof -memprofile=mem.prof -bench=.
go tool pprof cpu.prof
```

## 测试通过标准

### P0级别（必须通过）
- ✅ CR-01: Lua脚本原子性验证
- ✅ CR-02: 窗口创建并发竞争
- ✅ CR-03: 窗口过期并发重建
- ✅ CR-06: total_consumed并发更新

### P1级别（重要）
- ✅ CR-04: 多套餐并发扣减
- ✅ CR-05: 订阅启用并发冲突

### 性能要求
- 100并发下，单次Lua脚本执行时间 < 2ms
- 总测试执行时间 < 30秒
- 无内存泄漏
- 竞态检测器无告警

## 常见问题排查

### Q1: 测试中出现consumed不匹配

**现象**:
```
Expected: 10000000, Actual: 9850000
```

**可能原因**:
1. 部分请求失败但未正确计数
2. Lua脚本逻辑错误
3. miniredis与真实Redis行为不一致

**排查方法**:
```go
// 启用详细日志
for i, err := range errors {
    if err != nil {
        t.Logf("Request %d failed: %v", i, err)
    }
}

// 检查Redis状态
fields := windowHelper.GetAllWindowFields(subId, "hourly")
t.Logf("Window state: %+v", fields)
```

### Q2: 窗口被创建多次

**现象**:
```
Expected: 1 window, Actual: 3 windows
```

**可能原因**:
1. miniredis不支持Lua原子性
2. 窗口Key命名冲突
3. Lua脚本逻辑错误

**排查方法**:
```bash
# 查看所有窗口Key
redis-cli --scan --pattern "subscription:*:window"

# 检查Lua脚本
redis-cli SCRIPT LOAD "$(cat check_and_consume_sliding_window.lua)"
```

### Q3: 订阅被重复激活

**现象**:
```
Expected: 1 success, Actual: 2 success
```

**可能原因**:
1. DB UPDATE未使用WHERE条件
2. 缺乏事务保护
3. 状态检查和更新之间有时间窗口

**排查方法**:
```sql
-- 检查SQL语句
-- 正确做法:
UPDATE subscriptions
SET status = 'active', start_time = ?, end_time = ?
WHERE id = ? AND status = 'inventory'
RETURNING *

-- 检查affected_rows
if affected_rows == 0 {
    return errors.New("invalid status")
}
```

## 调试技巧

### 1. 启用详细日志
```go
s.T().Logf("Goroutine %d: attempting to consume %d", i, requestQuota)
```

### 2. 使用竞态检测器
```bash
go test -v -race -run TestCR01
```

### 3. 检查Redis状态
```go
// 在测试中间暂停并检查Redis
time.Sleep(100 * time.Millisecond)
keys := server.MiniRedis.Keys()
t.Logf("Redis keys: %v", keys)
```

### 4. 验证原子计数器
```go
// 使用atomic包避免测试本身的竞态
var counter int32
atomic.AddInt32(&counter, 1)
finalValue := atomic.LoadInt32(&counter)
```

## 依赖的Service层函数

### 必需实现的函数

1. **service.CheckAndConsumeSlidingWindow**
```go
func CheckAndConsumeSlidingWindow(
    subscriptionId int,
    config SlidingWindowConfig,
    quota int64,
) (*WindowResult, error)
```

2. **model.IncrementSubscriptionConsumed**
```go
func IncrementSubscriptionConsumed(subscriptionId int, quota int64) error {
    return DB.Model(&Subscription{}).
        Where("id = ?", subscriptionId).
        Update("total_consumed", gorm.Expr("total_consumed + ?", quota)).
        Error
}
```

3. **model.ActivateSubscription**
```go
func ActivateSubscription(subscriptionId int, now int64) error {
    endTime := now + 30*24*3600 // 假设1个月

    result := DB.Model(&Subscription{}).
        Where("id = ? AND status = ?", subscriptionId, SubscriptionStatusInventory).
        Updates(map[string]interface{}{
            "status":     SubscriptionStatusActive,
            "start_time": now,
            "end_time":   endTime,
        })

    if result.RowsAffected == 0 {
        return errors.New("invalid status")
    }
    return result.Error
}
```

## 补充验证建议

### 1. 增加延迟测试
在某些测试中增加随机延迟，模拟更真实的网络延迟：
```go
time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
```

### 2. 增加失败注入
模拟Redis临时故障：
```go
if i%10 == 0 {
    // 模拟10%的Redis失败
    server.MiniRedis.Close()
    time.Sleep(100 * time.Millisecond)
    server.MiniRedis.Restart()
}
```

### 3. 增加数据一致性交叉验证
```go
// 验证Redis和DB的一致性
redisTotal := pkgAConsumed + pkgBConsumed
dbTotalA := subA.TotalConsumed
dbTotalB := subB.TotalConsumed
assert.Equal(t, redisTotal, dbTotalA + dbTotalB)
```

---

**文档版本**: v1.0
**最后更新**: 2025-12-12
**维护者**: QA Team
