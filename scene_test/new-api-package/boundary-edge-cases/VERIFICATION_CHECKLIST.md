# 边界与异常场景测试 - 验证清单

## 测试用例验证清单

### ✅ EC-01: 窗口时间边界-刚好过期

**验证步骤**:
1. [ ] 创建60秒duration的窗口
2. [ ] 首次请求消耗2.5M，记录旧窗口start_time
3. [ ] FastForward 60秒（刚好过期）
4. [ ] 再次请求消耗3M
5. [ ] 验证新窗口start_time > 旧窗口start_time
6. [ ] 验证新窗口consumed=3M（重新计数）
7. [ ] 验证新窗口duration=60秒

**关键断言**:
```go
assert.Greater(t, result2.StartTime, oldStartTime, "Window should be rebuilt")
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 3000000)
```

**Lua脚本逻辑**:
```lua
if now >= end_time then
    redis.call('DEL', key)
    -- 创建新窗口
end
```

---

### ✅ EC-02: 窗口时间边界-差1秒

**验证步骤**:
1. [ ] 创建60秒duration的窗口
2. [ ] 首次请求消耗2.5M
3. [ ] FastForward 59秒（end_time = now + 1）
4. [ ] 再次请求消耗3M
5. [ ] 验证窗口时间未变
6. [ ] 验证consumed累加为5.5M

**关键断言**:
```go
assert.Equal(t, oldStartTime, result2.StartTime, "Window should not change")
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 5500000)
```

**边界判断**:
- `now >= end_time` → 过期（重建）
- `now < end_time` → 有效（累加）
- 临界点：`now = end_time - 1` → 有效

---

### ✅ EC-03: 限额边界-刚好用尽

**验证步骤**:
1. [ ] 创建limit=10000000的窗口
2. [ ] PresetWindowState设置consumed=9999999
3. [ ] 请求1 quota
4. [ ] 验证请求成功
5. [ ] 验证consumed=10000000

**关键断言**:
```go
testutil.AssertWindowResultSuccess(t, result, 10000000)
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 10000000)
```

**Lua脚本逻辑**:
```lua
if consumed + quota > limit then
    -- 超限
    return {0, ...}
else
    -- 9999999 + 1 = 10000000 <= 10000000 → 允许
end
```

---

### ✅ EC-04: 限额边界-超1 quota

**验证步骤**:
1. [ ] 创建limit=10000000的窗口
2. [ ] PresetWindowState设置consumed=10000000
3. [ ] 请求1 quota
4. [ ] 验证请求失败
5. [ ] 验证consumed保持10000000

**关键断言**:
```go
testutil.AssertWindowResultFailed(t, result, 10000000)
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 10000000)
```

**Lua脚本逻辑**:
```lua
if consumed + quota > limit then
    -- 10000000 + 1 = 10000001 > 10000000 → 拒绝
    return {0, consumed, start_time, end_time}
end
```

---

### ✅ EC-05: 套餐生命周期边界

**验证步骤**:
1. [ ] 创建并启用订阅
2. [ ] 手动设置end_time=当前时间
3. [ ] 执行SQL更新：`UPDATE subscriptions SET status='expired' WHERE end_time <= now`
4. [ ] 验证至少1条记录被更新
5. [ ] 查询用户可用套餐
6. [ ] 验证列表为空（已过期）

**关键SQL**:
```sql
UPDATE subscriptions
SET status = 'expired'
WHERE status = 'active' AND end_time <= ?
```

**依赖函数**:
- `model.GetUserActiveSubscriptions(userId, now)` - 待实现

---

### ✅ EC-06: 极小quota请求

**验证步骤**:
1. [ ] 创建窗口
2. [ ] 请求1 quota（极小值）
3. [ ] 验证请求成功
4. [ ] 验证consumed=1

**关键断言**:
```go
testutil.AssertWindowResultSuccess(t, result, 1)
testutil.AssertWindowConsumed(t, rm, subscriptionId, "hourly", 1)
```

---

### ✅ EC-07: 极大quota请求

**验证步骤**:
1. [ ] 创建小时限额20M的窗口
2. [ ] 请求100M quota（远超限额）
3. [ ] 验证请求被拒绝
4. [ ] 验证consumed=0或窗口未创建

**关键断言**:
```go
testutil.AssertWindowResultFailed(t, result, 0)
```

**边界值**:
- 请求值：100000000 (100M)
- 限额值：20000000 (20M)
- 倍数：5倍

---

### ✅ EC-08: 用户拥有0个套餐

**验证步骤**:
1. [ ] 仅创建用户，不创建套餐和订阅
2. [ ] 查询用户可用套餐
3. [ ] 验证返回空列表
4. [ ] 验证用户余额未变

**依赖函数**:
- `model.GetUserActiveSubscriptions(userId, now)` - 待实现

**预期行为**:
```go
packages := GetUserActiveSubscriptions(userId, now)
if len(packages) == 0 {
    // 降级到用户余额
}
```

