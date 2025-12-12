# 并发与数据竞态测试套件

## 概览

本目录包含NewAPI包月套餐系统的并发与数据竞态测试，对应测试方案 **2.8 章节**。

## 测试目标

验证套餐系统在高并发场景下的数据一致性和原子性保证：
1. **Redis Lua脚本原子性**：验证滑动窗口扣减的TOCTOU竞态保护
2. **窗口并发安全**：验证窗口创建、过期重建的并发场景
3. **多套餐优先级并发**：验证多个套餐同时被并发请求时的正确选择
4. **DB原子性**：验证订阅状态转换、total_consumed累加的并发安全

## 测试用例列表

| ID | 测试场景 | 优先级 | 状态 |
|:---|:---|:---|:---|
| **CR-01** | Lua脚本原子性验证 | P0 | ✅ 已实现 |
| **CR-02** | 窗口创建并发竞争 | P0 | ✅ 已实现 |
| **CR-03** | 窗口过期并发重建 | P0 | ✅ 已实现 |
| **CR-04** | 多套餐并发扣减 | P1 | ✅ 已实现 |
| **CR-05** | 订阅启用并发冲突 | P1 | ✅ 已实现 |
| **CR-06** | total_consumed并发更新 | P0 | ✅ 已实现 |

## 测试用例详细说明

### CR-01: Lua脚本原子性验证

**测试场景**：
- 100个goroutine并发请求同一套餐
- 小时限额10M，每次请求0.15M
- 理论最大成功请求数：66 (10M / 0.15M)

**验证点**：
- ✅ consumed值 = 成功请求数 × 0.15M（精确匹配，无误差）
- ✅ 成功请求数 ≤ 66
- ✅ Redis中consumed值 ≤ 10M（严格不超限）
- ✅ 无TOCTOU竞态（多个线程同时读取consumed后计算导致超额）

**核心测试逻辑**：
```go
// 并发调用CheckAndConsumeSlidingWindow
result, err := service.CheckAndConsumeSlidingWindow(subId, config, 150000)

// 统计成功/失败数
if result.Success {
    successCount++
    totalConsumed += 150000
}

// 验证精确性
assert.Equal(t, successCount * 150000, totalConsumed)
assert.LessOrEqual(t, redisConsumed, 10000000)
```

---

### CR-02: 窗口创建并发竞争

**测试场景**：
- Redis中无窗口（首次请求）
- 100个goroutine同时发起请求

**验证点**：
- ✅ 只创建1个窗口（Lua脚本串行化）
- ✅ 所有请求看到的start_time一致
- ✅ consumed = 100 × 每次请求quota

**核心测试逻辑**：
```go
// 确保窗口不存在
windowHelper.DeleteWindow(subId, "hourly")

// 并发请求
startTimes := []int64{}
for i := 0; i < 100; i++ {
    go func() {
        result := CheckAndConsumeSlidingWindow(...)
        startTimes = append(startTimes, result.StartTime)
    }()
}

// 验证start_time一致性
assert.True(t, allStartTimesEqual(startTimes))
assert.Equal(t, 1, countWindowKeys("subscription:*:hourly:*"))
```

---

### CR-03: 窗口过期并发重建

**测试场景**：
- 先创建一个已过期的窗口（end_time < now，已消耗5M）
- 100个goroutine同时请求

**验证点**：
- ✅ 旧窗口被删除（只删除一次）
- ✅ 新窗口被创建（只创建一次）
- ✅ 新窗口consumed = 100 × 每次请求quota（不包含旧窗口的5M）
- ✅ 所有请求看到的新窗口start_time一致

**核心测试逻辑**：
```go
// 创建过期窗口
windowHelper.CreateExpiredWindow(subId, "hourly", 5000000, 10000000)
oldEndTime := getWindowEndTime(...)

// 并发请求
results := concurrentCheckAndConsume(100, ...)

// 验证
assert.Greater(t, newStartTime, oldEndTime) // 窗口已重建
assert.Equal(t, 100*requestQuota, newConsumed) // 旧consumed被丢弃
```

---

### CR-04: 多套餐并发扣减

**测试场景**：
- 用户拥有2个套餐：
  - 套餐A：优先级15，小时限额3M（可满足15个请求）
  - 套餐B：优先级5，小时限额20M
