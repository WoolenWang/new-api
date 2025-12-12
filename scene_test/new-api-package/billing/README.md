# NewAPI 包月套餐 - 计费准确性测试套件

## 概览

本目录包含NewAPI包月套餐功能的**计费准确性专项测试**，覆盖正常计费流程和异常场景的容错处理。

## 测试覆盖范围

### 正常计费测试 (`billing_accuracy_test.go`)

| 测试ID | 测试场景 | 验证点 | 优先级 |
|--------|----------|--------|--------|
| **BA-01** | 套餐消耗基础计费 | 验证计费公式：`(InputTokens + OutputTokens×CompletionRatio) × ModelRatio × GroupRatio` | **P0** |
| **BA-02** | Fallback时应用GroupRatio | 套餐超限后使用用户余额时，GroupRatio正确应用 | **P0** |
| **BA-03** | 流式请求预扣与补差 | 流式请求的预估扣费和实际补差机制 | **P0** |
| **BA-04** | 缓存Token计费 | 验证缓存Token的特殊计费（cached×0.1 + normal×1.0） | P1 |
| **BA-05** | 多模型混合计费 | 同一套餐在不同模型下的累加正确性 | P1 |

### 异常计费测试 (`billing_exception_test.go`)

| 测试ID | 测试场景 | 验证点 | 优先级 |
|--------|----------|--------|--------|
| **BA-06** | 上游返回空usage | 系统容错，使用默认估算，不crash | P1 |
| **BA-06-Multi** | 多次空usage累积 | 估算值正确累加 | P1 |
| **BA-06-Malformed** | 畸形usage字段 | 格式错误时的容错处理 | P1 |
| **BA-07** | 请求失败不扣费 | 500错误时套餐和余额都不扣减 | **P0** |
| **BA-07-401** | 401鉴权失败不扣费 | 鉴权失败时的回滚逻辑 | **P0** |
| **BA-07-RateLimit** | 429限流不扣费 | 上游限流时不扣费 | **P0** |
| **BA-07-Timeout** | 超时不扣费 | 请求超时时的处理 | P1 |
| **BA-08** | 流式中断部分扣费 | 流式请求中途断开的计费 | P1 |
| **BA-09** | 套餐刚好用尽 | 月度限额边界处理（99.9M + 0.2M） | P2 |
| **BA-09-Strict** | 严格月度限额 | 不允许Fallback时的严格限制 | P2 |

## 测试原理

### 计费公式验证

**基础公式**:
```
Quota = (InputTokens + OutputTokens × CompletionRatio) × ModelRatio × GroupRatio
```

**关键验证点**:
1. **套餐消耗**: DB的`subscription.total_consumed`精确累加
2. **滑动窗口**: Redis窗口的`consumed`原子扣减
3. **用户余额**: Fallback时正确应用GroupRatio
4. **异常容错**: 失败请求不扣费，系统不crash

### 测试数据配置

**测试用户**:
- Username: `billing-test-user-*` / `exception-test-user-*`
- Group: `vip` (GroupRatio=2.0)
- Quota: 100M (初始余额)

**测试套餐**:
- Priority: 15 (高优先级)
- Quota: 500M (月度限额)
- HourlyLimit: 20M (小时限额)
- FallbackToBalance: true/false (根据场景配置)

**Mock LLM响应**:
- StatusCode: 200 (成功) / 4xx/5xx (失败)
- PromptTokens: 根据场景配置
- CompletionTokens: 根据场景配置

## 运行测试

### 运行所有计费测试
```bash
cd scene_test/new-api-package/billing
go test -v ./...
```

### 运行特定测试套件
```bash
# 仅运行正常计费测试
go test -v -run TestBillingAccuracyTestSuite

# 仅运行异常计费测试
go test -v -run TestBillingExceptionTestSuite
```

### 运行特定测试用例
```bash
# 运行BA-01基础计费测试
go test -v -run TestBillingAccuracyTestSuite/TestBA01

# 运行BA-07失败不扣费测试
go test -v -run TestBillingExceptionTestSuite/TestBA07
```

### 运行P0优先级测试
```bash
# 运行所有P0测试（BA-01, BA-02, BA-03, BA-07系列）
go test -v -run "TestBA0[123]|TestBA07"
```

## 测试环境

### 必要组件
- **内存数据库**: SQLite内存模式（隔离环境）
- **Mock Redis**: miniredis（模拟滑动窗口）
- **Mock LLM**: httptest.Server（模拟上游响应）

