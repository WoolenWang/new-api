# 滑动时间窗口核心测试套件

## 概述

本测试套件实现了 `NewAPI-支持多种包月套餐-优化版-测试方案.md` 第 **2.2 章节** 的所有测试用例，专注于验证滑动时间窗口（Sliding Window）机制的核心功能。

## 测试覆盖范围

### 核心验证点
- ✅ **窗口生命周期管理**：创建、累加、超限、过期、重建
- ✅ **原子性保证**：Lua脚本确保并发场景下无TOCTOU竞态
- ✅ **多维度独立**：RPM/小时/日/周窗口互不干扰
- ✅ **特殊处理**：RPM按请求数计数，其他按quota
- ✅ **资源优化**：无请求不创建Redis Key

## 测试用例列表

| 测试ID | 测试场景 | 优先级 | 实现函数 |
|--------|---------|--------|---------|
| **SW-01** | 首次请求创建窗口 | P0 | `TestSW01_FirstRequest_CreatesWindow` |
| **SW-02** | 窗口内扣减累加 | P0 | `TestSW02_WithinWindow_Accumulates` |
| **SW-03** | 窗口超限拒绝 | P0 | `TestSW03_Exceeded_Rejects` |
| **SW-04** | 窗口过期自动重建 | P0 | `TestSW04_Expired_Rebuilds` |
| **SW-05** | 窗口TTL自动清理 | P1 | `TestSW05_TTL_AutoCleanup` |
| **SW-06** | RPM特殊处理 | P0 | `TestSW06_RPM_SpecialHandling` |
| **SW-07** | 多维度独立滑动 | P0 | `TestSW07_MultiDimension_IndependentSliding` |
| **SW-08** | 4小时窗口跨度 | P1 | `TestSW08_FourHourly_CrossesMidnight` |
| **SW-09** | 无请求不创建Key | P1 | `TestSW09_NoRequest_NoKeyCreated` |
| **SW-10** | Lua脚本原子性 | P0 | `TestSW10_LuaAtomic_Concurrency` |

**总计**: 10个测试用例 (7个P0, 3个P1)

## 技术架构

### 核心组件
- **miniredis**: 内存Redis模拟，完整支持Lua脚本
- **内存SQLite**: 隔离的数据库环境
- **Lua脚本**: `service/check_and_consume_sliding_window.lua`
- **业务逻辑**: `service/package_sliding_window.go`

### 依赖的辅助工具
- `testutil/redis_mock.go` - miniredis封装和Redis操作工具
- `testutil/sliding_window_helper.go` - 滑动窗口专用断言函数
- `testutil/package_helper.go` - 套餐和订阅创建工具

## 运行测试

### 运行所有滑动窗口测试
```bash
cd scene_test/new-api-package/sliding-window
go test -v ./...
```

### 运行特定测试
```bash
# 运行SW-01测试
go test -v -run TestSW01_FirstRequest_CreatesWindow

# 运行所有P0优先级测试
go test -v -run "TestSW0[1-4|6-7]|TestSW10"

# 运行并发测试
go test -v -run TestSW10_LuaAtomic_Concurrency
```

