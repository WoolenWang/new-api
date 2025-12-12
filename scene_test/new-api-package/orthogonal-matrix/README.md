# 正交配置矩阵测试 (Orthogonal Matrix Test)

## 概述

本目录包含NewAPI包月套餐功能的正交配置矩阵测试，覆盖套餐、用户、渠道、Token等多维度配置组合的验证。

## 测试原理

正交测试是一种高效的测试设计技术，用于在多个因子的不同水平组合中，选择最具代表性的测试用例，以最少的测试用例覆盖最多的因子交互场景。

### 正交因子定义 (7个维度)

| 因子 | 水平（Level） | 说明 |
| :--- | :--- | :--- |
| **套餐类型** | L1: 全局套餐<br>L2: P2P分组套餐<br>L3: 双套餐组合 | 验证不同套餐归属和优先级组合 |
| **套餐优先级** | L1: 低优先级（5）<br>L2: P2P固定（11）<br>L3: 高优先级（15） | 验证优先级降级逻辑 |
| **用户系统分组** | L1: default<br>L2: vip<br>L3: svip | 验证计费倍率和分组路由 |
| **用户P2P分组** | L1: 未加入任何P2P分组<br>L2: 加入P2P分组G1 | 验证P2P权限和路由 |
| **渠道类型** | L1: 公共渠道<br>L2: P2P共享渠道<br>L3: 私有渠道<br>L4: 混合渠道 | 验证渠道可见性和路由 |
| **Token配置** | L1: 无特殊配置<br>L2: billing_group覆盖<br>L3: p2p_group_id限制 | 验证Token级别的权限控制 |
| **滑动窗口状态** | L1: 窗口不存在<br>L2: 窗口有效<br>L3: 窗口过期<br>L4: 窗口接近超限 | 验证滑动窗口的创建、累加、重建、超限逻辑 |

## 测试用例列表

### OM-01: 全局套餐高优先级VIP用户公共渠道
- **核心验证点**: 基础套餐扣减流程、窗口创建、计费分组
- **预期结果**: 套餐扣减成功，创建新窗口，用户余额不变
- **优先级**: P0

### OM-02: P2P套餐default用户G1渠道
- **核心验证点**: P2P套餐权限、P2P渠道路由
- **预期结果**: 套餐扣减成功，创建新窗口，计费分组=default
- **优先级**: P0

### OM-03: 全局套餐低优先级VIP用户billing覆盖为default
- **核心验证点**: Token billing_group覆盖、窗口累加
- **预期结果**: 套餐扣减成功，窗口累加，计费分组=default
- **优先级**: P0

### OM-04: P2P套餐VIP用户加入G1使用P2P渠道窗口过期
- **核心验证点**: 窗口过期检测、窗口重建、Token P2P限制
- **预期结果**: 窗口重建并套餐扣减成功
- **优先级**: P0

### OM-05: 全局套餐高优先级svip用户窗口有效但已超限
- **核心验证点**: 滑动窗口超限检测、Fallback到用户余额
- **预期结果**: 套餐超限，Fallback到用户余额
- **优先级**: P0

### OM-06: P2P套餐default用户加入G1但渠道为私有
- **核心验证点**: 私有渠道权限隔离、路由失败处理
- **预期结果**: 无法使用私有渠道，路由失败
- **优先级**: P0

### OM-07: 全局套餐低优先级default用户加入G1但Token无P2P限制
- **核心验证点**: Token无P2P限制时的P2P渠道隔离
- **预期结果**: Token无P2P限制时无法使用P2P渠道
- **优先级**: P0

### OM-08: 多套餐组合：全局15+P2P11，VIP用户，billing列表，窗口有效
- **核心验证点**: 多套餐优先级降级、混合渠道路由、billing列表优先级
- **预期结果**: 优先级15套餐超限后降级到优先级11套餐
- **优先级**: P0
- **特殊说明**: 需要发起两次请求，先耗尽高优先级套餐，再验证降级

## 运行测试

### 运行所有正交测试

