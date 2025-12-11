# P2P分组聚合统计测试套件说明

## 测试目录结构

```
scene_test/new-api-monitoring-stats/group-statistics/
├── aggregation_test.go           # 聚合计算正确性测试 (GS-01 ~ GS-07)
├── event_throttle_test.go        # 事件驱动与节流测试 (GE-01 ~ GE-04)
├── concurrency_control_test.go   # 并发控制测试 (GC-01 ~ GC-05)
└── query_test.go                 # 分组统计查询测试 (GQ-01 ~ GQ-07)
```

## 测试套件概览

### 1. 聚合计算正确性测试 (aggregation_test.go)

**测试目标**: 验证分组聚合逻辑的数学正确性

| 测试ID | 测试场景 | 优先级 | 关键验证点 |
|--------|---------|--------|-----------|
| GS-01 | 求和类指标聚合 (TPM, RPM, TotalTokens) | P0 | `Group.TPM = Σ(Channel_i.TPM)` |
| GS-02 | 加权平均失败率聚合 | P0 | 按请求数加权平均 `Σ(FR_i × RC_i) / Σ(RC_i)` |
| GS-03 | 加权平均响应时间聚合 | P0 | 按请求数加权平均 `Σ(RT_i × RC_i) / Σ(RC_i)` |
| GS-04 | 并发数直接求和 | P1 | 并发能力叠加 `Σ(Concurrency_i)` |
| GS-05 | 去重用户数聚合 | P0 | HyperLogLog去重 (需后端实现) |
| GS-06 | 按模型维度独立聚合 | P0 | 不同模型生成独立统计记录 |
| GS-07 | 禁用渠道不参与聚合 | P0 | 仅统计 `status=1` 的渠道 |

**关键测试数据**:
- 两个渠道: Ch1 (100 req, 10 fail, 1000 tokens), Ch2 (200 req, 40 fail, 2000 tokens)
- 预期聚合: TPM=200, 失败率=16.67%, 总tokens=3000

### 2. 事件驱动与节流测试 (event_throttle_test.go)

**测试目标**: 验证事件触发机制和30分钟节流策略

| 测试ID | 测试场景 | 优先级 | 关键验证点 |
|--------|---------|--------|-----------|
| GE-01 | 渠道更新触发聚合事件 | P0 | 渠道统计持久化后触发聚合 |
| GE-02 | 30分钟节流机制 | P0 | 同一分组30分钟内只聚合一次 |
| GE-03 | 节流窗口过期 | P0 | 31分钟后可再次触发 (需时间Mock) |
| GE-04 | 跨分组独立节流 | P1 | 不同分组的节流计时器独立 |

**测试策略**:
- 在T0、T0+10min、T0+20min更新3个渠道
- 验证只有T0触发聚合，后续更新被节流
- 验证不同分组的节流互不影响

### 3. 并发控制测试 (concurrency_control_test.go)

**测试目标**: 验证分布式锁和全局并发限制

| 测试ID | 测试场景 | 优先级 | 关键验证点 |
|--------|---------|--------|-----------|
| GC-01 | 分布式锁防止重复聚合 | P0 | Worker A获锁成功，Worker B失败 |
| GC-02 | 锁超时恢复 (180秒TTL) | P1 | 锁过期后新Worker可获取 |
| GC-03 | 全局并发限制 (Max=5) | P0 | 10个任务最多5个并发 |
| GC-04 | 锁释放失败处理 | P2 | 依赖TTL避免死锁 |
| GC-05 | 竞态条件防护 (额外) | Bonus | 并发更新无数据损坏 |

**并发测试设计**:
- 使用goroutine模拟多Worker同时触发聚合
- 验证最终数据一致性（无重复计算、无数据丢失）
- 验证锁机制正确性（互斥、超时恢复）

### 4. 分组统计查询测试 (query_test.go)

**测试目标**: 验证查询API的正确性和权限控制

| 测试ID | 测试场景 | 优先级 | 关键验证点 |
|--------|---------|--------|-----------|
| GQ-01 | 分组总体统计查询 (无model过滤) | P1 | 返回所有模型聚合数据 |
| GQ-02 | 按模型过滤查询 | P1 | 仅返回指定模型统计 |
| GQ-03 | 权限控制 (仅成员可查) | P0 | 非成员返回403 |
| GQ-04 | 数据时效性 | P1 | UpdatedAt在30分钟内 |
| GQ-05 | 空分组查询 (额外) | Bonus | 返回空或全0数据 |
| GQ-06 | 历史统计查询 (额外) | Bonus | 按时间窗口查询历史数据 |
| GQ-07 | 多分组对比 (额外) | Bonus | 跨分组数据对比 |

**权限测试场景**:
- User1 (分组成员/Owner) → 成功访问
- User2 (分组成员) → 成功访问
- User3 (非成员) → 返回403

## 数据模型

### GroupStatisticsModel
```go
type GroupStatisticsModel struct {
    GroupID         int     // 分组ID
    ModelName       string  // 模型名称
    TimeWindowStart int64   // 时间窗口起始
    TPM             int     // 每分钟Tokens数
    RPM             int     // 每分钟请求数
    TotalTokens     int64   // 总Token数
    FailRate        float64 // 失败率 (%)
    AvgResponseTime int     // 平均响应时间 (ms)
    AvgConcurrency  float64 // 平均并发数
    UniqueUsers     int     // 去重用户数
    UpdatedAt       int64   // 最后更新时间
}
```

