# 套餐统计值正确性验证测试套件

## 概述

本测试套件实现了《NewAPI-支持多种包月套餐-优化版-测试方案.md》中 **2.9 统计值正确性验证测试 (Statistics Correctness)** 章节的全部8个测试案例。

## 测试目标

验证套餐消耗统计、窗口使用率等衍生数据的计算准确性，确保：
- 订阅的 `total_consumed` 仅累计成功请求
- Redis滑动窗口的 `consumed` 值正确累加
- 窗口使用率、剩余时间、剩余额度等衍生指标计算准确
- 多窗口独立统计正确
- Fallback和窗口超限的统计指标准确

## 测试案例清单

| 测试ID | 测试场景 | 优先级 | 实现函数 |
|:---|:---|:---:|:---|
| **ST-01** | total_consumed累计（成功请求累加，失败请求不计） | P0 | `TestST01_TotalConsumedAccumulation` |
| **ST-02** | 滑动窗口consumed验证 | P0 | `TestST02_SlidingWindowConsumed` |
| **ST-03** | 窗口使用率计算 | P1 | `TestST03_WindowUtilizationRate` |
| **ST-04** | 窗口剩余时间计算 | P1 | `TestST04_WindowTimeLeft` |
| **ST-05** | 多窗口聚合统计 | P1 | `TestST05_MultiWindowAggregation` |
| **ST-06** | 套餐剩余额度计算 | P1 | `TestST06_RemainingQuotaCalculation` |
| **ST-07** | Fallback触发率统计 | P2 | `TestST07_FallbackTriggerRate` |
| **ST-08** | 窗口超限次数统计 | P2 | `TestST08_WindowExceededCount` |

## 测试架构

### 核心组件

1. **statistics_test.go**: 主测试文件，包含所有8个测试函数
2. **testutil/statistics_helper.go**: 统计测试专用辅助函数
3. **testutil/package_helper.go**: 套餐和订阅管理辅助函数
4. **miniredis**: 内存Redis模拟器，用于验证滑动窗口状态

### 测试数据隔离

每个测试用例：
- 独立创建用户、套餐、订阅
- 使用独立的miniredis实例（全局共享）
- 测试结束后清理数据（`CleanupPackageTestData`）

## 运行测试

### 运行所有统计测试
```bash
cd scene_test/new-api-package/statistics
go test -v
```

### 运行单个测试
```bash
# ST-01: total_consumed累计
go test -v -run TestST01_TotalConsumedAccumulation

# ST-02: 滑动窗口consumed
go test -v -run TestST02_SlidingWindowConsumed

# ST-03: 窗口使用率
go test -v -run TestST03_WindowUtilizationRate

# ST-04: 窗口剩余时间
go test -v -run TestST04_WindowTimeLeft

# ST-05: 多窗口聚合
go test -v -run TestST05_MultiWindowAggregation

# ST-06: 剩余额度
go test -v -run TestST06_RemainingQuotaCalculation

# ST-07: Fallback触发率
go test -v -run TestST07_FallbackTriggerRate

# ST-08: 窗口超限次数
go test -v -run TestST08_WindowExceededCount
```

### 运行P0优先级测试
```bash
go test -v -run "TestST0[12]"
```

