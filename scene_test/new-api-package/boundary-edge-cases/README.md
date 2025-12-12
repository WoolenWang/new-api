# 边界与异常场景测试套件

## 测试目标

本测试套件对应测试设计文档 `NewAPI-支持多种包月套餐-优化版-测试方案.md` 中的 **2.10 边界与异常场景测试 (Boundary & Edge Cases)**，旨在验证系统在极端条件和异常输入下的鲁棒性。

## 测试用例清单

### 时间边界测试（P0优先级）

| 测试ID | 测试函数 | 测试场景 | 验证重点 |
| :--- | :--- | :--- | :--- |
| **EC-01** | `TestEC01_WindowTimeBoundary_ExactlyExpired` | 窗口时间刚好过期（end_time = now） | Lua判定为过期，删除并重建窗口 |
| **EC-02** | `TestEC02_WindowTimeBoundary_OneSecondLeft` | 窗口还有1秒未过期（end_time = now + 1） | Lua判定为有效，允许扣减 |

### 限额边界测试（P0优先级）

| 测试ID | 测试函数 | 测试场景 | 验证重点 |
| :--- | :--- | :--- | :--- |
| **EC-03** | `TestEC03_LimitBoundary_ExactlyFull` | 消耗刚好达到限额（consumed + quota = limit） | Lua判定为未超限，扣减成功 |
| **EC-04** | `TestEC04_LimitBoundary_ExceedByOne` | 消耗超出限额1 quota（consumed + quota > limit） | Lua判定为超限，拒绝扣减 |

### 生命周期边界测试（P0优先级）

| 测试ID | 测试函数 | 测试场景 | 验证重点 |
| :--- | :--- | :--- | :--- |
| **EC-05** | `TestEC05_SubscriptionLifecycleBoundary_ExactlyExpired` | 套餐end_time刚好到期 | 定时任务标记为expired，不可用于新请求 |

### 极端数值测试（P2优先级）

| 测试ID | 测试函数 | 测试场景 | 验证重点 |
| :--- | :--- | :--- | :--- |
| **EC-06** | `TestEC06_MinimalQuota_OneQuota` | 请求极小值（1 quota） | 系统正常处理 |
| **EC-07** | `TestEC07_MaximalQuota_HundredMillion` | 请求极大值（100M quota） | 正确处理超限，不崩溃 |

### 套餐数量边界测试

| 测试ID | 测试函数 | 测试场景 | 验证重点 | 优先级 |
| :--- | :--- | :--- | :--- | :--- |
| **EC-08** | `TestEC08_ZeroPackages_UseBalance` | 用户拥有0个套餐 | 直接使用用户余额 | P1 |
| **EC-09** | `TestEC09_TwentyPackages_Performance` | 用户拥有20个套餐 | 优先级遍历性能<50ms | P2 |

### 配置边界测试（P1优先级）

| 测试ID | 测试函数 | 测试场景 | 验证重点 |
| :--- | :--- | :--- | :--- |
| **EC-10** | `TestEC10_PackageQuotaZero_OnlyCheckWindows` | 套餐quota=0（月度不限制） | 仅检查滑动窗口 |
| **EC-11** | `TestEC11_AllLimitsZero_OnlyCheckMonthly` | 所有limit都为0（滑动窗口不限制） | 仅检查月度总限额 |

### 数据隔离测试（P2优先级）

| 测试ID | 测试函数 | 测试场景 | 验证重点 |
| :--- | :--- | :--- | :--- |
| **EC-12** | `TestEC12_RedisKeyConflict_Isolation` | 两个订阅的Redis Key隔离性 | 系统正确隔离，无数据污染 |

## 测试技术要点

### 1. 时间控制
使用miniredis的`FastForward`功能模拟时间流逝：
```go
rm.FastForward(60 * time.Second) // 快进60秒
```