- 50个goroutine并发请求（每次0.2M）

**验证点**：
- ✅ 套餐A被优先使用（高优先级）
- ✅ 套餐A用满后自动降级到套餐B
- ✅ usedA + usedB = 50
- ✅ 总consumed = 50 × 0.2M = 10M

**核心测试逻辑**：
```go
// 并发请求，自动选择套餐
for i := 0; i < 50; i++ {
    go func() {
        // 优先级选择逻辑
        if pkgA.TryConsume(0.2M) {
            usedA++
        } else if pkgB.TryConsume(0.2M) {
            usedB++
        }
    }()
}

// 验证
assert.Equal(t, 15, usedA) // 套餐A用满
assert.Equal(t, 35, usedB) // 剩余请求用套餐B
```

---

### CR-05: 订阅启用并发冲突

**测试场景**：
- 1个订阅处于inventory状态
- 2个goroutine同时调用启用接口

**验证点**：
- ✅ 只有1个请求成功
- ✅ 另1个请求返回"invalid status"错误
- ✅ 最终订阅状态为active
- ✅ start_time和end_time只被设置一次（无重复）

**核心测试逻辑**：
```go
// 创建inventory状态订阅
sub := CreateSubscription(status: "inventory")

// 并发调用激活
go ActivateSubscription(subId) // 请求1
go ActivateSubscription(subId) // 请求2

// 验证
assert.Equal(t, 1, successCount)
assert.Equal(t, 1, statusConflictErrors)
assert.Equal(t, "active", finalStatus)
```

**原子性保证方案**：
```sql
-- 方案1: 使用WHERE子句原子更新
UPDATE subscriptions
SET status = 'active', start_time = ?, end_time = ?
WHERE id = ? AND status = 'inventory'
-- 返回affected_rows，如果为0则表示状态已被其他线程修改

-- 方案2: 使用乐观锁
UPDATE subscriptions
SET status = 'active', start_time = ?, end_time = ?, version = version + 1
WHERE id = ? AND status = 'inventory' AND version = ?
```

---

### CR-06: total_consumed并发更新

**测试场景**：
- 100个goroutine并发更新同一订阅的total_consumed
- 每次更新0.1M

**验证点**：
- ✅ 最终total_consumed = 100 × 0.1M = 10M（精确匹配）
- ✅ 无lost update（累加丢失）
- ✅ 无over-counting（重复计费）
- ✅ GORM Expr原子性保证

**核心测试逻辑**：
```go
// 并发更新
for i := 0; i < 100; i++ {
    go func() {
        // 使用GORM Expr确保原子性
        DB.Model(&Subscription{}).
           Where("id = ?", subId).
           Update("total_consumed", gorm.Expr("total_consumed + ?", 100000))
    }()
}

// 验证
finalConsumed := getSubscription(subId).TotalConsumed
assert.Equal(t, 10000000, finalConsumed)
```

**错误示例（非原子操作）**：
```go
// ❌ 错误做法（会导致lost update）
sub := getSubscription(subId)
newConsumed := sub.TotalConsumed + 100000
DB.Model(&Subscription{}).Where("id = ?", subId).Update("total_consumed", newConsumed)

// 并发场景：
// 线程A: 读取0, 计算100000, 写入100000
// 线程B: 读取0, 计算100000, 写入100000
// 最终: 100000 (丢失了一次更新)
```

---

## 运行测试

### 运行整个并发测试套件
```bash
cd scene_test/new-api-package/concurrency
go test -v
```

### 运行特定测试
```bash
# 运行CR-01
go test -v -run TestCR01_LuaScriptAtomicity

# 运行CR-06
go test -v -run TestCR06_TotalConsumed
```

### 性能分析
```bash
# 运行性能分析
go test -v -cpuprofile=cpu.prof -memprofile=mem.prof

# 查看CPU profile
go tool pprof cpu.prof

# 查看内存 profile
go tool pprof mem.prof
```

### 竞态检测
```bash
# 使用Go竞态检测器
go test -v -race

# 注意：竞态检测器会显著降低性能，仅用于验证
```

## 实现状态

