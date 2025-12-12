# P2P分组与套餐权限组合测试

## 测试目标

验证P2P分组套餐的权限隔离、订阅限制、动态权限变更等关键安全特性，确保：
1. **权限边界清晰**：只有分组成员可以看到和订阅P2P套餐
2. **动态权限管理**：用户退出分组后立即失去套餐使用权限
3. **优先级逻辑正确**：多个P2P套餐按ID排序

## 测试用例概览

| 测试ID | 测试场景 | 优先级 | 状态 |
|-------|---------|-------|------|
| PP-01 | P2P套餐仅组内可见 | P0 | ✅ 已实现 |
| PP-02 | P2P套餐仅组内可订阅 | P0 | ✅ 已实现 |
| PP-03 | 加入分组后可订阅 | P0 | ✅ 已实现 |
| PP-04 | 退出分组后订阅失效（关键安全测试） | P0 | ✅ 已实现 |
| PP-05 | P2P Owner自己订阅 | P1 | ✅ 已实现 |
| PP-06 | 多P2P分组套餐优先级 | P1 | ✅ 已实现 |

## 测试用例详解

### PP-01: P2P套餐仅组内可见

**测试目标**：验证套餐市场的可见性权限控制

**测试步骤**：
1. Owner创建P2P分组G1
2. 添加memberUserA到G1
3. Owner创建绑定到G1的P2P套餐
4. 外部用户查询套餐市场 → 不显示P2P套餐
5. 分组成员查询套餐市场 → 显示P2P套餐
6. 分组Owner查询套餐市场 → 显示P2P套餐

**关键验证点**：
- API层面的可见性过滤（`/api/packages/market`接口应该基于用户的P2P分组过滤）
- 确保外部用户无法通过任何方式获知P2P套餐的存在

---

### PP-02: P2P套餐仅组内可订阅

**测试目标**：验证订阅接口的权限控制

**测试步骤**：
1. Owner创建P2P分组G1和P2P套餐
2. 外部用户尝试订阅P2P套餐
3. 验证返回403 Forbidden
4. 验证数据库中没有创建订阅记录

**关键验证点**：
- 订阅接口必须验证用户是否为分组成员（`IsUserInGroup`检查）
- 防止恶意用户绕过前端直接调用API订阅未授权的套餐

**安全意义**：
这是防止权限提升攻击的关键测试，确保P2P套餐的隔离性。

---

### PP-03: 加入分组后可订阅

**测试目标**：验证正常的订阅流程

**测试步骤**：
1. Owner创建P2P分组G1和P2P套餐
2. Owner将用户A添加到G1（status=1 Active）
3. 验证数据库中存在user_groups记录
4. 用户A订阅P2P套餐
5. 验证订阅成功，状态为inventory

**关键验证点**：
- `user_groups.status=1` 是判断用户是否有权订阅的关键条件
- 订阅记录正确创建，初始状态为inventory

---

### PP-04: 退出分组后订阅失效（关键安全测试）

**测试目标**：验证动态权限撤销机制

**测试步骤**：
1. 用户A加入G1，订阅并启用P2P套餐
2. 验证用户在退出前拥有G1分组权限
3. 验证用户在退出前有可用套餐
4. 用户A退出G1分组
5. 验证数据库中的user_groups记录被删除/状态变更
6. 验证用户的P2P分组列表不再包含G1
7. **关键验证**：用户的可用套餐数量减少为0

**关键验证点**：
- 退出分组应该立即影响用户的套餐可用性
- `GetUserAvailablePackages`查询应该正确过滤掉用户已失去权限的P2P套餐
- 即使订阅记录仍为Active状态，也因用户失去分组权限而无法使用

**安全意义**：
这是最关键的安全测试，防止用户在被踢出分组后仍能使用分组资源。
这个测试确保了权限变更的即时性，避免权限泄漏。

---

### PP-05: P2P Owner自己订阅

**测试目标**：验证Owner的特殊权限

**测试步骤**：
1. Owner创建P2P分组G1
2. Owner创建绑定到G1的P2P套餐
3. 验证套餐的创建者是Owner
4. Owner订阅自己创建的套餐
5. 验证订阅成功

**关键验证点**：
- Owner作为分组创建者，默认拥有该分组的访问权限
- Owner可以订阅自己创建的P2P套餐

---

### PP-06: 多P2P分组套餐优先级

**测试目标**：验证同优先级套餐的排序逻辑

**测试步骤**：
1. Owner创建两个P2P分组G1和G2
2. 为两个分组分别创建套餐（优先级都是11）
3. 用户A加入两个分组
4. 用户A订阅并启用两个套餐
5. 查询用户可用套餐列表
6. 验证排序：优先级相同时按subscription.id升序