### 环境配置
测试框架自动配置以下环境：
- `PACKAGE_ENABLED=true` - 启用套餐功能
- `SQL_DSN=file::memory:` - 使用内存数据库
- `REDIS_CONN_STRING` - 指向miniredis

## 关键验证逻辑

### 1. 套餐扣减验证
```go
// 验证DB的total_consumed
testutil.AssertSubscriptionConsumed(t, subscriptionId, expectedQuota)

// 验证用户余额不变（使用套餐时）
testutil.AssertUserQuotaUnchanged(t, userId, initialQuota)
```

### 2. Fallback验证
```go
// 验证套餐未扣减（超限）
updatedSub, _ := model.GetSubscriptionById(subscriptionId)
assert.Equal(t, 0, updatedSub.TotalConsumed)

// 验证用户余额扣减
finalQuota, _ := model.GetUserQuota(userId, true)
assert.Equal(t, expectedFinalQuota, finalQuota)
```

### 3. 异常容错验证
```go
// 验证请求失败时不扣费
assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
testutil.AssertSubscriptionConsumed(t, subscriptionId, 0)
testutil.AssertUserQuotaUnchanged(t, userId, initialQuota)
```

## 测试数据清理

每个测试用例执行前会自动清理：
- `subscriptions` 表
- `packages` 表
- `user_groups` 表
- `groups` 表
- `users` 表（保留系统用户）

这确保测试用例之间完全隔离，互不影响。

## 已知限制

### BA-04 缓存Token计费
- 当前测试为简化实现，标记为`Skip`
- 完整验证需要后端支持`cache_read_tokens`字段
- 待后端实现后完善测试逻辑

### BA-08 流式中断
- 当前测试为简化实现，标记为`Skip`
- 完整验证需要了解系统对流式中断的具体处理逻辑
- 建议配合实际流式中断场景进行手动验证

## 扩展测试用例

除测试方案文档要求的9个基础用例外，本套件还包含以下扩展用例：

- **BA-06-Multi**: 多次空usage请求的累积行为
- **BA-06-Malformed**: 畸形usage字段的容错
- **BA-07-401**: 401鉴权失败不扣费
- **BA-07-RateLimit**: 429限流不扣费
- **BA-07-Timeout**: 超时不扣费
- **BA-09-Strict**: 严格月度限额（不允许Fallback）

这些扩展用例增强了测试的健壮性，覆盖更多边界场景。

## 故障排查

### 测试失败常见原因

1. **套餐扣减不匹配**
   - 检查ModelRatio配置是否正确
   - 检查GroupRatio是否按用户分组正确计算
   - 检查CompletionRatio默认值（通常为1.0或1.2）

2. **用户余额意外变化**
   - 检查Fallback配置（`fallback_to_balance`）
   - 检查套餐小时限额是否足够大
   - 检查月度总限额是否触发

3. **异步更新延迟**
   - 增加`time.Sleep`等待时间
   - 检查后台异步任务是否正常运行

4. **Mock服务器响应异常**
   - 验证Mock配置是否生效
   - 检查渠道BaseURL是否正确指向Mock

### 调试命令

```bash
# 查看详细日志
go test -v -run TestBA01 2>&1 | tee test.log

# 检查数据库状态（测试失败时保留现场）
sqlite3 scene_test/test.db "SELECT * FROM subscriptions;"

# 检查Redis状态（需要miniredis支持）
# (测试中可通过TestServer.MiniRedis访问)
```

## 参考资料

### 设计文档
- `docs/NewAPI-支持多种包月套餐-优化版.md` - 套餐功能总体设计
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 测试方案详细设计

### 相关代码
- `service/package_consume.go` - 套餐消耗逻辑（待实现）
- `service/package_sliding_window.go` - 滑动窗口Lua脚本（待实现）
- `service/pre_consume_quota.go` - 预扣费集成点（待修改）
- `service/quota.go` - 后扣费集成点（待修改）

### 测试工具
- `scene_test/testutil/package_helper.go` - 套餐测试辅助函数
- `scene_test/testutil/client.go` - API客户端封装
- `scene_test/testutil/mock_llm_server.go` - Mock LLM服务器

## 维护者

- QA Team
- 后端开发团队

---

**创建日期**: 2025-12-12
**版本**: v1.0
**状态**: 已完成编码，待后端实现后执行
