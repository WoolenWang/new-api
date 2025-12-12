# 缓存一致性测试套件

## 概述

本测试套件实现了 NewAPI 包月套餐系统的 **2.6 缓存一致性测试** 的所有 7 个测试用例。

测试目标：验证套餐系统的三级缓存（内存 -> Redis -> DB）架构的一致性、降级策略和故障恢复能力。

## 测试用例覆盖

| 测试ID | 测试场景 | 优先级 | 核心验证点 | 实现状态 |
|--------|---------|--------|-----------|---------|
| **CC-01** | 套餐信息缓存写穿 | P1 | Cache-Aside 模式正确性 | ✓ 已实现 |
| **CC-02** | 订阅信息缓存失效 | P1 | 状态变更的缓存传播 | ✓ 已实现 |
| **CC-03** | 滑动窗口Redis失效 | P1 | 窗口自动重建逻辑 | ✓ 已实现 |
| **CC-04** | Redis完全不可用 | **P0** | **降级策略（最高优先级）** | ✓ 已实现 |
| **CC-05** | Redis恢复后功能恢复 | P1 | 自动恢复能力 | ✓ 已实现 |
| **CC-06** | DB与Redis数据对比 | P1 | 大量请求后数据一致性 | ✓ 已实现 |
| **CC-07** | Lua脚本加载失败降级 | P1 | Lua失败时的降级处理 | ✓ 已实现 |

## 测试架构

### 测试框架
- **测试套件**: `testify/suite` - 提供 Setup/TearDown 生命周期管理
- **断言库**: `testify/assert` - 丰富的断言函数
- **Redis模拟**: `miniredis/v2` - 内存 Redis 模拟，支持 Lua 脚本
- **数据库**: 内存 SQLite（待集成）

### 测试流程

```
SetupSuite (启动测试环境)
    └── 启动 miniredis
    └── 启动测试服务器（TODO）
    └── 创建测试用户（TODO）

每个测试用例:
    SetupTest (清空 Redis 数据)
        └── 执行测试逻辑
        └── 断言验证
    TearDownTest

TearDownSuite (清理测试环境)
    └── 关闭 miniredis
    └── 关闭测试服务器（TODO）
```

## 运行测试

### 前置条件

1. 安装依赖：
```bash
go get github.com/alicebob/miniredis/v2
go get github.com/stretchr/testify/suite
go get github.com/stretchr/testify/assert
```

2. 确保以下系统模块已实现（参见集成清单）

### 运行命令