### 生成测试覆盖率报告
```bash
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

## 测试关键验证点

### ST-01: total_consumed累计
- ✅ 初始值为0
- ✅ 成功请求累加到total_consumed
- ✅ 失败请求不影响total_consumed
- ✅ 最终值仅为成功请求的总和

### ST-02: 滑动窗口consumed
- ✅ Redis窗口Hash正确创建
- ✅ consumed字段正确累加
- ✅ 多次请求累加值正确

### ST-03: 窗口使用率
- ✅ 使用率公式：(consumed/limit) × 100
- ✅ 7M/10M = 70%
- ✅ 浮点数精度验证（InDelta）

### ST-04: 窗口剩余时间
- ✅ 剩余时间公式：end_time - current_time
- ✅ end_time在未来1800秒，剩余时间=1800
- ✅ 时间边界正确

### ST-05: 多窗口聚合统计
- ✅ 小时、日、周窗口独立创建
- ✅ 各窗口consumed值独立统计
- ✅ 窗口间不相互影响

### ST-06: 套餐剩余额度
- ✅ 剩余额度公式：quota - total_consumed
- ✅ 100M - 35M = 65M
- ✅ DB数据正确读取

### ST-07: Fallback触发率
- ✅ 模拟100次请求
- ✅ 统计Fallback触发次数
- ✅ 计算触发率百分比
- ✅ 验证套餐consumed仅为非Fallback请求

### ST-08: 窗口超限次数
- ✅ 模拟100次请求
- ✅ 统计窗口超限拒绝次数
- ✅ 计算超限率
- ✅ 验证不允许Fallback时用户余额不变

## 辅助函数说明

### statistics_helper.go 提供的核心函数

| 函数名 | 功能 | 使用场景 |
|:---|:---|:---|
| `SimulateWindowInRedis` | 在miniredis中创建滑动窗口 | 模拟窗口状态 |
| `GetWindowConsumedFromRedis` | 获取窗口consumed值 | 读取窗口消耗 |
| `GetWindowLimitFromRedis` | 获取窗口limit值 | 读取窗口限额 |
| `GetWindowTimeFromRedis` | 获取窗口时间信息 | 读取窗口时间范围 |
| `CalculateWindowUtilizationRate` | 计算窗口使用率 | 衍生指标计算 |
| `CalculateRemainingTime` | 计算剩余时间 | 衍生指标计算 |
| `CalculateRemainingQuota` | 计算剩余额度 | 衍生指标计算 |
| `AssertWindowConsumed` | 断言窗口consumed值 | 测试验证 |
| `AssertWindowUtilization` | 断言窗口使用率 | 测试验证 |
| `AssertWindowTimeLeft` | 断言窗口剩余时间 | 测试验证 |
| `AssertRemainingQuota` | 断言剩余额度 | 测试验证 |
| `UpdateSubscriptionConsumed` | 更新订阅消耗量 | 测试数据准备 |
| `IncrementSubscriptionConsumed` | 增加订阅消耗量 | 测试数据准备 |

## 依赖说明

### 外部依赖
- `github.com/alicebob/miniredis/v2`: Redis内存模拟器
- `github.com/stretchr/testify/assert`: 断言库

### 内部依赖
- `model`: 数据模型层（Package, Subscription, User等）
- `common`: 公共工具函数
- `testutil`: 测试工具函数

## 注意事项

1. **数据库状态**: 每个测试后需调用 `CleanupPackageTestData` 清理数据
2. **Redis状态**: 每个测试后需手动删除创建的窗口Key
3. **时间依赖**: 部分测试依赖当前时间，使用 `time.Now().Unix()` 获取
4. **并发安全**: 当前测试为单线程顺序执行，不涉及并发问题
5. **浮点精度**: 使用率等浮点计算使用 `assert.InDelta` 允许0.01误差

## 故障排查

### 测试失败常见原因

1. **Redis连接失败**
   - 确保miniredis正确启动
   - 检查 `mr` 变量是否初始化

2. **数据库查询失败**
   - 确保数据库连接已建立
   - 检查 `model.DB` 是否初始化

3. **数据未清理**
   - 上一个测试未正确清理
   - 手动执行 `CleanupPackageTestData`

4. **时间计算错误**
   - 检查时间戳单位（秒 vs 毫秒）
   - 验证窗口duration设置

### 调试技巧

```go
// 打印窗口详细信息
startTime, endTime, consumed, limit, _ := testutil.GetWindowInfo(mr, subId, "hourly")
t.Logf("Window: start=%d, end=%d, consumed=%d, limit=%d", startTime, endTime, consumed, limit)

// 打印订阅信息
sub, _ := model.GetSubscriptionById(subId)
t.Logf("Subscription: id=%d, consumed=%d, status=%s", sub.Id, sub.TotalConsumed, sub.Status)

// 检查Redis Key是否存在
key := testutil.FormatWindowKey(subId, "hourly")
exists := mr.Exists(key)
t.Logf("Window key %s exists: %v", key, exists)
```

## 维护者

- QA Team
- 后端开发团队

---

**最后更新**: 2025-12-12
**版本**: v1.0
**对应文档**: NewAPI-支持多种包月套餐-优化版-测试方案.md (第2.9节)
