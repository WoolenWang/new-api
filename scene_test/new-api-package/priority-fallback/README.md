# 套餐优先级与Fallback测试套件

## 概览

本测试套件验证NewAPI包月套餐功能中的**优先级机制**和**Fallback降级**逻辑的正确性。

## 测试覆盖范围

### 核心测试场景（9个测试用例）

| 测试ID | 测试场景 | 优先级 | 关键验证点 |
|--------|----------|--------|-----------|
| **PF-01** | 单套餐未超限 | P0 | 正常使用套餐，余额不变 |
| **PF-02** | 单套餐超限-允许Fallback | P0 | 套餐超限后Fallback到用户余额 |
| **PF-03** | 单套餐超限-禁止Fallback | P0 | 返回429错误，不扣费 |
| **PF-04** | 多套餐优先级降级 | P0 | 高优先级超限后自动降级到低优先级 |
| **PF-05** | 优先级相同按ID排序 | P1 | 相同优先级按subscription.id ASC排序 |
| **PF-06** | 所有套餐超限-Fallback | P0 | 所有套餐超限后使用用户余额 |
| **PF-07** | 所有套餐超限-无Fallback | P0 | 最后一个套餐禁止Fallback时返回429 |
| **PF-08** | 月度总限额优先检查 | P1 | 月度总限额检查优先于滑动窗口 |
| **PF-09** | 多窗口任一超限即失败 | P0 | 小时/日窗口任一超限，套餐不可用 |

## 测试设计原理

### 优先级机制
```
套餐按优先级降序排列：Priority 21 → 1
├── 高优先级套餐优先消耗
├── 超限后自动尝试低优先级套餐
└── 相同优先级按 subscription.id ASC 排序
```

### Fallback机制
```
所有套餐遍历完毕后：
├── 检查最后一个套餐的 fallback_to_balance 配置
├── true → 使用用户余额
└── false → 返回 429 Too Many Requests
```

### 滑动窗口检查顺序
```
1. 月度总限额检查（DB: total_consumed）
2. RPM窗口检查（Redis Lua）
3. 小时窗口检查（Redis Lua）
4. 4小时窗口检查（Redis Lua）
5. 日窗口检查（Redis Lua）
6. 周窗口检查（Redis Lua）

任一步骤超限 → 整个套餐不可用
```

## 运行测试

### 运行所有优先级测试
```bash
cd scene_test/new-api-package/priority-fallback
go test -v
```

### 运行特定测试
```bash
# 只运行PF-01
go test -v -run TestPF01_SinglePackage_NotExceeded

# 只运行P0优先级测试
go test -v -run ".*PF0[1-4,6-7,9].*"
```

### 生成覆盖率报告
```bash
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

## 测试数据配置

### 用户配置
- **Group**: default (ratio=1.0), vip (ratio=2.0)
- **Quota**: 100M（足够测试使用）

### 套餐配置
| 场景 | 优先级 | 月度限额 | 小时限额 | 日限额 | Fallback |
|------|--------|----------|----------|--------|----------|
| 单套餐测试 | 15 | 500M | 5M-10M | - | true/false |
| 多套餐测试 | 15, 5 | 500M | 5M, 20M | - | true/false |
| 多窗口测试 | 15 | 500M | 10M | 20M | true |

### Mock LLM配置
- 可控的 `prompt_tokens` 和 `completion_tokens`
- 用于精确控制quota消耗量
- 默认 ModelRatio = 1.0

## 关键验证点

### 1. 套餐消耗验证
```go
testutil.AssertSubscriptionConsumed(t, sub.Id, expectedConsumed)
```

### 2. 用户余额验证
```go
testutil.AssertUserQuotaUnchanged(t, user.Id, initialQuota)
testutil.AssertUserQuotaChanged(t, user.Id, initialQuota, -expectedDeduction)
```

### 3. 滑动窗口状态验证
```go
windowKey := testutil.FormatWindowKey(sub.Id, "hourly")
consumed, _ := miniRedis.HGet(windowKey, "consumed")
```

### 4. HTTP响应码验证
- `200 OK`: 成功（使用套餐或Fallback到余额）
- `429 Too Many Requests`: 套餐超限且禁止Fallback

## 依赖关系

### 测试工具依赖
- `testutil.TestServer`: HTTP测试服务器
- `testutil.MockLLMServer`: Mock上游LLM
- `miniredis`: Redis模拟（滑动窗口）
- `SQLite in-memory`: 内存数据库

### 数据模型依赖
- `model.Package`: 套餐模板
- `model.Subscription`: 套餐订阅
- `model.User`: 用户
- `model.Token`: 认证Token
- `model.Channel`: 转发渠道

## 故障排查

### 测试失败场景

#### 1. 套餐未正确扣减
**原因**: PreConsumeQuota逻辑未正确调用TryConsumeFromPackage
**检查**: 查看 `service/pre_consume_quota.go` 中的集成点

#### 2. 优先级排序错误
**原因**: SQL排序逻辑错误
**检查**: `GetUserAvailablePackages` 的 ORDER BY 子句

#### 3. Fallback未生效
**原因**: fallback_to_balance配置未正确读取
**检查**: `SelectAvailablePackage` 中的Fallback决策逻辑

#### 4. 滑动窗口超限未检测
**原因**: Lua脚本未正确执行
**检查**: Redis连接状态，Lua脚本加载状态

### 调试技巧

```go
// 打印详细日志
t.Logf("Package: %+v", pkg)
t.Logf("Subscription: %+v", sub)

// 手动查询Redis窗口
windowKey := testutil.FormatWindowKey(sub.Id, "hourly")
values, _ := miniRedis.HGetAll(windowKey)
t.Logf("Window state: %+v", values)

// 查询所有用户套餐
packages, _ := model.GetUserAvailablePackages(user.Id, nil, time.Now().Unix())
t.Logf("Available packages: %d", len(packages))
```

## 参考文档

- **设计文档**: `docs/NewAPI-支持多种包月套餐-优化版.md`
- **测试方案**: `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` (第2.3节)
- **总体测试框架**: `scene_test/README.md`

## 维护者

- QA Team
- 后端开发团队

---

**最后更新**: 2025-12-12
**版本**: v1.0