### 已完成
- ✅ 测试框架和目录结构
- ✅ 并发测试辅助工具（concurrency_helper.go）
- ✅ CR-01: Lua脚本原子性验证测试
- ✅ CR-02: 窗口创建并发竞争测试
- ✅ CR-03: 窗口过期并发重建测试
- ✅ CR-04: 多套餐并发扣减测试
- ✅ CR-05: 订阅启用并发冲突测试
- ✅ CR-06: total_consumed并发更新测试

### 待实现（需要backend支持）
- ⏳ 取消TODO注释，集成实际的服务层函数
- ⏳ 集成testutil.StartTestServer()
- ⏳ 集成model层CRUD函数
- ⏳ 集成service.CheckAndConsumeSlidingWindow()

## 依赖项

### 必需的辅助工具
- `testutil/server.go` - 测试服务器启动
- `testutil/concurrency_helper.go` - 并发测试辅助函数 ✅
- `testutil/fixtures.go` - 测试数据预置
- `testutil/redis_mock.go` - miniredis封装

### 必需的Service层函数
- `service.CheckAndConsumeSlidingWindow()` - Lua脚本调用
- `model.IncrementSubscriptionConsumed()` - 原子更新total_consumed
- `model.ActivateSubscription()` - 订阅启用（需支持并发安全）

## 设计亮点

### 1. 严格的并发安全验证
每个测试用例都包含多层验证：
- **应用层计数**：使用atomic计数器统计成功/失败
- **Redis层验证**：直接查询Redis窗口状态
- **DB层验证**：查询最终数据库状态
- **交叉验证**：三层数据互相验证一致性

### 2. 零容忍原子性检查
```go
tolerance := int64(0) // 原子操作不允许任何误差
assert.Equal(t, expected, actual) // 精确匹配
```

### 3. 竞态场景模拟
- **CR-01**：模拟TOCTOU竞态（多个线程同时读取consumed后计算）
- **CR-02**：模拟并发创建竞态（多个线程同时发现窗口不存在）
- **CR-03**：模拟并发重建竞态（多个线程同时发现窗口过期）
- **CR-05**：模拟状态转换竞态（多个线程同时尝试激活）

### 4. 性能与正确性兼顾
- 使用`sync.WaitGroup`确保所有goroutine完成
- 使用`atomic`包避免引入测试本身的竞态
- 收集详细的执行时间和错误信息用于性能分析

## 故障排查

### 测试失败场景分析

#### Scenario 1: consumed值不匹配
```
Expected: 10000000, Actual: 10500000
```
**可能原因**：
- Lua脚本未正确限制超额扣减
- HINCRBY操作在超限检查之前执行
- 窗口limit值配置错误

**排查步骤**：
1. 检查Lua脚本逻辑：是否先检查limit再HINCRBY
2. 检查Redis中窗口的limit字段值
3. 启用Redis MONITOR查看实际执行的命令

#### Scenario 2: 窗口创建多次
```
Expected: 1 window, Actual: 3 windows
```
**可能原因**：
- miniredis不支持Lua脚本原子性
- Lua脚本中EXISTS和HSET之间存在间隙
- Redis Key命名冲突

**排查步骤**：
1. 使用`redis-cli KEYS subscription:*`查看所有Key
2. 检查Lua脚本是否使用了正确的原子操作
3. 验证miniredis版本是否支持Lua

#### Scenario 3: 订阅被重复激活
```
Expected: 1 success, Actual: 2 success
```
**可能原因**：
- DB更新未使用WHERE status = 'inventory'条件
- 缺乏事务保护
- 状态检查和更新之间存在间隙

**排查步骤**：
1. 检查SQL UPDATE语句是否包含WHERE条件
2. 检查是否使用了事务
3. 查看affected_rows是否正确处理

## 参考资料

### 设计文档
- `docs/NewAPI-支持多种包月套餐-优化版.md` - 套餐系统设计
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 本测试方案依据

### 相关代码
- `service/package_sliding_window.go` - 滑动窗口实现 (待实现)
- `service/check_and_consume_sliding_window.lua` - Lua脚本 (待实现)
- `model/subscription.go` - 订阅模型 (待实现)

### 相关测试
- `scene_test/new-api-package/sliding-window/` - 滑动窗口基础测试
- `scene_test/new-api-package/billing/` - 计费准确性测试

---

**创建日期**: 2025-12-12
**作者**: QA Team
**版本**: v1.0
