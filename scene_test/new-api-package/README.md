# NewAPI 包月套餐测试套件

## 概述

本目录包含NewAPI包月套餐功能的完整测试套件，覆盖滑动时间窗口、计费准确性、优先级降级、P2P权限等核心功能。

## 目录结构

```
scene_test/new-api-package/
├── sliding-window/                    # 滑动时间窗口核心测试
│   ├── window_basic_test.go          # SW-01 ~ SW-10 基础测试
│   ├── window_concurrency_test.go    # SW-10 并发扩展测试
│   └── README.md                      # 滑动窗口测试说明
│
├── lifecycle/                         # 套餐生命周期测试 (待实现)
│   └── lifecycle_test.go              # LC-01 ~ LC-09
│
├── billing/                           # 计费准确性测试 (待实现)
│   ├── billing_accuracy_test.go      # BA-01 ~ BA-05
│   └── billing_exception_test.go     # BA-06 ~ BA-09
│
├── priority-fallback/                 # 优先级与Fallback测试 (待实现)
│   └── priority_test.go               # PF-01 ~ PF-09
│
└── README.md                          # 本文件
```

## 当前实现状态

### ✅ 已完成 (2.2 滑动时间窗口核心测试)
- **测试用例数**: 10个（7个P0 + 3个P1）
- **测试文件**:
  - `sliding-window/window_basic_test.go` - 基础功能测试
  - `sliding-window/window_concurrency_test.go` - 并发扩展测试
- **辅助工具**:
  - `testutil/redis_mock.go` - miniredis封装
  - `testutil/sliding_window_helper.go` - 滑动窗口专用断言

### ⏳ 待实现
- 2.1 套餐生命周期测试 (LC-01 ~ LC-09)
- 2.4 计费准确性专项测试 (BA-01 ~ BA-09)
- 2.3 套餐优先级与Fallback测试 (PF-01 ~ PF-09)
- 2.5 正交配置矩阵测试 (OM-01 ~ OM-08)
- 2.6 缓存一致性测试 (CC-01 ~ CC-07)
- 2.7 计费与路由组合测试 (BR-01 ~ BR-04)
- 2.8 并发与数据竞态测试 (CR-01 ~ CR-06)

## 运行测试

### 运行所有套餐测试
```bash
cd scene_test/new-api-package
go test -v ./...
```

### 仅运行滑动窗口测试
```bash
cd scene_test/new-api-package/sliding-window
go test -v
```

### 运行特定测试
```bash
# 运行SW-01测试
go test -v -run TestSW01_FirstRequest_CreatesWindow

# 运行所有P0测试
go test -v -run "TestSW0[1-4|6-7]|TestSW10"

# 运行并发测试
go test -v -run ".*Concurrency.*"

# 跳过压力测试
go test -v -short
```

### 性能基准测试
```bash
go test -bench=BenchmarkSW10 -benchmem
```

## 测试依赖

### Go包依赖
```bash
go get github.com/alicebob/miniredis/v2
go get github.com/go-redis/redis/v8
go get github.com/stretchr/testify/assert
```

### 环境要求
- Go 1.18+
- SQLite支持 (CGO_ENABLED=1)
- Redis (通过miniredis模拟)

## 关键设计文档

### 设计文档
- `docs/NewAPI-支持多种包月套餐-优化版.md` - 套餐系统总体设计
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 测试方案详细说明

### 核心代码
- `service/package_sliding_window.go` - 滑动窗口实现
- `service/check_and_consume_sliding_window.lua` - Redis Lua脚本
- `model/package.go` - 套餐数据模型
- `model/subscription.go` - 订阅数据模型

## 测试原理

### 滑动窗口机制
滑动窗口从用户首次请求时刻开始，持续固定时长：

```
用户在 15:58:30 首次请求小时窗口：
├── start: 15:58:30
├── end:   16:58:29 (start + 3600秒)
└── 保证完整60分钟可用

窗口过期后（16:58:30），下次请求创建新窗口：
├── start: 17:05:00 (下次请求时刻)
├── end:   18:04:59
```

### Lua脚本原子操作
```lua
-- 单个原子操作完成：
1. 检查窗口是否存在
2. 如果不存在 → 创建窗口
3. 如果存在 → 检查是否过期
4. 如果过期 → 删除旧窗口，创建新窗口
5. 如果有效 → 检查限额
6. 如果未超限 → 原子扣减
7. 返回结果
```

### 多维度时间窗口
| 维度 | Duration | TTL | 单位 | 用途 |
|------|----------|-----|------|------|
| RPM | 60秒 | 90秒 | 请求数 | 防止突发流量 |
| Hourly | 3600秒 | 4200秒 | quota | 小时级限流 |
| 4Hourly | 14400秒 | 18000秒 | quota | 跨度限流 |
| Daily | 86400秒 | 93600秒 | quota | 日级限流 |
| Weekly | 604800秒 | 691200秒 | quota | 周级限流 |

## 故障排查

### 常见问题

**1. miniredis启动失败**
```
Error: Failed to start miniredis
解决: go get github.com/alicebob/miniredis/v2
```

**2. 时间误差导致断言失败**
```
Error: Window duration should be 3600 (actual: 3601)
原因: 时间计算存在微小误差
解决: 使用assert.InDelta允许误差
```

**3. 并发测试结果不稳定**
```
Error: Should have exactly 50 successful requests, got 49
原因: 极端情况下并发调度可能有微小差异
解决: 使用atomic计数器，增加并发量以稳定统计
```

**4. Lua脚本返回值解析失败**
```
Error: Lua script returned invalid result
原因: miniredis Lua支持可能与真实Redis有差异
解决: 检查Lua脚本语法，使用标准Redis命令
```

## 调试技巧

### 打印窗口详细信息
```go
testutil.DumpWindowInfo(t, rm, subscriptionId, "hourly")
```

### 检查所有Redis Key
```go
keys := rm.Server.Keys()
for _, key := range keys {
    t.Logf("Redis Key: %s", key)
}
```

### 手动验证Lua脚本
```go
// 直接调用Lua脚本并打印返回值
result, _ := rm.Client.EvalSha(ctx, scriptSHA, []string{key}, args...).Result()
t.Logf("Lua result: %+v", result)
```

## 测试最佳实践

### 1. 测试隔离
每个测试用例都应该：
- 使用独立的Redis实例（miniredis）
- 清理所有测试数据（teardownTest）
- 不依赖其他测试的执行顺序

### 2. 原子性验证
并发测试应：
- 使用`sync/atomic`进行计数
- 验证最终一致性
- 允许合理的并发调度误差

### 3. 时间处理
时间相关测试应：
- 使用`rm.FastForward()`快进时间
- 允许1秒误差（assert.InDelta）
- 记录关键时间戳用于调试

## 贡献指南

### 添加新测试
1. 在对应目录创建`*_test.go`文件
2. 遵循命名规范: `Test{TestID}_{Scenario}`
3. 添加完整文档注释
4. 使用AAA模式（Arrange-Act-Assert）
5. 在README中更新测试列表

### Code Review要点
- ✅ 测试用例是否覆盖设计文档的所有验证点
- ✅ 断言是否充分（覆盖正常、异常、边界）
- ✅ 是否正确清理测试数据
- ✅ 并发测试是否使用atomic计数器
- ✅ 时间相关断言是否允许合理误差

## 维护者
- QA Team
- 后端开发团队

---

**最后更新**: 2025-12-12
**测试框架版本**: v1.0
**覆盖测试方案**: NewAPI-支持多种包月套餐-优化版-测试方案.md (第2.2章节)