---

### ✅ EC-09: 用户拥有20个套餐

**验证步骤**:
1. [ ] 创建20个优先级20~1的套餐
2. [ ] 用户订阅所有套餐
3. [ ] 查询可用套餐并测量耗时
4. [ ] 验证返回20个套餐
5. [ ] 验证按优先级降序排列
6. [ ] 验证耗时<50ms

**性能要求**:
- 查询耗时：< 50ms
- 排序正确：priority DESC
- 数据量：20个套餐

**关键断言**:
```go
assert.Equal(t, 20, len(packages))
assert.Less(t, elapsed, 50*time.Millisecond)
```

---

### ✅ EC-10: 套餐quota=0

**验证步骤**:
1. [ ] 创建quota=0, hourly_limit=10M的套餐
2. [ ] 模拟已消耗500M月度额度
3. [ ] 请求5M
4. [ ] 验证请求成功（月度不限制）
5. [ ] 验证小时窗口正常扣减

**配置语义**:
```
quota = 0      → 月度不限制
hourly_limit > 0 → 小时仍限制
```

**依赖函数**:
- `service.GetSlidingWindowConfigs(pkg)` - 已实现

---

### ✅ EC-11: 所有limit都为0

**验证步骤**:
1. [ ] 创建quota=50M, 所有limit=0的套餐
2. [ ] 调用GetSlidingWindowConfigs
3. [ ] 验证返回空列表
4. [ ] 验证月度限额仍生效

**配置语义**:
```
hourly_limit = 0  → 小时不限制
daily_limit = 0   → 日不限制
quota > 0         → 月度仍限制
```

**预期行为**:
```go
configs := GetSlidingWindowConfigs(pkg)
// configs 应为空，因为所有 limit > 0 的条件都不满足
assert.Empty(t, configs)
```

---

### ✅ EC-12: Redis Key名称冲突

**验证步骤**:
1. [ ] 创建两个独立订阅sub1, sub2
2. [ ] 验证sub1.Id != sub2.Id
3. [ ] 为sub1创建窗口，消耗5M
4. [ ] 为sub2创建窗口，消耗8M
5. [ ] 验证两个窗口独立
6. [ ] 修改sub1窗口，验证sub2不受影响

**Key命名规范**:
```
subscription:{subscription_id}:hourly:window
```

**隔离性验证**:
```go
key1 := fmt.Sprintf("subscription:%d:hourly:window", sub1.Id)
key2 := fmt.Sprintf("subscription:%d:hourly:window", sub2.Id)
assert.NotEqual(t, key1, key2)

// sub1: consumed=8M (5M+3M)
// sub2: consumed=8M (不变)
```

---

## 测试前置条件检查

### 必需组件
- [ ] miniredis v2 已安装
- [ ] SQLite支持（CGO_ENABLED=1）
- [ ] testutil包已实现所有辅助函数
- [ ] service.CheckAndConsumeSlidingWindow已实现
- [ ] Lua脚本已编写并可加载

### 可选组件（影响部分测试）
- [ ] model.GetUserActiveSubscriptions - EC-05, EC-08, EC-09依赖
- [ ] service.GetSlidingWindowConfigs - EC-10, EC-11依赖

## 测试后验证

### 检查Redis状态
```go
// 查看所有窗口Key
keys := rm.Server.Keys()
for _, key := range keys {
    t.Logf("Key: %s", key)
}

// 查看特定窗口
testutil.DumpWindowInfo(t, rm, subscriptionId, "hourly")
```

### 检查数据库状态
```go
// 查看订阅状态
sub, _ := model.GetSubscriptionById(subscriptionId)
t.Logf("Subscription: id=%d, status=%s, consumed=%d",
    sub.Id, sub.Status, sub.TotalConsumed)

// 查看套餐配置
pkg, _ := model.GetPackageByID(sub.PackageId)
t.Logf("Package: quota=%d, hourly_limit=%d",
    pkg.Quota, pkg.HourlyLimit)
```

## 覆盖率目标

### 代码覆盖率
- service/package_sliding_window.go: 90%+
- service/check_and_consume_sliding_window.lua: 100% (所有分支)

### 场景覆盖率
- 时间边界：2/2（100%）
- 限额边界：2/2（100%）
- 极端数值：2/2（100%）
- 套餐数量：2/2（100%）
- 配置边界：2/2（100%）
- 数据隔离：1/1（100%）

### 总体覆盖率
- 测试用例数：12个
- 验证点数：50+个
- 边界条件覆盖：100%

## 失败场景预期

### 预期失败（Skip）
如果以下函数未实现，相关测试会skip：
- EC-05: `model.GetUserActiveSubscriptions` 未实现
- EC-08: `model.GetUserActiveSubscriptions` 未实现
- EC-09: `model.GetUserActiveSubscriptions` 未实现