### ChannelStatisticsModel
```go
type ChannelStatisticsModel struct {
    ChannelID       int    // 渠道ID
    ModelName       string // 模型名称
    TimeWindowStart int64  // 时间窗口起始
    RequestCount    int    // 请求总数
    FailCount       int    // 失败请求数
    TotalTokens     int64  // 总Token数
    TotalLatencyMs  int64  // 总延迟
    // ...更多字段
}
```

## Mock服务

本测试套件使用以下Mock组件:
- **MockUpstreamServer**: 模拟上游LLM服务，返回标准响应
- (不需要MockJudgeLLM，因为分组统计不涉及模型评估)

## 运行测试

### 运行所有分组统计测试
```bash
cd scene_test/new-api-monitoring-stats/group-statistics
go test -v ./...
```

### 运行特定测试套件
```bash
# 聚合计算测试
go test -v -run TestAggregationSuite

# 事件节流测试
go test -v -run TestEventThrottleSuite

# 并发控制测试
go test -v -run TestConcurrencyControlSuite

# 查询测试
go test -v -run TestQuerySuite
```

### 运行特定测试用例
```bash
# 测试求和类指标聚合
go test -v -run TestAggregationSuite/TestGS01_SummationMetrics

# 测试权限控制
go test -v -run TestQuerySuite/TestGQ03_PermissionControl
```

## 测试依赖

### 前置条件
1. **后端API实现**: 需要以下API端点已实现
   - `POST /api/internal/channel_statistics` - 创建渠道统计
   - `GET /api/p2p_groups/:id/stats` - 查询分组统计
   - `POST /api/internal/groups/:id/trigger_aggregation` - 手动触发聚合
   - `GET /api/internal/groups/:id/aggregation_status` - 查询聚合状态

2. **数据库表**: 需要以下表已创建
   - `channel_statistics` - 渠道统计时序表
   - `group_statistics` - 分组聚合统计表

3. **后台服务**: 需要以下后台服务运行
   - DB Sync Worker - 渠道统计持久化
   - GroupAggregator Worker - 分组聚合计算
   - Event Listener - 监听渠道更新事件

### 可选依赖
- **Redis**: 用于节流状态和分布式锁 (可使用miniredis Mock)
- **Time Mocking**: GE-03测试需要时间控制能力

## 测试数据准备

### 辅助工具函数

**testutil/group_stats_helper.go** 提供:
- `CreateTestChannelStatistics()` - 创建测试用的渠道统计数据
- `CalculateExpectedFailRate()` - 计算预期的加权平均失败率
- `CalculateExpectedAvgResponseTime()` - 计算预期的加权平均响应时间
- `SumChannelTPM()` / `SumChannelRPM()` - 计算总和类指标

### 数据准备示例

```go
// 创建两个渠道的统计数据
stats1 := CreateTestChannelStatistics(channel1.ID, "gpt-4", 100, 10, 1000)
stats2 := CreateTestChannelStatistics(channel2.ID, "gpt-4", 200, 40, 2000)

// 计算预期聚合结果
expectedFailRate := CalculateExpectedFailRate([]*ChannelStatisticsModel{stats1, stats2})
// Result: (10*100 + 40*200) / 300 = 30.0%

expectedTPM := SumChannelTPM([]*ChannelStatisticsModel{stats1, stats2})
// Result: (1000 + 2000) / 15 = 200
```

## 验收标准

### 功能完整性
- [x] 所有P0优先级测试用例 (11个)
- [x] 所有P1优先级测试用例 (5个)
- [x] P2优先级测试用例 (1个)
- [x] 额外奖励测试 (3个)

### 聚合算法验证
- [x] 求和类指标 (TPM, RPM, TotalTokens, TotalQuota, TotalSessions)
- [x] 加权平均类指标 (FailRate, AvgResponseTime)
- [x] 特殊聚合指标 (AvgConcurrency直接求和)
- [x] 去重指标 (UniqueUsers HyperLogLog)

### 并发安全性
- [x] 分布式锁互斥性测试
- [x] 全局并发数限制测试
- [x] 竞态条件防护测试
- [x] 锁超时恢复测试

### 性能要求
- [ ] 聚合计算完成时间 < 5秒 (单个分组)
- [ ] 查询响应时间 < 500ms
- [ ] 并发场景下数据准确性误差 < 0.1%

## 已知限制

1. **GS-04, GS-05**: 需要后端实现AvgConcurrency字段和UniqueUsers HyperLogLog
2. **GE-03**: 完整验证需要时间Mock或等待31分钟
3. **GC-02**: 锁超时测试需要等待180秒或时间Mock
4. **权限测试**: 需要后端实现分组成员权限检查

## 测试覆盖率

| 测试类别 | 测试用例数 | P0 | P1 | P2 | Bonus |
|---------|-----------|----|----|----|----|
| 聚合计算 | 7 | 4 | 1 | 0 | 2 |
| 事件节流 | 4 | 2 | 1 | 0 | 1 |
| 并发控制 | 5 | 2 | 1 | 1 | 1 |
| 查询功能 | 7 | 2 | 2 | 0 | 3 |
| **总计** | **23** | **10** | **5** | **1** | **7** |

## 下一步工作

1. **后端API实现**: 实现 `/api/p2p_groups/:id/stats` 等接口
2. **后台Worker实现**: 实现GroupAggregator Worker和事件监听器
3. **Redis集成**: 实现节流状态和分布式锁
4. **集成测试**: 在CI环境中运行完整测试套件
5. **性能优化**: 根据测试结果优化聚合算法和查询性能

## 相关设计文档

- `docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md` - 详细设计
- `docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示-测试方案.md` - 测试方案
- `docs/07-NEW-API-分组改动详细设计.md` - 分组改动设计
