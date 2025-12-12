# 边界与异常场景测试用例清单

## 测试用例详细信息

### EC-01: 窗口时间边界-刚好过期

**测试ID**: EC-01
**优先级**: P0
**函数**: `TestEC01_WindowTimeBoundary_ExactlyExpired`

**测试场景**:
- 创建duration=60秒的滑动窗口
- 首次请求创建窗口，消耗2.5M
- 快进刚好60秒（end_time = now）
- 再次请求，消耗3M

**验证点**:
1. ✓ 第二次请求成功
2. ✓ 新窗口start_time > 旧窗口start_time（窗口已重建）
3. ✓ 新窗口consumed=3M（重新开始计数，而非累加）
4. ✓ 新窗口duration=60秒
5. ✓ Lua脚本正确判定窗口已过期

**Lua脚本关键逻辑**:
```lua
if now >= end_time then
    -- 窗口过期，删除并重建
    redis.call('DEL', key)
    -- 创建新窗口...
end
```

---

### EC-02: 窗口时间边界-差1秒

**测试ID**: EC-02
**优先级**: P0
**函数**: `TestEC02_WindowTimeBoundary_OneSecondLeft`

**测试场景**:
- 创建duration=60秒的滑动窗口
- 首次请求创建窗口，消耗2.5M
- 快进59秒（end_time = now + 1，还有1秒）
- 再次请求，消耗3M

**验证点**:
1. ✓ 第二次请求成功
2. ✓ 窗口时间保持不变（start_time和end_time都不变）
3. ✓ consumed累加为5.5M（2.5M + 3M）
4. ✓ Lua脚本正确判定窗口仍然有效

**边界条件**:
- `now >= end_time` → 过期
- `now < end_time` → 有效（差1秒的情况）

---

### EC-03: 限额边界-刚好用尽

**测试ID**: EC-03
**优先级**: P0
**函数**: `TestEC03_LimitBoundary_ExactlyFull`

**测试场景**:
- 创建limit=10000000的窗口
- 预设consumed=9999999
- 请求1 quota（9999999 + 1 = 10000000）

**验证点**:
1. ✓ 请求成功（刚好等于限额，应允许）
2. ✓ consumed精确为10000000
3. ✓ Lua脚本正确判断 `consumed + quota <= limit`

**边界条件**:
- `consumed + quota <= limit` → 允许
- `consumed + quota > limit` → 拒绝

---

### EC-04: 限额边界-超1 quota

**测试ID**: EC-04
**优先级**: P0
**函数**: `TestEC04_LimitBoundary_ExceedByOne`

**测试场景**:
- 创建limit=10000000的窗口
- 预设consumed=10000000（已用尽）
- 请求1 quota（10000000 + 1 = 10000001 > limit）

**验证点**:
1. ✓ 请求失败（超限）
2. ✓ consumed保持10000000不变（未扣减）
3. ✓ Lua脚本返回status=0

**Lua脚本关键逻辑**:
```lua
if consumed + quota > limit then
    -- 超限，返回失败
    return {0, consumed, start_time, end_time}
else
    -- 未超限，扣减
    local new_consumed = redis.call('HINCRBY', key, 'consumed', quota)
    return {1, new_consumed, start_time, end_time}
end
```

---

### EC-05: 套餐生命周期边界

**测试ID**: EC-05
**优先级**: P0
**函数**: `TestEC05_SubscriptionLifecycleBoundary_ExactlyExpired`

**测试场景**:
- 创建并启用订阅
- 手动设置end_time=当前时间（刚好过期）
- 触发定时任务标记过期订阅
- 查询用户可用套餐

**验证点**:
1. ✓ 定时任务标记订阅为expired
2. ✓ 查询可用套餐时不包含已过期订阅
3. ✓ SQL查询条件：`status='active' AND end_time <= now`

**SQL逻辑**:
```sql
UPDATE subscriptions
SET status = 'expired'
WHERE status = 'active' AND end_time <= ?
```

---

### EC-06: 极小quota请求

**测试ID**: EC-06
**优先级**: P2
**函数**: `TestEC06_MinimalQuota_OneQuota`

**测试场景**:
- 请求1 quota（极小值）

**验证点**:
1. ✓ 请求成功
2. ✓ consumed精确为1
3. ✓ 系统不会因极小值而出现精度问题

---

### EC-07: 极大quota请求

**测试ID**: EC-07
**优先级**: P2
**函数**: `TestEC07_MaximalQuota_HundredMillion`

**测试场景**:
- 小时限额20M
- 请求100M quota（远超限额）

**验证点**:
1. ✓ 请求被拒绝（超限）
2. ✓ consumed为0（未扣减）
3. ✓ 系统不会因极大值而崩溃
4. ✓ Lua脚本正确处理大数值

---

### EC-08: 用户拥有0个套餐

**测试ID**: EC-08
**优先级**: P1
**函数**: `TestEC08_ZeroPackages_UseBalance`

**测试场景**:
- 用户无任何订阅
- 查询可用套餐
- 模拟请求

