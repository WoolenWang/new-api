# 异常与容错测试套件 (Exception & Fault Tolerance Tests)

## 概览

本测试套件专门针对 NewAPI 包月套餐系统的**异常处理**和**容错能力**进行验证，确保系统在各种异常情况下能够：
- 优雅降级，不影响主业务流程
- 记录详细的错误日志，便于运维排查
- 具备自动恢复能力

## 测试场景覆盖

| 测试ID | 测试场景 | 优先级 | 关键验证点 |
| :--- | :--- | :--- | :--- |
| **EX-01** | Redis中途断开 | P0 | 降级到DB-only模式，记录降级日志，Redis恢复后功能自动恢复 |
| **EX-02** | DB中途断开 | P1 | 异步更新失败不影响API响应，记录ERROR日志，支持数据补齐 |
| **EX-03** | Lua脚本返回异常格式 | P1 | Type assertion容错，不panic，降级允许请求通过 |
| **EX-04** | 套餐查询超时 | P1 | 5秒超时控制，降级到用户余额，不阻塞请求 |
| **EX-05** | Pipeline批量查询失败 | P1 | 部分失败不影响整体，降级处理，错误隔离 |
| **EX-06** | 套餐过期但未标记 | P1 | 动态过期检查，保护性验证，定时任务最终标记 |

## 测试设计原则

### 1. 降级策略验证
所有异常场景都应该有明确的降级策略：
- **Redis不可用** → 降级到仅检查月度总限额（DB）
- **DB不可用** → 异步更新失败，不影响响应
- **Lua脚本异常** → 降级到允许请求通过
- **查询超时** → 降级到用户余额
- **Pipeline失败** → 部分成功即可

### 2. 鲁棒性验证
验证系统在异常情况下不会崩溃：
- 不会出现panic
- Type assertion失败时有容错处理
- 网络/IO错误有重试或降级机制

### 3. 恢复能力验证
验证异常恢复后功能自动恢复：
- Redis断开→恢复后滑动窗口功能正常
- DB断开→恢复后数据可补齐
- Pipeline失败→恢复后批量查询正常

### 4. 日志完整性验证
验证异常情况下的日志记录：
- 降级日志（WARN级别）
- 错误日志（ERROR级别）
- 包含关键上下文信息（subscription_id, period, error_message）

## 关键技术实现

### 异常注入方式

#### 1. Redis异常模拟
```go
// 关闭Redis
s.miniRedis.Close()

// 恢复Redis
s.miniRedis, _ = miniredis.Run()
```

#### 2. DB异常模拟
```go
// 关闭DB连接
sqlDB, _ := s.db.DB()
sqlDB.Close()

// 重新打开DB
s.db, _ = gorm.Open(sqlite.Open("file::memory:?cache=shared"), ...)
```

#### 3. Lua异常模拟
```go
// 返回nil
mockLuaScriptExecution(subscriptionID, nil)

// 返回错误格式
mockLuaScriptExecution(subscriptionID, "invalid")

// 返回元素不足
mockLuaScriptExecution(subscriptionID, []interface{}{1, 2})
```

#### 4. 超时模拟
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// 模拟慢速操作
time.Sleep(6 * time.Second)
```

### Type Assertion容错处理示例

设计文档中要求的容错逻辑（`service/package_sliding_window.go`）：

```go
// 解析Lua返回值（带容错）
func parseLuaResult(result interface{}) (*WindowResult, error) {
    defer func() {
        if r := recover(); r != nil {
            // 捕获panic，记录日志
            common.SysError(fmt.Sprintf("Lua result parsing panic: %v", r))
        }
    }()

    // 1. 检查nil
    if result == nil {
        return &WindowResult{Success: true}, nil // 降级
    }

    // 2. 检查类型
    resultArray, ok := result.([]interface{})
    if !ok {
        common.SysLog("Lua returned non-array, degrading")
        return &WindowResult{Success: true}, nil
    }

    // 3. 检查长度
    if len(resultArray) < 4 {
        common.SysLog("Lua returned insufficient elements, degrading")
        return &WindowResult{Success: true}, nil
    }

    // 4. 解析每个元素（带type assertion）
    status, ok1 := resultArray[0].(int64)
    consumed, ok2 := resultArray[1].(int64)
    startTime, ok3 := resultArray[2].(int64)
    endTime, ok4 := resultArray[3].(int64)

    if !ok1 || !ok2 || !ok3 || !ok4 {
        common.SysLog("Lua returned wrong element types, degrading")
        return &WindowResult{Success: true}, nil
    }

    // 5. 正常解析成功
    return &WindowResult{
        Success:   status == 1,
        Consumed:  consumed,
        StartTime: startTime,
        EndTime:   endTime,
    }, nil
}
```

## 运行测试

### 运行所有异常容错测试
```bash
cd scene_test/new-api-package/exception-tolerance
go test -v
```

### 运行特定测试用例
```bash
# 仅运行P0级别测试
go test -v -run TestEX01