### 生成覆盖率报告
```bash
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## 测试数据说明

### 默认套餐配置（setupTest函数）
- **月度总限额**: 100M quota
- **小时限额**: 20M quota
- **每日限额**: 150M quota
- **每周限额**: 500M quota
- **RPM限制**: 60 requests/minute
- **优先级**: 15

### 默认用户配置
- **系统分组**: default
- **余额**: 10M quota
- **状态**: 启用

## 关键验证逻辑

### SW-01: 首次请求创建窗口
```go
// 验证点：
1. Redis Hash Key被创建
2. start_time = now (当前时间)
3. end_time = start_time + duration
4. consumed = estimatedQuota
5. TTL被正确设置
6. Lua返回status=1
```

### SW-03: 窗口超限拒绝
```go
// 验证点：
1. 首次请求8M（成功）
2. 再次请求5M（8+5=13M > 10M限额）
3. 第二次请求返回Success=false
4. consumed保持8M（未增加）
5. 窗口状态不变
```

### SW-10: 并发原子性
```go
// 验证点：
1. 100个goroutine并发请求
2. 限额10M，每次0.2M
3. 理论成功数=50次 (10M/0.2M)
4. consumed严格等于成功数×0.2M
5. 无超额扣减（TOCTOU竞态）
```

## Redis Key结构

### 窗口Key格式
```
subscription:{subscription_id}:{period}:window
```

### Hash字段
| 字段 | 类型 | 说明 |
|------|------|------|
| `start_time` | int64 | 窗口开始时间（Unix秒） |
| `end_time` | int64 | 窗口结束时间 |
| `consumed` | int64 | 已消耗量 |
| `limit` | int64 | 限额 |

### 示例
```
Key: subscription:123:hourly:window
Hash:
  start_time: 1702388310
  end_time: 1702391910
  consumed: 8500000
  limit: 20000000
TTL: 4200秒
```

## 故障排查

### 常见问题

**1. miniredis连接失败**
```
Error: Failed to start miniredis
解决: 检查是否安装了github.com/alicebob/miniredis/v2
```

**2. Lua脚本未加载**
```
Error: Lua script execution failed
解决: 确保service/check_and_consume_sliding_window.lua存在
     检查go:embed指令是否正确
```

**3. 窗口时间不匹配**
```
Error: Window duration should be 3600 seconds (actual: 3601)
原因: 时间计算存在微小误差
解决: 使用assert.InDelta允许1秒误差
```

**4. 并发测试不稳定**
```
Error: Should have exactly 50 successful requests
原因: 并发调度导致结果不确定
解决: 增加并发请求数量（100+）以稳定统计规律
```

### 调试技巧

**打印窗口详细信息**
```go
testutil.DumpWindowInfo(t, rm, subscriptionId, "hourly")
```

**手动检查Redis状态**
```go
keys := rm.Server.Keys()
for _, key := range keys {
    t.Logf("Redis Key: %s", key)
}
```

**验证Lua脚本逻辑**
```go
// 在miniredis中直接执行Lua脚本并打印返回值
result, err := rm.Client.EvalSha(ctx, scriptSHA, keys, args...).Result()
t.Logf("Lua result: %+v", result)
```

## 参考文档

### 设计文档
- `docs/NewAPI-支持多种包月套餐-优化版.md` - 滑动窗口核心设计
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 测试方案详细说明（第2.2章节）

### 相关代码
- `service/package_sliding_window.go` - 滑动窗口核心逻辑
- `service/check_and_consume_sliding_window.lua` - Redis Lua脚本
- `model/package.go` - 套餐数据模型
- `model/subscription.go` - 订阅数据模型

## 贡献指南

### 添加新测试用例
1. 在 `window_basic_test.go` 中添加新的测试函数
2. 遵循命名规范: `TestSW{ID}_{Scenario}`
3. 添加完整注释（测试ID、优先级、场景、预期结果）
4. 使用AAA模式（Arrange-Act-Assert）

### 测试用例模板
```go
// ============================================================================
// SW-XX: 测试场景名称
// 测试ID: SW-XX
// 优先级: P0/P1/P2
// 测试场景: 详细场景描述
// 预期结果:
//   - 预期结果1
//   - 预期结果2
// ============================================================================
func TestSWXX_Scenario_Description(t *testing.T) {
	t.Log("SW-XX: Testing scenario description")

	// Arrange: 准备测试数据
	rm, subscriptionId := setupTest(t)
	defer teardownTest(rm)

	// Act: 执行操作
	// ...

	// Assert: 验证结果
	// ...

	t.Log("SW-XX: Test completed - summary")
}
```

## 维护者
- 测试团队
- 后端开发团队

---

**最后更新**: 2025-12-12
**测试用例版本**: v1.0
**测试框架版本**: scene_test v2.0
