# 计费与路由组合测试 (Billing & Routing Integration Tests)

## 测试目标

本测试套件验证**套餐系统**与**路由系统**的独立性和协同性，确保：
- 套餐只影响**额度管理**（从哪个资源池扣费）
- 路由系统**独立工作**（基于BillingGroup和RoutingGroups选择渠道）
- 两个系统在同一请求中协同但互不干扰

## 测试用例列表

### BR-01: 套餐与BillingGroup独立 (P0)
**测试场景**：
- 用户A的系统分组为vip
- 用户A订阅了全局套餐（优先级15）
- 系统中有Ch-vip和Ch-default两个渠道

**预期行为**：
1. 路由：基于用户的BillingGroup（vip），路由到Ch-vip渠道
2. 计费来源：从套餐扣减，而非用户余额
3. 计费倍率：使用vip的GroupRatio

**关键验证点**：
- ✓ 路由到正确的渠道（Ch-vip）
- ✓ 套餐total_consumed增加
- ✓ 用户余额保持不变
- ✓ 计费倍率符合vip分组

---

### BR-02: 套餐与P2P路由无关 (P0)
**测试场景**：
- 用户B订阅了P2P套餐（绑定分组G1）
- 用户B是P2P分组G1的成员
- 系统中有P2P渠道Ch-G1和公共渠道Ch-public

**预期行为**：
1. 路由：用户B可以访问G1授权的P2P渠道和公共渠道
2. 计费来源：从P2P套餐扣减
3. 路由范围：RoutingGroups包含G1和用户的系统分组

**关键验证点**：
- ✓ 可以路由到P2P渠道或公共渠道
- ✓ P2P套餐正确扣减
- ✓ RoutingGroups正确包含P2P分组
- ✓ 用户余额保持不变

---

### BR-03: Token覆盖BillingGroup (P0)
**测试场景**：
- 用户A的系统分组为vip
- 用户A订阅了全局套餐
- Token的billing_group配置为["default"]
- 系统中有渠道Ch-default

**预期行为**：
1. 路由：基于Token覆盖的BillingGroup（default），路由到Ch-default渠道
2. 计费来源：仍从套餐扣减
3. 计费倍率：使用default的GroupRatio（而非vip）

**关键验证点**：
- ✓ 路由到Ch-default渠道（而非Ch-vip）
- ✓ BillingGroup被Token覆盖为default
- ✓ 套餐正确扣减
- ✓ 计费倍率使用default（不是vip）
- ✓ 用户余额保持不变

---

### BR-04: 套餐用尽后路由不变 (P1)
**测试场景**：
- 用户A（vip分组）订阅了套餐A（小时限额5M）
- 用户A有充足的余额（100M）
- 系统中有渠道Ch-vip

**预期行为（两阶段）**：

**阶段1（套餐可用）**：
- 请求3M quota（小于5M限额）
- 路由到Ch-vip渠道
- 从套餐扣减

**阶段2（套餐超限Fallback）**：
- 再请求4M quota（总计7M > 5M限额）
- 路由仍然到Ch-vip渠道
- 从用户余额扣减

**关键验证点**：
- ✓ 两个阶段的请求都成功
- ✓ 两个阶段都路由到同一个Ch-vip渠道
- ✓ 阶段1：套餐扣减，余额不变
- ✓ 阶段2：套餐不扣减（已超限），余额扣减
- ✓ **路由逻辑不受套餐状态变化影响**（核心验证点）

---

## 测试实现架构

### 文件结构
```
scene_test/new-api-package/billing-routing/
├── billing_routing_test.go   # 主测试文件
├── helper.go                  # 辅助函数和断言工具
└── README.md                  # 本文档
```

### 测试套件结构
```go
type BillingRoutingTestSuite struct {
    suite.Suite
    // 测试服务器、数据库、Redis等基础设施
}
```

### 辅助函数
- `CallChatCompletion()`: 调用chat completion API
- `CreateChatRequest()`: 创建标准聊天请求
- `AssertPackageConsumed()`: 断言套餐被消耗
- `AssertUserQuotaUnchanged()`: 断言用户余额未变
- `AssertRoutedToChannel()`: 断言路由到指定渠道
- `AssertBillingGroup()`: 断言计费分组
- `CalculateExpectedQuota()`: 计算预期消耗quota

## 运行测试

### 运行所有计费与路由测试
```bash
cd scene_test/new-api-package/billing-routing
go test -v
```