# 运行Redis异常测试
go test -v -run TestEX01_RedisDisconnectDuringRequest

# 运行Lua异常测试
go test -v -run TestEX03_LuaScriptReturnsInvalidFormat
```

### 调试模式运行
```bash
# 显示详细日志
go test -v -args -test.v

# 禁用并行执行（便于调试）
go test -v -p 1
```

## 测试数据说明

### 默认测试数据
- **套餐**: priority=15, hourlyLimit=20M, quota=500M
- **订阅**: status=active, total_consumed=0
- **用户**: quota=100M, group=default

### Redis窗口Key格式
```
subscription:{subscription_id}:{period}:window

示例:
subscription:1:hourly:window
subscription:1:daily:window
subscription:1:rpm:window
```

### 窗口Hash字段
- `start_time` (INT64): 窗口开始时间戳（秒）
- `end_time` (INT64): 窗口结束时间戳（秒）
- `consumed` (INT64): 已消耗额度或请求数
- `limit` (INT64): 限额

## 预期日志输出

### EX-01: Redis断开
```
[WARN] Redis unavailable, sliding window check skipped for subscription 123
[INFO] Degrading to DB-only mode, updating total_consumed directly
```

### EX-02: DB断开
```
[ERROR] Failed to update subscription 123: database connection lost
[WARN] Subscription update will be retried when DB recovers
```

### EX-03: Lua异常
```
[WARN] Lua script returned invalid format: expected 4 elements, got 2
[INFO] Degrading: allowing request to pass through
```

### EX-04: 查询超时
```
[WARN] Package query timeout for user 456 after 5 seconds
[INFO] Falling back to user balance
```

### EX-05: Pipeline失败
```
[ERROR] Pipeline command failed for subscription:1:daily:window: key does not exist
[WARN] Degrading: using available window results only
```

### EX-06: 过期套餐
```
[WARN] Subscription 789 has expired (end_time: 2025-12-01 10:00:00) but status is still active
[INFO] Applying dynamic expiration check, filtering out expired subscription
```

## 故障排查

### 测试失败常见原因

1. **Redis连接问题**
   - 检查miniredis是否成功启动
   - 检查Redis地址是否正确配置

2. **DB连接问题**
   - 确保内存DB初始化成功
   - 检查表结构是否已迁移

3. **时间相关问题**
   - miniredis的FastForward功能可能不完全支持所有场景
   - 使用真实的time.Sleep进行时间推进

4. **异步操作问题**
   - 异步更新可能需要等待时间
   - 使用适当的同步机制（channel, waitgroup）

## 依赖关系

### 外部依赖
- `github.com/alicebob/miniredis/v2` - Redis模拟
- `github.com/stretchr/testify` - 断言库
- `gorm.io/gorm` - ORM
- `gorm.io/driver/sqlite` - SQLite驱动

### 内部依赖
- `testutil/package_helper.go` - 套餐测试辅助函数
- `testutil/sliding_window_helper.go` - 滑动窗口辅助函数
- `testutil/redis_mock.go` - Redis Mock辅助函数
- `model/package.go` - 套餐数据模型
- `model/subscription.go` - 订阅数据模型
- `service/package_sliding_window.go` - 滑动窗口服务（待实现）

## 参考文档

- `docs/NewAPI-支持多种包月套餐-优化版.md` - 套餐系统设计文档
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 测试设计文档（第2.12节）

## 维护指南

### 添加新的异常场景测试

1. 在`exception_test.go`中添加新的测试函数
2. 遵循命名规范: `TestEX{ID}_{Scenario}`
3. 添加完整注释（Test ID, Priority, Scenario, Expected Result）
4. 使用AAA模式（Arrange-Act-Assert）
5. 添加详细的日志输出

### 更新测试数据

如果套餐/订阅的数据结构发生变化：
1. 更新`TestPackage`和`TestSubscription`结构体
2. 更新`createTestPackage`和`createTestSubscription`辅助函数
3. 检查所有断言是否仍然有效

### 性能考虑

- 异常测试可能涉及延迟操作（sleep, timeout），执行时间较长
- 建议在CI中单独执行异常测试套件
- 可使用`-short`标签跳过长时间运行的测试

---

**最后更新**: 2025-12-12
**版本**: v1.0
**维护者**: QA Team