**验证点**:
1. ✓ 查询返回空列表
2. ✓ 系统应直接使用用户余额
3. ✓ 用户余额未被套餐逻辑影响

**业务逻辑**:
```go
packages := GetUserAvailablePackages(userId)
if len(packages) == 0 {
    // 直接使用用户余额
    return nil, nil, nil
}
```

---

### EC-09: 用户拥有20个套餐

**测试ID**: EC-09
**优先级**: P2
**函数**: `TestEC09_TwentyPackages_Performance`

**测试场景**:
- 创建20个不同优先级的套餐（20到1）
- 用户订阅所有套餐
- 查询可用套餐并测量耗时
- 验证优先级排序

**验证点**:
1. ✓ 返回所有20个套餐
2. ✓ 套餐按优先级降序排列（20, 19, 18, ..., 1）
3. ✓ 查询性能<50ms
4. ✓ 选择优先级最高的套餐使用

**性能要求**:
- 查询20个套餐耗时 < 50ms
- SQL优化：使用索引 `idx_user_status`
- JOIN优化：一次查询完成

---

### EC-10: 套餐quota=0

**测试ID**: EC-10
**优先级**: P1
**函数**: `TestEC10_PackageQuotaZero_OnlyCheckWindows`

**测试场景**:
- 套餐quota=0（月度不限制）
- 小时限额10M
- 模拟已消耗500M月度额度
- 请求5M

**验证点**:
1. ✓ 请求成功（不检查月度限额）
2. ✓ 仅检查小时窗口
3. ✓ GetSlidingWindowConfigs返回非空配置

**配置语义**:
- `quota=0` → 月度不限制
- `hourly_limit>0` → 小时仍限制

---

### EC-11: 所有limit都为0

**测试ID**: EC-11
**优先级**: P1
**函数**: `TestEC11_AllLimitsZero_OnlyCheckMonthly`

**测试场景**:
- 套餐quota=50M（仅月度限制）
- 所有滑动窗口limit=0
- 请求20M

**验证点**:
1. ✓ GetSlidingWindowConfigs返回空列表
2. ✓ 系统仅检查DB的total_consumed字段
3. ✓ 月度限额仍然生效

**配置语义**:
- `hourly_limit=0` → 小时不限制
- `quota>0` → 月度仍限制

---

### EC-12: Redis Key名称冲突

**测试ID**: EC-12
**优先级**: P2
**函数**: `TestEC12_RedisKeyConflict_Isolation`

**测试场景**:
- 创建两个独立的订阅（sub1, sub2）
- 为两个订阅分别创建窗口，消耗不同额度
- 修改sub1的窗口，验证sub2不受影响

**验证点**:
1. ✓ 两个订阅ID唯一
2. ✓ Redis Key唯一：`subscription:{id}:hourly:window`
3. ✓ sub1和sub2的consumed完全独立
4. ✓ 修改sub1不影响sub2
5. ✓ 无数据污染

**Key命名规范**:
```
subscription:{subscription_id}:{period}:window
```

## 测试通过标准

### P0级别（必须全部通过）
- EC-01: 窗口时间边界-刚好过期
- EC-02: 窗口时间边界-差1秒
- EC-03: 限额边界-刚好用尽
- EC-04: 限额边界-超1 quota
- EC-05: 套餐生命周期边界

**要求**: 0失败，0错误

### P1级别（通过率>95%）
- EC-08: 用户拥有0个套餐
- EC-10: 套餐quota=0
- EC-11: 所有limit都为0

**要求**: 最多1个失败

### P2级别（可选）
- EC-06: 极小quota请求
- EC-07: 极大quota请求
- EC-09: 用户拥有20个套餐
- EC-12: Redis Key名称冲突

**要求**: 尽力通过

## 常见问题排查

### 问题1: 时间边界测试失败

**现象**:
```
Expected new window start time to be greater, but got equal
```

**原因**:
- miniredis的FastForward可能未正确推进时间
- Lua脚本中的时间比较逻辑有误（应为 `>=` 而非 `>`）

**解决方案**:
检查Lua脚本：
```lua
if now >= end_time then  -- 注意：应该是 >=
    -- 删除并重建
end
```

### 问题2: 限额边界测试失败

**现象**:
```
Expected request to succeed but got failed
```

**原因**:
- PresetWindowState设置的consumed值不正确
- Lua脚本中的限额判断逻辑有误

**解决方案**:
检查Lua脚本：
```lua
if consumed + quota > limit then  -- 注意：应该是 >（大于才拒绝）
    return {0, consumed, start_time, end_time}
else
    -- 允许扣减
end
```

### 问题3: 性能测试失败（EC-09）

**现象**:
```
Query took 75ms, expected <50ms
```

**原因**:
- SQL查询未使用索引
- 测试环境数据库性能差

**解决方案**:
1. 确保索引存在：`idx_user_status (user_id, status)`
2. 使用EXPLAIN分析查询计划
3. 在真实环境（非内存DB）中重测

### 问题4: 函数未实现导致Skip

**现象**:
```
--- SKIP: TestEC05_SubscriptionLifecycleBoundary_ExactlyExpired
    GetUserActiveSubscriptions not implemented yet
```