### 2. 预设窗口状态
使用`testutil.PresetWindowState`手动设置Redis Hash，用于测试边界条件：
```go
testutil.PresetWindowState(t, rm, subscriptionId, "hourly", testutil.WindowState{
    StartTime: time.Now().Unix(),
    EndTime:   time.Now().Unix() + 3600,
    Consumed:  9999999,  // 设置为接近limit的值
    Limit:     10000000,
})
```

### 3. 性能测量
对于性能相关测试（如EC-09），使用`time.Since()`测量执行时间：
```go
startTime := time.Now()
// ... 执行操作 ...
elapsed := time.Since(startTime)
assert.Less(t, elapsed, 50*time.Millisecond, "Operation should complete within 50ms")
```

### 4. 数据隔离验证
测试不同订阅的窗口是否完全隔离，确保Redis Key命名唯一性。

## 运行测试

### 运行所有边界测试
```bash
cd scene_test/new-api-package/boundary-edge-cases
go test -v
```

### 运行特定测试
```bash
# 运行时间边界测试
go test -v -run TestEC01
go test -v -run TestEC02

# 运行限额边界测试
go test -v -run TestEC03
go test -v -run TestEC04

# 运行性能测试
go test -v -run TestEC09
```

### 运行P0优先级测试
```bash
go test -v -run "TestEC0[1-5]"
```

## 依赖说明

### 需要实现的服务层函数
本测试套件依赖以下服务层函数（需在实现阶段完成）：

1. **service.CheckAndConsumeSlidingWindow** - 滑动窗口检查并消耗（已实现）
2. **service.GetSlidingWindowConfigs** - 获取套餐的所有滑动窗口配置
3. **model.GetUserActiveSubscriptions** - 查询用户活跃订阅列表
4. **model.GetSubscriptionById** - 根据ID获取订阅（已实现）
5. **model.GetPackageByID** - 根据ID获取套餐（已实现）

### 需要实现的常量
确保 `model` 包中定义了以下常量：
```go
const (
    SubscriptionStatusInventory = "inventory"
    SubscriptionStatusActive    = "active"
    SubscriptionStatusExpired   = "expired"
    SubscriptionStatusCancelled = "cancelled"
)
```

## 测试数据准备

每个测试用例都会创建独立的测试环境：
- 测试用户（100M余额）
- 测试套餐（100M月度限额，20M小时限额等）
- 激活的订阅

## 验证要点

### 时间边界精确性
- 刚好过期（end_time = now）应重建
- 差1秒（end_time = now + 1）应保持有效

### 限额计算精确性
- 刚好用尽（consumed + quota = limit）应通过
- 超1 quota（consumed + quota = limit + 1）应拒绝

### 极端值处理
- 1 quota（极小值）应正常处理
- 100M quota（极大值）应正确判断超限

### 性能保证
- 20个套餐查询应在50ms内完成

## 故障排查

### 测试跳过
如果某些测试被skip，说明对应的服务层函数尚未实现：
```
--- SKIP: TestEC05_SubscriptionLifecycleBoundary_ExactlyExpired (0.00s)
    boundary_edge_test.go:328: GetUserActiveSubscriptions not implemented yet
```

### 时间相关失败
如果时间边界测试失败，检查：
1. miniredis的FastForward是否正确工作
2. Lua脚本中的时间比较逻辑是否正确（>= vs >）

### 限额相关失败
如果限额边界测试失败，检查：
1. Lua脚本中的限额判断逻辑（consumed + quota > limit）
2. PresetWindowState是否正确设置了初始状态

## 集成说明

本测试套件与其他测试套件的关系：
- **sliding-window/** - 滑动窗口基础功能测试
- **concurrency/** - 并发场景测试
- **billing/** - 计费准确性测试
- **priority-fallback/** - 优先级与Fallback测试

边界测试补充了上述测试套件，专注于极端条件和边界情况。

## 参考文档

- 设计文档：`docs/NewAPI-支持多种包月套餐-优化版.md`
- 测试方案：`docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 第2.10节
- Lua脚本：`service/check_and_consume_sliding_window.lua`
- 辅助函数：`scene_test/testutil/boundary_edge_helper.go`