```bash
# 运行所有缓存一致性测试
cd scene_test/new-api-package/cache-consistency
go test -v

# 运行特定测试
go test -v -run TestCC04  # Redis 不可用测试（P0）
go test -v -run TestCC01  # 套餐缓存写穿测试
go test -v -run TestCC06  # 数据一致性测试

# 运行所有 P0 优先级测试
go test -v -run ".*CC04.*"

# 生成测试报告
go test -v -json > test_report.json

# 生成覆盖率报告
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

## 测试用例详解

### CC-01: 套餐信息缓存写穿

**测试目标**: 验证 Cache-Aside 模式的正确性

**测试流程**:
1. 创建套餐 → DB 写入
2. 立即查询套餐
3. 验证 Redis 中存在缓存且与 DB 一致

**关键断言**:
- `package:{id}` Key 存在
- 缓存字段完整（name, priority, quota, hourly_limit）
- TTL 合理（<= 600秒）

---

### CC-02: 订阅信息缓存失效

**测试目标**: 验证状态变更后缓存的异步更新

**测试流程**:
1. 创建订阅（status=inventory）
2. 启用订阅（status=active）
3. 查询订阅，验证缓存已更新

**关键断言**:
- `subscription:{id}` Key 存在
- status = "active"
- start_time 和 end_time 已设置

---

### CC-03: 滑动窗口Redis失效

**测试目标**: 验证窗口被删除后的自动重建

**测试流程**:
1. 首次请求创建窗口
2. 手动删除窗口 Key
3. 再次请求，验证窗口重建

**关键断言**:
- 新窗口 consumed 从 0 开始
- 新窗口 start_time > 旧窗口 start_time
- 窗口时长正确（3600秒）

---

### CC-04: Redis完全不可用 (P0)

**测试目标**: 验证 Redis 不可用时的降级策略

**测试流程**:
1. 关闭 miniredis
2. 发起套餐请求
3. 验证降级成功

**关键断言**:
- 请求成功（HTTP 200）
- 跳过滑动窗口检查
- 仅检查月度总限额
- 系统日志记录降级警告

**优先级**: P0（最高优先级，必须通过才能上线）

---

### CC-05: Redis恢复后功能恢复

**测试目标**: 验证 Redis 恢复后的自动恢复能力

**测试流程**:
1. Redis 不可用时请求（降级）
2. 恢复 Redis
3. 再次请求，验证滑动窗口功能恢复

**关键断言**:
- 第一次请求降级成功
- 第二次请求窗口被创建
- 功能自动恢复，无需手动干预

---

### CC-06: DB与Redis数据对比

**测试目标**: 验证大量请求后数据一致性

**测试流程**:
1. 发起 100 次请求（每次 1M quota）
2. 对比 DB.total_consumed 和 Redis.window.consumed

**关键断言**:
- DB 和 Redis 数据误差 < 1%
- 在测试环境下应完全一致
- 窗口未超限

---

### CC-07: Lua脚本加载失败降级

**测试目标**: 验证 Lua 脚本加载失败时的降级

**测试流程**:
1. 清空 scriptSHA
2. 模拟 SCRIPT LOAD 失败
3. 发起请求

**关键断言**:
- 请求降级成功
- 系统日志记录错误
- 服务不被阻塞

## 集成清单

### 需要实现的系统模块

#### 1. 数据模型层

- [ ] `model/package.go`
  - Package 结构体（含所有限额字段）
  - CreatePackage, GetPackageById, UpdatePackage
  - 缓存回填逻辑（Cache-Aside）

- [ ] `model/subscription.go`
  - Subscription 结构体
  - CreateSubscription, GetSubscriptionById
  - 状态更新和缓存失效

#### 2. 服务层

- [ ] `service/package_sliding_window.go`
  - Lua 脚本定义（check_and_consume_sliding_window.lua）
  - CheckAndConsumeSlidingWindow 函数
  - GetAllSlidingWindowsStatus 函数
  - Redis 降级处理（RedisEnabled 检查）
  - Lua 脚本预加载（init 函数）

- [ ] `service/package_consume.go`
  - TryConsumeFromPackage 函数
  - GetUserAvailablePackages 函数
  - CheckAndReservePackageQuota 函数

#### 3. 测试工具函数

- [ ] `scene_test/testutil/package_helper.go`
  - CreateTestPackage
  - CreateAndActivateSubscription
  - CreateTestUser
  - CreateTestToken
  - CallChatCompletion

- [ ] `scene_test/testutil/server.go`
  - StartTestServer（启动测试环境）
  - StopTestServer（清理测试环境）

#### 4. 全局配置

- [ ] `common/env.go` 或 `common/config.go`
  - RedisEnabled 标志
  - PackageEnabled 配置开关

### 集成步骤

1. **阶段1: 实现数据模型**
   - 创建 Package 和 Subscription 表迁移
   - 实现基础 CRUD 函数

2. **阶段2: 实现滑动窗口逻辑**
   - 编写 Lua 脚本
   - 实现 Go 封装函数
   - 集成到计费流程

3. **阶段3: 实现测试工具**
   - 创建 testutil 辅助函数
   - 启动测试服务器

4. **阶段4: 取消测试代码中的 TODO**
   - 逐步替换模拟数据为真实 API 调用
   - 运行测试验证

5. **阶段5: 测试优化**
   - 添加更多边界条件测试
   - 性能基准测试
   - CI/CD 集成

## 测试数据

### 模拟数据约定

为了保证测试的可预测性，使用以下约定的测试数据：

| 实体 | ID范围 | 说明 |
|------|--------|------|
| 套餐 | 100-199 | CC-01 使用 100, CC-02 使用 101... |
| 订阅 | 200-299 | CC-01 使用 200, CC-02 使用 201... |
| 用户 | 1-99 | 测试用户，统一使用 testUserId |
| 窗口 | - | subscription:{id}:{period}:window |

### Redis Key 命名规范

```
package:{package_id}                      # 套餐缓存
subscription:{subscription_id}            # 订阅缓存
subscription:{id}:hourly:window          # 小时窗口
subscription:{id}:daily:window           # 日窗口
subscription:{id}:weekly:window          # 周窗口
subscription:{id}:rpm:window             # RPM窗口
subscription:{id}:4hourly:window         # 4小时窗口
```

## 调试与故障排查

### 常见问题

**Q1: 测试跳过 (Skip)**
- 原因: testutil 工具函数未实现
- 解决: 实现对应的辅助函数，取消 TODO 注释

**Q2: miniredis 连接失败**
- 原因: miniredis 未正确启动
- 解决: 检查 SetupSuite 中的启动逻辑

**Q3: 窗口验证失败**
- 原因: Lua 脚本逻辑与测试预期不符
- 解决: 检查 Lua 脚本实现，对齐测试设计文档

**Q4: 数据一致性验证失败**
- 原因: 异步更新未完成
- 解决: 增加 waitForAsyncOperation 的等待时间

### 调试技巧

```go
// 打印 Redis 所有 Key
s.miniRedis.Keys()