**关键验证点**：
- SQL排序逻辑：`ORDER BY packages.priority DESC, subscriptions.id ASC`
- 确保套餐选择的确定性和可预测性
- ID较小的订阅优先被选中使用

**业务意义**：
先购买的套餐优先消耗，符合用户直觉。

---

## 运行测试

### 运行所有P2P权限测试
```bash
cd scene_test/new-api-package/p2p-permission
go test -v ./...
```

### 运行特定测试用例
```bash
# 运行PP-01
go test -v -run TestPP01

# 运行PP-04（关键安全测试）
go test -v -run TestPP04

# 运行所有P0优先级测试
go test -v -run "PP0[1-4]"
```

### 生成覆盖率报告
```bash
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## 测试数据准备

每个测试用例在`SetupTest`阶段自动准备：
- **4个测试用户**：Owner、MemberA、MemberB、Outsider
- **对应的Token**：每个用户一个Token
- **清理机制**：每个测试后自动清理数据

## 依赖的辅助函数

所有辅助函数位于 `scene_test/testutil/package_helper.go`：

### P2P分组管理
- `CreateP2PGroupViaAPI`: 通过API创建P2P分组
- `AddUserToGroupViaAPI`: 通过API添加用户到分组
- `RemoveUserFromGroupViaAPI`: 通过API从分组移除用户

### P2P套餐管理
- `CreateP2PPackageViaAPI`: 通过API创建P2P套餐
- `QueryPackageMarketViaAPI`: 通过API查询套餐市场
- `SubscribePackageViaAPI`: 通过API订阅套餐
- `CheckPackageInMarket`: 检查套餐是否在市场列表中

### 数据库验证
- `GetUserP2PGroupIDs`: 获取用户的P2P分组ID列表
- `GetUserAvailablePackageCount`: 获取用户可用套餐数量
- `AssertPackageExists`: 断言套餐存在
- `AssertPackagePriority`: 断言套餐优先级

## 关键设计要点

### 1. 权限隔离机制
P2P套餐通过`p2p_group_id`字段绑定到特定分组，只有满足以下条件的用户才能访问：
- 用户在`user_groups`表中有对应记录
- `user_groups.status = 1` (Active)

### 2. 动态权限变更
用户退出分组时，虽然订阅记录（`subscriptions`表）仍然存在，但在查询可用套餐时会被过滤掉：
```sql
WHERE packages.p2p_group_id = 0 OR packages.p2p_group_id IN (用户的P2P分组列表)
```

### 3. 优先级排序规则
所有P2P套餐优先级固定为11，同优先级按订阅ID升序排序：
```sql
ORDER BY packages.priority DESC, subscriptions.id ASC
```

## 测试覆盖的安全风险

1. **权限提升攻击**：外部用户通过直接调用API订阅P2P套餐
2. **权限泄漏**：用户退出分组后仍能使用套餐资源
3. **信息泄露**：外部用户通过套餐市场获知P2P套餐的存在
4. **优先级绕过**：通过创建多个订阅操纵优先级

## 故障排查

### 测试失败常见原因

1. **PP-01/PP-02失败**：
   - 检查套餐市场API是否正确实现P2P分组过滤
   - 检查订阅接口是否验证`IsUserInGroup`

2. **PP-04失败（最关键）**：
   - 检查退出分组接口是否正确删除/更新`user_groups`记录
   - 检查`GetUserAvailablePackages`查询SQL是否包含P2P分组过滤
   - 验证缓存是否正确失效（退出分组后应清除用户的P2P分组缓存）

3. **PP-06失败**：
   - 检查SQL的ORDER BY子句
   - 验证subscription.id的自增顺序

### 调试技巧

```go
// 打印用户的P2P分组列表
groupIDs := testutil.GetUserP2PGroupIDs(t, userID)
t.Logf("User P2P Groups: %v", groupIDs)

// 打印可用套餐数量
count := testutil.GetUserAvailablePackageCount(t, userID, groupIDs)
t.Logf("Available Package Count: %d", count)

// 检查数据库中的user_groups记录
var userGroups []model.UserGroup
model.DB.Where("user_id = ?", userID).Find(&userGroups)
for _, ug := range userGroups {
	t.Logf("UserGroup: groupID=%d, status=%d", ug.GroupId, ug.Status)
}
```

## 参考文档

- `docs/NewAPI-支持多种包月套餐-优化版.md` - 套餐系统设计
- `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md` - 测试方案设计
- `docs/07-NEW-API-分组改动详细设计.md` - P2P分组设计
- `scene_test/README.md` - 测试框架说明

## 维护者

- QA Team
- Backend Development Team

---

**创建时间**: 2025-12-12
**版本**: v1.0
**对应测试方案**: 2.11 P2P分组与套餐权限组合测试