```bash
cd scene_test/new-api-package/orthogonal-matrix
go test -v
```

### 运行特定测试用例

```bash
# 运行 OM-01
go test -v -run TestOM01_GlobalPackageHighPriorityVipUserPublicChannel

# 运行 OM-02
go test -v -run TestOM02_P2PPackageDefaultUserG1Channel

# 运行 OM-08（多套餐降级）
go test -v -run TestOM08_MultiPackagePriorityDegradation
```

### 运行整个测试套件

```bash
# 运行整个OrthogonalMatrixSuite
go test -v -run TestOrthogonalMatrixSuite
```

## 测试架构

### 测试流程

```
1. SetupSuite (一次)
   ├── 启动测试服务器
   ├── 初始化Redis Mock
   └── 初始化全局配置

2. For each test case:
   ├── SetupTest
   │   └── 清理数据库
   ├── setupOrthogonalTestCase
   │   ├── 创建用户
   │   ├── 创建P2P分组（如果需要）
   │   ├── 创建套餐
   │   ├── 创建订阅并启用
   │   ├── 创建渠道
   │   ├── 创建Token
   │   └── 设置滑动窗口状态
   ├── executePackageRequest
   │   ├── 构建请求参数
   │   └── 发起HTTP请求
   ├── verifyOrthogonalResult
   │   ├── 验证响应码
   │   ├── 验证套餐消耗
   │   ├── 验证用户余额
   │   ├── 验证计费分组
   │   └── 验证滑动窗口状态
   ├── cleanupOrthogonalTestCase
   │   ├── 删除Redis窗口
   │   ├── 删除订阅
   │   ├── 删除套餐
   │   ├── 删除Token
   │   ├── 删除渠道
   │   ├── 删除P2P分组
   │   └── 删除用户
   └── TearDownTest

3. TearDownSuite (一次)
   ├── 停止测试服务器
   └── 关闭Redis Mock
```

### 关键组件

| 组件 | 说明 | 文件 |
| :--- | :--- | :--- |
| **OrthogonalMatrixSuite** | 测试套件，管理服务器生命周期 | `orthogonal_test.go` |
| **OrthogonalTestCase** | 测试用例配置结构 | `orthogonal_test.go` |
| **OrthogonalTestContext** | 测试上下文，保存创建的实体ID | `orthogonal_test.go` |
| **orthogonalTestCases** | 8个预定义的正交测试用例 | `orthogonal_test.go` |
| **PackageTestData** | 套餐测试数据结构 | `testutil/package_helper.go` |
| **OrthogonalPackageHelper** | 正交测试辅助工具 | `testutil/orthogonal_package_helper.go` |

## 测试用例配置说明

每个测试用例包含以下配置项：

```go
type OrthogonalTestCase struct {
	ID                   string   // 测试用例ID (OM-01, OM-02, ...)
	Name                 string   // 测试用例名称
	PackageType          string   // 套餐类型: "global", "p2p", "both"
	PackagePriority      int      // 套餐优先级: 5, 11, 15
	PackageHourlyLimit   int64    // 小时限额 (quota单位)
	PackageFallback      bool     // 是否允许Fallback到用户余额
	UserGroup            string   // 用户系统分组: "default", "vip", "svip"
	UserInP2PGroup       bool     // 是否加入P2P分组
	P2PGroupName         string   // P2P分组名称
	ChannelType          string   // 渠道类型: "public", "p2p", "private", "mixed"
	ChannelSystemGroup   string   // 渠道的系统分组
	TokenConfig          string   // Token配置: "normal", "billing_override", "p2p_restriction"
	TokenBillingGroups   []string // Token覆盖的计费分组列表
	TokenP2PGroupID      int      // Token限制的P2P分组ID
	WindowState          string   // 滑动窗口状态: "not_exist", "active", "active_exceeded", "expired"
	ExpectedResult       string   // 预期结果: "package_consumed", "balance_consumed", "rejected"
	ExpectedBillingGroup string   // 预期的计费分组
	ExpectedRouting      string   // 预期的路由结果描述
}
```