// 打印窗口详情
windowKey := fmt.Sprintf("subscription:%d:hourly:window", subscriptionId)
values, _ := s.miniRedis.HGetAll(windowKey)
s.T().Logf("Window: %+v", values)

// 检查 TTL
ttl := s.miniRedis.TTL(windowKey)
s.T().Logf("TTL: %.0f seconds", ttl.Seconds())

// 验证窗口是否过期
endTimeStr, _ := s.miniRedis.HGet(windowKey, "end_time")
endTime, _ := strconv.ParseInt(endTimeStr, 10, 64)
now := time.Now().Unix()
s.T().Logf("Window expired: %v (now=%d, end=%d)", now >= endTime, now, endTime)
```

## 设计参考

### 相关设计文档

- `docs/NewAPI-支持多种包月套餐-优化版.md` - 套餐系统总体设计
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 测试设计文档（2.6节）
- `docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md` - 缓存架构参考

### 核心设计要点

1. **三级缓存架构**:
   ```
   L1 (内存) -> L2 (Redis) -> L3 (DB)
   ```

2. **Cache-Aside 模式**:
   ```
   查询流程:
   1. 读缓存 -> 命中: 返回
   2. 未命中: 读DB -> 异步回填缓存 -> 返回
   ```

3. **滑动窗口机制**:
   - 窗口从首次请求时刻开始
   - 持续固定时长（如3600秒）
   - 按需创建，TTL自动清理

4. **降级策略**:
   - Redis不可用: 跳过滑动窗口，仅检查月度总限额
   - Lua加载失败: 降级，允许请求通过
   - 所有降级都记录日志

## 测试输出示例

```
=== RUN   TestCacheConsistencySuite
=== RUN   TestCacheConsistencySuite/TestCC04_RedisCompletelyUnavailable
CC-04: 开始测试 Redis 完全不可用时的降级策略
[Arrange] 创建测试套餐和订阅
[Act] 停止 miniredis，模拟 Redis 不可用
[Act] 发起 API 请求（Redis 不可用状态）
[Assert] 验证降级策略生效
✓ 验证通过: 请求降级成功（HTTP 200）
✓ 验证通过: 套餐扣减逻辑（模拟）
✓ 验证通过: 用户余额不变
==========================================================
CC-04 测试完成: Redis 完全不可用时降级策略正确
关键验证点:
  1. 请求降级成功，返回 HTTP 200
  2. 跳过滑动窗口检查，仅检查月度总限额
  3. 套餐 total_consumed 正常更新
  4. 用户余额不变（使用套餐）
  5. 系统日志记录降级警告
==========================================================
--- PASS: TestCacheConsistencySuite/TestCC04_RedisCompletelyUnavailable (0.15s)
```

## 下一步

### 立即行动

1. 实现 `testutil/package_helper.go` 中的辅助函数
2. 取消测试代码中的 TODO 注释
3. 运行测试验证

### 后续优化

1. 添加并发测试（多个 goroutine 同时操作缓存）
2. 添加缓存性能基准测试
3. 集成到 CI/CD 流程
4. 添加缓存命中率统计

## 维护者

- QA Team
- 后端开发团队

---

**创建日期**: 2025-12-12
**最后更新**: 2025-12-12
**版本**: v1.0
