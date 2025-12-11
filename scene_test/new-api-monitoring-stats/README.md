# NewAPI 监控统计系统测试套件

## 概览

本目录包含NewAPI监控统计系统的完整集成测试套件，涵盖：
- **2.2 模型智能监控测试** (Model Monitoring)
- **2.3 P2P分组聚合统计测试** (Group Statistics)

## 目录结构

```
scene_test/new-api-monitoring-stats/
├── model-monitoring/              # 模型智能监控测试
│   ├── baseline_management_test.go       # 基准管理测试 (MB-01 ~ MB-04)
│   ├── policy_schedule_test.go           # 策略调度测试 (MP-01 ~ MP-05)
│   ├── probe_evaluation_test.go          # 探测评估测试 (ME-01 ~ ME-08)
│   └── result_query_test.go              # 结果查询测试 (MR-01 ~ MR-05)
│
├── group-statistics/              # P2P分组聚合统计测试
│   ├── aggregation_test.go               # 聚合计算测试 (GS-01 ~ GS-07)
│   ├── event_throttle_test.go            # 事件节流测试 (GE-01 ~ GE-04)
│   ├── concurrency_control_test.go       # 并发控制测试 (GC-01 ~ GC-05)
│   ├── query_test.go                     # 查询功能测试 (GQ-01 ~ GQ-07)
│   └── README.md                         # 分组统计测试说明
│
└── README.md                      # 本文件
```

## 测试套件汇总

### 2.2 模型智能监控测试 (22个用例)

#### 基准管理 (4个用例)
- **MB-01**: 创建基准 - 验证基准数据正确持久化 [P0]
- **MB-02**: 基准唯一性约束 - 相同组合更新而非新增 [P0]
- **MB-03**: 基准查询 - 批量查询所有基准 [P1]
- **MB-04**: 基准更新 - 更换基准渠道和输出内容 [P1]

#### 策略调度 (5个用例)
- **MP-01**: 创建监控策略 - 配置监控范围和频率 [P0]
- **MP-02**: 策略调度触发 - 手动触发MonitorWorker [P0]
- **MP-03**: 策略禁用 - 禁用后任务不执行 [P1]
- **MP-04**: 多策略叠加 - 多个策略同时工作 [P0]
- **MP-05**: 渠道级配置覆盖 - 渠道配置优先于全局策略 [P0]

#### 探测评估 (8个用例)
- **ME-01**: 规则评估-编码通过 - 代码可执行判定pass [P0]
- **ME-02**: 规则评估-编码失败 - 语法错误判定fail [P0]
- **ME-03**: LLM裁判-风格通过 - 高相似度(95%)判pass [P0]
- **ME-04**: LLM裁判-风格失败 - 低相似度(30%)判fail [P0]
- **ME-05**: 评估标准差异 - strict/standard不同阈值 [P0]
- **ME-06**: 探测重试机制 - 失败后自动重试3次 [P1]
- **ME-07**: 探测失败标记 - 标记monitor_failed状态 [P0]
- **ME-08**: 裁判LLM失败处理 - 裁判服务故障隔离 [P1]

#### 结果查询 (5个用例)
- **MR-01**: 结果持久化 - 完整字段存储验证 [P0]
- **MR-02**: 历史结果查询 - 按时间倒序查询 [P1]
- **MR-03**: 模型横向对比报告 - 跨渠道对比 [P1]
- **MR-04**: 时间范围过滤 - 精确时间过滤 [P2]
- **MR-05**: 多测试类型查询 - 区分不同检测类型 [Bonus]

### 2.3 P2P分组聚合统计测试 (23个用例)

#### 聚合计算 (7个用例)
- **GS-01**: 求和类指标 - TPM/RPM/TotalTokens直接累加 [P0]
- **GS-02**: 加权平均失败率 - 按请求数加权 [P0]
- **GS-03**: 加权平均响应时间 - 按请求数加权 [P0]
- **GS-04**: 并发数求和 - 并发能力叠加 [P1]
- **GS-05**: 去重用户数 - HyperLogLog去重 [P0]
- **GS-06**: 按模型维度聚合 - 独立统计记录 [P0]
- **GS-07**: 禁用渠道排除 - 仅统计启用渠道 [P0]

#### 事件节流 (4个用例)
- **GE-01**: 渠道更新触发事件 - 验证事件驱动 [P0]
- **GE-02**: 30分钟节流机制 - 防止频繁聚合 [P0]
- **GE-03**: 节流窗口过期 - 31分钟后可再聚合 [P0]
- **GE-04**: 跨分组独立节流 - 不同分组互不影响 [P1]

#### 并发控制 (5个用例)
- **GC-01**: 分布式锁获取 - 防止重复聚合 [P0]
- **GC-02**: 锁超时恢复 - 180秒TTL机制 [P1]
- **GC-03**: 全局并发限制 - 最多5个Worker并发 [P0]
- **GC-04**: 锁释放失败处理 - TTL避免死锁 [P2]
- **GC-05**: 竞态条件防护 - 数据一致性保障 [Bonus]

#### 查询功能 (7个用例)
- **GQ-01**: 分组总体统计 - 所有模型聚合 [P1]
- **GQ-02**: 按模型过滤 - 单模型统计 [P1]
- **GQ-03**: 权限控制 - 仅成员可查询 [P0]
- **GQ-04**: 数据时效性 - 30分钟内更新 [P1]
- **GQ-05**: 空分组查询 - 边界情况处理 [Bonus]
- **GQ-06**: 历史统计查询 - 时间窗口查询 [Bonus]
- **GQ-07**: 多分组对比 - 跨分组比较 [Bonus]

## 总体统计