## 验证点说明

### package_consumed (套餐扣减)

验证点：
- ✓ 请求成功 (HTTP 200)
- ✓ 使用套餐 (UsingPackageId > 0)
- ✓ 计费分组正确
- ✓ 套餐total_consumed增加
- ✓ 用户余额不变
- ✓ 滑动窗口状态正确（创建/累加/重建）

### balance_consumed (余额扣减)

验证点：
- ✓ 请求成功 (HTTP 200)
- ✓ 未使用套餐 (UsingPackageId = 0)
- ✓ 用户余额减少
- ✓ 套餐消耗不增加（或已达限额）

### rejected (请求被拒绝)

验证点：
- ✓ 请求失败 (HTTP 403/404/429)
- ✓ 套餐未扣减
- ✓ 用户余额不变
- ✓ 滑动窗口不创建（或不更新）

## 调试技巧

### 打印窗口详情

```go
testutil.DumpWindowInfo(t, redisMock, subscriptionID, "hourly")
```

### 检查订阅状态

```go
sub, _ := model.GetSubscriptionById(subscriptionID)
t.Logf("Subscription: ID=%d, TotalConsumed=%d, Status=%s",
    sub.Id, sub.TotalConsumed, sub.Status)
```

### 检查用户余额

```go
quota, _ := model.GetUserQuota(userID, true)
t.Logf("User quota: %d", quota)
```

### 单步调试特定用例

在测试方法中添加断点，然后运行：

```bash
dlv test -- -test.run TestOM01_GlobalPackageHighPriorityVipUserPublicChannel
```

## 扩展正交测试用例

### 添加新的测试用例

1. 在`orthogonalTestCases`数组中添加新的配置：

```go
{
	ID:                   "OM-09",
	Name:                 "新测试场景描述",
	PackageType:          "global",
	PackagePriority:      15,
	// ... 其他配置
	ExpectedResult:       "package_consumed",
	ExpectedBillingGroup: "vip",
	ExpectedRouting:      "预期路由描述",
},
```

2. (可选) 创建独立的测试方法：

```go
func (s *OrthogonalMatrixSuite) TestOM09_NewScenario() {
	s.T().Skip("等待实现")

	tc := orthogonalTestCases[8] // OM-09
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}
```

## 故障排查

### 测试跳过 (Skip)

**原因**: 后端套餐API未实现

**解决**:
1. 实现套餐相关的后端API（packages, subscriptions, sliding windows）
2. 移除测试方法中的 `s.T().Skip()` 语句

### 窗口状态断言失败

**原因**: RedisMock未初始化

**解决**:
1. 在`SetupSuite`中取消注释Redis Mock初始化：
```go
s.redisMock = testutil.NewRedisMock(s.T())
```

### 渠道路由失败

**原因**: 渠道系统分组或P2P授权配置不匹配

**检查**:
1. 用户的系统分组和P2P分组
2. 渠道的Group字段和AllowedGroups字段
3. Token的billing_group和p2p_group_id配置

### 套餐未扣减

**原因**: 套餐服务未启用或套餐优先级逻辑有误

**检查**:
1. 环境变量 `PACKAGE_ENABLED=true`
2. 套餐状态为 `active`
3. 订阅状态为 `active` 且未过期
4. 套餐优先级排序是否正确

## 性能考虑

### 测试隔离

每个测试用例独立创建和清理数据，确保测试之间互不影响。

### 并发运行

可以使用 `-parallel` 参数并发运行测试：

```bash
go test -v -parallel 4
```

**注意**: 由于测试会修改数据库，建议使用独立的测试数据库或内存数据库。

## 相关设计文档

- `docs/NewAPI-支持多种包月套餐-优化版.md` - 套餐系统设计文档
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 本测试方案依据
- `docs/07-NEW-API-分组改动详细设计.md` - 分组系统设计

## 维护者

- QA Team
- 后端开发团队

---

**最后更新**: 2025-12-12
**版本**: v1.0