### 真实失败（需修复）
如果以下测试失败，说明存在缺陷：
- EC-01, EC-02: Lua脚本时间判断逻辑错误
- EC-03, EC-04: Lua脚本限额判断逻辑错误
- EC-06, EC-07: 极端值处理有问题
- EC-12: Redis Key隔离性问题

## 测试报告模板

测试完成后，应生成如下报告：

```
========================================
边界与异常场景测试报告
========================================
执行时间: 2025-12-12 15:30:00
测试环境: Windows/SQLite/miniredis

测试结果汇总:
- 总用例数: 12
- 通过: 10
- 失败: 0
- 跳过: 2 (EC-05, EC-08 - 依赖函数未实现)

P0级别: 5/5 通过 ✓
P1级别: 3/3 通过 ✓
P2级别: 4/4 通过 ✓

性能测试:
- EC-09 (20个套餐查询): 38ms ✓ (目标<50ms)

边界条件验证:
- 时间边界: 2/2 通过 ✓
- 限额边界: 2/2 通过 ✓
- 极端数值: 2/2 通过 ✓
- 配置边界: 2/2 通过 ✓

详细结果见下...
```

## 集成CI/CD

### GitHub Actions配置示例
```yaml
name: Boundary Edge Cases Tests

on:
  push:
    paths:
      - 'service/package_sliding_window.go'
      - 'service/check_and_consume_sliding_window.lua'
      - 'scene_test/new-api-package/boundary-edge-cases/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Run Boundary Tests
        run: |
          cd scene_test/new-api-package/boundary-edge-cases
          go test -v -timeout 10m
```

### 本地快速验证
```bash
# 运行P0测试（CI/CD门禁）
go test -v -run "TestEC0[1-5]" ./scene_test/new-api-package/boundary-edge-cases

# 生成覆盖率报告
go test -v -coverprofile=coverage.out ./scene_test/new-api-package/boundary-edge-cases
go tool cover -html=coverage.out -o coverage.html
```

## 测试数据快速参考

### 时间维度
| 窗口类型 | Duration | TTL | 用于测试 |
| :--- | :--- | :--- | :--- |
| RPM | 60秒 | 90秒 | EC-06 |
| Hourly | 3600秒 | 4200秒 | EC-01~04 |
| Daily | 86400秒 | 93600秒 | EC-10 |

### 额度范围
| 场景 | Quota值 | Limit值 | 用于测试 |
| :--- | :--- | :--- | :--- |
| 极小值 | 1 | 20M | EC-06 |
| 极大值 | 100M | 20M | EC-07 |
| 边界-刚好 | 1 | 10M | EC-03 |
| 边界-超1 | 1 | 10M | EC-04 |

### 套餐配置
| 场景 | Quota | Limits | 用于测试 |
| :--- | :--- | :--- | :--- |
| 标准套餐 | 100M | 20M/150M/500M | EC-01~04 |
| 无月度限制 | 0 | 10M | EC-10 |
| 仅月度限制 | 50M | 全为0 | EC-11 |

---

## 调试技巧

### 1. 打印窗口详细信息
```go
testutil.DumpWindowInfo(t, rm, subscriptionId, "hourly")
// 输出:
// Key: subscription:123:hourly:window
// Fields: start_time=1702388310, end_time=1702391910, consumed=5500000, limit=20000000
// TTL: 3800s
```

### 2. 检查时间推进
```go
before := time.Now().Unix()
rm.FastForward(60 * time.Second)
after := rm.Server.Now().Unix()
t.Logf("Time advanced: %d seconds", after-before)
```

### 3. 验证Lua脚本执行
```go
// 在service代码中添加日志
common.SysLog(fmt.Sprintf("Lua script input: now=%d, duration=%d, limit=%d, quota=%d",
    now, config.Duration, config.Limit, quota))
common.SysLog(fmt.Sprintf("Lua script output: status=%d, consumed=%d",
    status, consumed))
```

### 4. 手动验证Redis状态
```go
// 在测试中直接查询miniredis
fields, _ := rm.Server.HGetAll(key)
for k, v := range fields {
    t.Logf("%s: %s", k, v)
}
```

---

## 回归测试清单

在修改以下代码后，必须重新运行本测试套件：

- [ ] `service/package_sliding_window.go` - 滑动窗口核心逻辑
- [ ] `service/check_and_consume_sliding_window.lua` - Lua脚本
- [ ] `model/subscription.go` - 订阅模型和查询
- [ ] `model/package.go` - 套餐模型和查询

## 已知限制

1. **时间模拟精度**: miniredis的FastForward可能有1-2秒的误差
2. **性能测试环境**: 内存数据库性能不代表真实环境
3. **并发测试规模**: 100个goroutine可能不足以触发真实并发问题

## 改进建议

1. **增加压力测试**: EC-09可扩展到100个套餐
2. **增加时区测试**: 测试跨时区的窗口行为
3. **增加闰秒处理**: 测试时间跳变场景
4. **增加浮点精度测试**: 测试quota计算的精度问题