**原因**:
- 服务层函数尚未实现

**解决方案**:
1. 实现 `model.GetUserActiveSubscriptions` 函数
2. 实现后重新运行测试

## 测试执行建议

### 建议执行顺序
1. 先运行P0级别测试（EC-01 ~ EC-05）
2. 再运行P1级别测试（EC-08, EC-10, EC-11）
3. 最后运行P2级别测试（EC-06, EC-07, EC-09, EC-12）

### 快速验证
```bash
# 仅运行P0测试
go test -v -run "TestEC0[1-5]"

# 仅运行时间边界测试
go test -v -run "TestEC0[12]"

# 仅运行限额边界测试
go test -v -run "TestEC0[34]"
```

## 依赖的辅助函数

### testutil.PresetWindowState
**用途**: 手动设置Redis窗口状态，用于测试边界条件
**位置**: `scene_test/testutil/boundary_edge_helper.go`

### testutil.CreateAlmostExpiredWindow
**用途**: 创建即将过期的窗口（还有N秒）
**位置**: `scene_test/testutil/boundary_edge_helper.go`

### testutil.CreateAlmostFullWindow
**用途**: 创建几乎用尽的窗口（还剩N quota）
**位置**: `scene_test/testutil/boundary_edge_helper.go`

### testutil.WindowExists
**用途**: 检查窗口是否存在
**位置**: `scene_test/testutil/boundary_edge_helper.go`

## 测试数据范围

### 时间范围
- 最小窗口：60秒（RPM）
- 最大窗口：604800秒（7天）
- 快进时间：1秒 ~ 4300秒

### 额度范围
- 最小值：1 quota
- 最大值：100000000 quota（100M）
- 边界值：limit - 1, limit, limit + 1

### 套餐数量
- 最小值：0个
- 最大值：20个
- 性能测试：20个

## 覆盖的代码路径

### Lua脚本分支
1. ✓ 窗口不存在 → 创建新窗口
2. ✓ 窗口存在且未过期 → 检查限额
3. ✓ 窗口存在但已过期 → 删除并重建
4. ✓ 限额检查通过 → HINCRBY扣减
5. ✓ 限额检查失败 → 返回0

### 服务层函数
1. ✓ service.CheckAndConsumeSlidingWindow
2. ✓ service.GetSlidingWindowConfigs
3. ○ model.GetUserActiveSubscriptions（待实现）

### 数据库操作
1. ✓ 查询订阅：`GetSubscriptionById`
2. ✓ 查询套餐：`GetPackageByID`
3. ✓ 批量更新状态：`UPDATE subscriptions SET status = 'expired'`
4. ✓ 原子递增：`HINCRBY`

## 测试覆盖率目标

| 类别 | 目标 | 实际 |
| :--- | :--- | :--- |
| **边界条件覆盖** | 100% | 100% (12/12用例) |
| **时间边界** | 2个场景 | ✓ EC-01, EC-02 |
| **限额边界** | 2个场景 | ✓ EC-03, EC-04 |
| **极端数值** | 2个场景 | ✓ EC-06, EC-07 |
| **套餐数量** | 2个场景 | ✓ EC-08, EC-09 |
| **配置边界** | 2个场景 | ✓ EC-10, EC-11 |

## 与其他测试套件的关系

```
boundary-edge-cases/          (本套件 - 边界与异常)
├── 补充 sliding-window/      (基础功能)
├── 补充 concurrency/          (并发场景)
├── 补充 priority-fallback/    (优先级逻辑)
└── 补充 billing/              (计费准确性)
```

**差异化定位**:
- `sliding-window/`: 关注正常流程和基础功能
- `boundary-edge-cases/`: 关注极端条件和边界情况
- 两者互补，共同确保系统鲁棒性

## 预期测试结果

运行测试后，预期输出：

```
=== RUN   TestEC01_WindowTimeBoundary_ExactlyExpired
    boundary_edge_test.go:85: EC-01: Testing window exactly at expiration boundary
    boundary_edge_test.go:116: EC-01: Test completed - Window expired exactly at boundary and rebuilt correctly
--- PASS: TestEC01_WindowTimeBoundary_ExactlyExpired (0.05s)

=== RUN   TestEC02_WindowTimeBoundary_OneSecondLeft
    boundary_edge_test.go:130: EC-02: Testing window with 1 second left before expiration
    boundary_edge_test.go:168: EC-02: Test completed - Window remained valid with 1 second left
--- PASS: TestEC02_WindowTimeBoundary_OneSecondLeft (0.03s)

...

PASS
ok      github.com/QuantumNous/new-api/scene_test/new-api-package/boundary-edge-cases  2.145s
```

## 参考资料

- **设计文档**: `docs/NewAPI-支持多种包月套餐-优化版.md`
- **测试方案**: `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 第2.10节
- **Lua脚本**: `service/check_and_consume_sliding_window.lua`
- **辅助函数**: `scene_test/testutil/boundary_edge_helper.go`
- **滑动窗口辅助**: `scene_test/testutil/sliding_window_helper.go`