| 测试模块 | 测试套件数 | 测试用例数 | P0 | P1 | P2 | Bonus |
|---------|-----------|-----------|----|----|----|----|
| 模型监控 | 4 | 22 | 13 | 7 | 1 | 1 |
| 分组统计 | 4 | 23 | 10 | 5 | 1 | 7 |
| **合计** | **8** | **45** | **23** | **12** | **2** | **8** |

## Mock服务

### testutil/mock_judge_llm.go
**MockJudgeLLM** - 裁判LLM服务Mock

**功能**:
- 返回可配置的相似度评分 (0-100)
- 支持高/中/低相似度预设
- 模拟失败场景
- 记录所有请求用于验证

**使用示例**:
```go
judgeLLM := testutil.NewMockJudgeLLM()
defer judgeLLM.Close()

// 配置高相似度 (pass)
judgeLLM.SetHighSimilarity() // 95%

// 配置低相似度 (fail)
judgeLLM.SetLowSimilarity()  // 30%

// 模拟失败
judgeLLM.SetFailure("service_unavailable", "Judge LLM unavailable")
```

### testutil/mock_upstream.go
**MockUpstreamServer** - 上游LLM服务Mock

**功能**:
- 返回标准chat completion响应
- 可配置响应延迟
- 可配置错误响应(5xx, timeout)
- 记录所有请求

## 辅助工具

### testutil/monitoring_helper.go
监控相关API客户端方法:
- Baseline CRUD APIs
- Monitor Policy CRUD APIs
- Monitoring Results Query APIs

### testutil/group_stats_helper.go
分组统计辅助方法:
- Channel Statistics CRUD
- Group Statistics Query
- Aggregation Trigger & Status
- 聚合计算辅助函数

## 运行所有测试

```bash
# 进入测试目录
cd scene_test/new-api-monitoring-stats

# 运行所有监控统计测试
go test -v ./...

# 仅运行模型监控测试
go test -v ./model-monitoring/...

# 仅运行分组统计测试
go test -v ./group-statistics/...

# 运行P0优先级测试
go test -v -run ".*P0.*" ./...

# 生成测试覆盖率报告
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## CI/CD集成

### GitHub Actions配置示例

```yaml
name: Monitoring Stats Tests

on:
  push:
    branches: [ main, develop ]
    paths:
      - 'service/channel_stats*'
      - 'service/monitor*'
      - 'service/group_stats*'
      - 'scene_test/new-api-monitoring-stats/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - name: Run Tests
        run: |
          cd scene_test/new-api-monitoring-stats
          go test -v -timeout 30m ./...
```

## 故障排查

### 常见问题

1. **测试跳过 (Skip)**: 后端API未实现
   - 解决: 实现对应的API端点

2. **超时**: 聚合或监控任务未在预期时间完成
   - 检查后台Worker是否运行
   - 增加等待时间或检查日志

3. **权限错误**: 测试用户无法访问API
   - 验证用户创建和分组关系
   - 检查权限中间件配置

4. **数据不一致**: 聚合结果与预期不符
   - 检查聚合公式实现
   - 验证渠道状态(enabled/disabled)
   - 检查时间窗口对齐

### 调试技巧

```go
// 打印详细日志
t.Logf("Channel Stats: %+v", stats)
t.Logf("Group Stats: %+v", groupStats)

// 检查聚合状态
status, _ := client.GetGroupAggregationStatus(groupID)
t.Logf("Aggregation Status: %+v", status)

// 手动触发聚合
client.TriggerGroupAggregation(groupID)
time.Sleep(5 * time.Second) // 增加等待时间
```

## 设计原则

1. **隔离性**: 每个测试套件独立运行，互不影响
2. **可重复性**: 使用内存数据库和Mock服务，确保结果一致
3. **完整性**: 覆盖正常、异常、边界场景
4. **文档化**: 每个测试都有清晰的注释和测试ID
5. **可维护性**: 使用testify/suite组织，自动管理资源

## 贡献指南

### 添加新测试用例

1. 选择合适的测试套件文件
2. 遵循命名规范: `Test{TestID}_{Scenario}`
3. 添加完整注释 (测试ID、优先级、场景、预期结果)
4. 使用AAA模式 (Arrange-Act-Assert)
5. 适当使用Skip处理未实现功能

### 测试用例模板

```go
// Test{ID}_{Scenario} tests {description}.
//
// Test ID: {ID}
// Priority: {P0|P1|P2}
// Test Scenario: {场景描述}
// Expected Result: {预期结果}
func (s *{Suite}Suite) Test{ID}_{Scenario}() {
    s.T().Log("{ID}: Testing {description}")

    // Arrange: 准备测试数据
    // ...

    // Act: 执行操作
    // ...

    // Assert: 验证结果
    assert.NoError(s.T(), err, "Operation should succeed")
    assert.Equal(s.T(), expected, actual, "Result should match")

    s.T().Logf("{ID}: Test completed - {summary}")
}
```

## 参考资料

### 设计文档
- `docs/01-NEW-API-集成Wquant-测试设计.md` - 集成测试总体设计
- `docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md` - 监控统计详细设计
- `docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示-测试方案.md` - 本测试方案依据
- `docs/07-NEW-API-分组改动详细设计.md` - 分组系统设计

### 相关代码
- `service/channel_stats_*.go` - 渠道统计服务 (待实现)
- `service/monitor_*.go` - 模型监控服务 (待实现)
- `service/group_stats_*.go` - 分组聚合服务 (待实现)
- `model/monitor_*.go` - 监控数据模型 (待实现)

## 维护者

- QA Team
- 后端开发团队

---

**最后更新**: 2025-12-11
**版本**: v1.0