### 运行特定测试用例
```bash
# BR-01
go test -v -run TestBR01_PackageAndBillingGroupIndependent

# BR-02
go test -v -run TestBR02_PackageDoesNotAffectP2PRouting

# BR-03
go test -v -run TestBR03_TokenOverridesBillingGroup

# BR-04
go test -v -run TestBR04_RoutingUnchangedAfterPackageExhausted
```

### 运行整个测试套件
```bash
go test -v -run TestBillingRoutingTestSuite
```

## 待实现项

当前测试代码为**完整的测试框架和逻辑**，但以下部分标记为`TODO`需要后续实现：

### 基础设施
- [ ] 测试服务器启动和停止
- [ ] 内存数据库初始化
- [ ] miniredis启动和配置
- [ ] Mock上游LLM服务

### 测试数据创建
- [ ] `CreateTestUser()`: 创建测试用户
- [ ] `CreateTestPackage()`: 创建测试套餐
- [ ] `CreateTestSubscription()`: 创建并启用订阅
- [ ] `CreateTestChannel()`: 创建测试渠道
- [ ] `CreateTestToken()`: 创建测试Token
- [ ] `CreateTestP2PGroup()`: 创建P2P分组
- [ ] `AddUserToP2PGroup()`: 将用户添加到P2P分组

### 数据验证
- [ ] 从响应中提取渠道ID（需要实现X-Channel-Id响应头或日志解析）
- [ ] 从RelayInfo中提取路由信息
- [ ] Redis滑动窗口状态验证
- [ ] 数据库状态验证

### 清理逻辑
- [ ] 测试数据清理函数

## 设计文档参考

- [NewAPI-支持多种包月套餐-优化版.md](../../../docs/NewAPI-支持多种包月套餐-优化版.md)
- [NewAPI-支持多种包月套餐-优化版-测试方案.md](../../../docs/NewAPI-支持多种包月套餐-优化版-测试方案.md)
- [07-NEW-API-分组改动详细设计.md](../../../docs/07-NEW-API-分组改动详细设计.md)

## 关键原则

### 解耦原则
> 套餐系统和路由系统必须解耦工作，互不影响核心逻辑

**套餐系统职责**：
- 决定从哪里扣费（套餐 or 用户余额）
- 管理滑动窗口限额
- 计算计费倍率

**路由系统职责**：
- 基于BillingGroup和RoutingGroups选择渠道
- 处理P2P分组授权
- 负载均衡和故障转移

### 独立性验证
每个测试用例都验证了以下独立性：
1. **路由不受套餐影响**：有无套餐、套餐优先级、套餐状态都不改变路由逻辑
2. **计费来源独立**：路由决策不依赖计费来源
3. **倍率计算独立**：GroupRatio由BillingGroup决定，不受路由渠道影响

## 测试覆盖矩阵

| 测试ID | 用户分组 | 套餐类型 | Token配置 | 渠道类型 | 套餐状态 | 验证点 |
|--------|---------|---------|-----------|---------|---------|--------|
| BR-01  | vip     | 全局    | 无特殊    | 系统分组 | 可用    | 路由+计费分离 |
| BR-02  | default | P2P     | 无特殊    | P2P+公共 | 可用    | P2P路由独立性 |
| BR-03  | vip     | 全局    | 覆盖分组  | 系统分组 | 可用    | Token覆盖生效 |
| BR-04  | vip     | 全局    | 无特殊    | 系统分组 | 可用→超限 | 路由稳定性 |

## 注意事项

1. **测试隔离**：每个测试用例应在独立的环境中运行，避免相互影响
2. **数据清理**：每个测试后应清理创建的数据
3. **并发安全**：测试中的数据操作应考虑并发安全性
4. **真实场景**：测试应尽可能模拟真实的用户请求场景
5. **日志记录**：关键步骤应有详细的日志输出，便于问题排查

## 常见问题

### Q: 如何获取请求使用的渠道ID？
A: 可以通过以下方式：
1. 响应头中的 `X-Channel-Id`
2. 日志解析
3. RelayInfo结构体（如果可访问）

### Q: 如何验证计费倍率？
A: 通过以下公式计算预期quota并与实际扣减对比：
```
ExpectedQuota = (InputTokens + OutputTokens × 1.2) × ModelRatio × GroupRatio
```

### Q: 为什么BR-04是P1而不是P0？
A: 虽然路由稳定性很重要，但这是一个edge case（套餐超限后的行为），相比核心路由逻辑（BR-01, BR-02）优先级稍低。

---

**最后更新**: 2025-12-12
**负责人**: QA Team
**状态**: 测试框架完成，待实现基础设施和数据层
