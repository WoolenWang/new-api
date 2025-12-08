# **NEW-API 分组计费相关实现计划 (v1.0)**
**文档目的**: 本文档为“P2P分组与计费解耦”功能提供一个分阶段、可并行、可验收的详细编码任务清单。计划旨在将设计文档中的架构蓝图转化为具体的开发任务，并明确每个任务的范围、目标和验收标准。

---

## **总体目标与验收标准**

**核心目标**: 彻底解耦计费与路由逻辑，引入灵活的计费组列表和P2P分组限制，以支持WQuant平台的复杂共享和成本优化需求。

**最终验收标准**:
1.  **计费组列表生效**: Token中定义的`group`计费组列表能够被正确解析，并在选路时按顺序进行失败转移。
2.  **P2P分组限制生效**: Token中定义的`p2p_group_id`能正确地将用户的P2P渠道访问范围限制在单个分组内。
3.  **权限校验闭环**: 在最终确定计费分组后，系统能有效校验该分组是否在用户的`UserUsableGroups`中，并正确拒绝无权限的请求。
4.  **计费准确性**: 无论通过何种路由逻辑（系统分组、P2P分组、计费组列表），最终的扣费都严格按照最终确定的`UsingGroup`和用户自身的`UserGroup`（用于匹配特殊倍率）进行。
5.  **自动化测试覆盖**: 新增的自动化集成测试用例（`scene_test`）能覆盖所有新的路由、计费和并发场景，确保改动后的系统稳定可靠。

---
## **阶段一：数据库与模型层改造**
**阶段目标**: 调整数据库表结构以支持新的分组设计，并更新 GORM 模型和缓存逻辑。

**阶段验收标准**:
1. `tokens` 表中 `allowed_p2p_groups` 字段被移除，新增 `p2p_group_id` 字段。
2. `Token` 模型中的 `Group` 字段能正确处理JSON数组字符串。
3. `User` 缓存模型中包含 `ExtendedGroups` 字段，并且 `GetUserActiveGroups` 能正确实现三级缓存（内存 -> Redis -> DB）的回填和失效逻辑。

| 任务ID | 文件/模块 | 修改内容 | 目标与验收标准 |
| :--- | :--- | :--- | :--- |
| 1.1 | `model/token.go` | **修改 `Token` 模型**。将 `AllowedP2PGroups` 字段重命名为 `P2PGroupID`，并修改其类型为 `*int`。修改 `Group` 字段的相关处理逻辑，使其能解析JSON数组成员。 | 1. `Token` 结构体中的字段与新设计匹配。<br>2. `GetAllowedP2PGroupIDs` 方法被移除或修改为 `GetP2PGroupID`。<br>3. `Token.Group` 的相关逻辑能处理 `["svip", "default"]` 这样的字符串。 |
| 1.2 | `model/main.go` | **更新数据库迁移**。在 `AutoMigrate` 中确保 `tokens` 表的结构变更能被正确执行。 | 1. 启动应用时，`tokens` 表结构自动更新。<br>2. （可选）提供一个一次性的SQL脚本用于迁移旧的 `allowed_p2p_groups` 数据到 `p2p_group_id`。 |
| 1.3 | `model/group_cache.go` | **完善P2P分组缓存**。确保 `GetUserActiveGroups` 实现了内存、Redis、DB三级缓存策略，并提供 `InvalidateUserGroupCache` 方法。 | 1. `GetUserActiveGroups` 在缓存未命中时能正确从数据库加载并回填缓存。<br>2. 在用户加入/退出P2P分组的操作中，能调用 `InvalidateUserGroupCache` 清除对应用户的缓存。 |

---
## **阶段二：核心路由与计费逻辑实现**
**阶段目标**: 重构数据面核心的渠道选择和计费逻辑，以支持新的分组设计。

**阶段验收标准**:
1. `Distribute` 中间件能正确构建包含多个计费组和P2P分组的 `RoutingGroups`。
2. `channel_select.go` 中的选路函数能正确处理多计费组的迭代和失败转移。
3. `price.go` 中的计费帮助函数始终基于最终的 `BillingGroup` 和用户的 `UserGroup` 来计算费用。

| 任务ID | 文件/模块 | 修改内容 | 目标与验收标准 |
| :--- | :--- | :--- | :--- |
| 2.1 | `middleware/distributor.go` | **重构上下文构建逻辑**。修改 `Distribute` 中间件，以实现新的 `BillingGroupList` 和 `P2PGroupID` 的解析。 | 1. 从`Token`中正确解析出 `BillingGroupList` (JSON数组) 和 `P2PGroupID` (整数)。<br>2. 调用`GetUserActiveGroups`获取用户的P2P分组。<br>3. 正确计算出最终的`RoutingGroups`并存入上下文，供后续选路使用。 |
| 2.2 | `service/channel_select.go` | **实现多计费组迭代选路**。修改 `CacheGetRandomSatisfiedChannelMultiGroup` (或新增一个函数)。 | 1. 函数接收 `BillingGroupList` 作为输入。<br>2. 按顺序遍历列表中的每个计费组。<br>3. 在每次迭代中，构建临时的`RoutingGroups`（当前计费组 + P2P组）进行渠道查找。<br>4. 一旦找到可用渠道，立即停止遍历并返回结果。<br>5. 如果所有计费组都无可用渠道，返回错误。 |
| 2.3 | `middleware/auth.go` | **调整Token认证逻辑**。修改 `TokenAuth` 中间件，确保在解析`Token.Group`时兼容字符串和数组两种格式。 | 1. 当 `Token.Group` 是JSON数组时，能正确解析并传递给后续逻辑。<br>2. 当 `Token.Group` 是单个字符串时，能作为 `["groupName"]` 的形式兼容处理。<br>3. 新增对 `p2p_group_id` 的解析和上下文设置。 |

---
## **阶段三：API接口与表现层调整**
**阶段目标**: 调整Token管理相关的API，使其支持新的分组配置字段。

**阶段验收标准**:
1. 创建和更新Token的API接口能够接收并正确处理 `group` (作为JSON数组字符串) 和 `p2p_group_id` 字段。
2. 获取Token列表和详情的API能正确返回新的分组字段。

| 任务ID | 文件/模块 | 修改内容 | 目标与验收标准 |
| :--- | :--- | :--- | :--- |
| 3.1 | `controller/token.go` | **更新Token管理API**。修改 `AddToken` 和 `UpdateToken` 方法。 | 1. `AddToken` 和 `UpdateToken` 能接收 `group` (JSON数组字符串) 和 `p2p_group_id` (整数)参数，并正确保存到数据库。<br>2. 对输入的 `group` 字段进行校验，确保是合法的JSON数组或单个字符串。 |
| 3.2 | `dto/openai_request.go` & 其他 `dto` 文件 | **检查并更新DTO定义**。（可能需要） | 1. 确保与 `Token` 模型相关的DTO（如果有）与数据库模型保持一致。 |

---
## **阶段四：自动化测试用例补充**
**阶段目标**: 编写新的集成测试用例，全面覆盖新的路由和计费逻辑，确保代码质量和系统稳定性。

**阶段验收标准**:
1.  所有新增的测试用例都已添加到 `scene_test` 目录下，并通过CI。
2.  测试用例覆盖了多计费组的成功和失败转移场景。
3.  测试用例覆盖了P2P分组限制的生效和失效场景。
4.  测试用例覆盖了计费逻辑在复杂路由场景下的准确性。

| 任务ID | 文件/模块 | 修改内容 | 目标与验收标准 |
| :--- | :--- | :--- | :--- |
| 4.1 | `scene_test/new-api-data-plane/routing-authorization/` | **补充核心路由测试**。 | 1. 新增测试用例 `TestRouting_TokenWithBillingGroupList_Success`：Token的`group`列表第一个分组就有渠道，验证路由和计费正确。<br>2. 新增测试用例 `TestRouting_TokenWithBillingGroupList_Failover`：第一个分组无渠道，第二个分组有渠道，验证路由和计费正确。<br>3. 新增测试用例 `TestRouting_TokenWithP2PRestriction`：Token指定`p2p_group_id`，验证只能访问该P2P分组下的渠道。 |
| 4.2 | `scene_test/new-api-data-plane/billing/` | **补充计费场景测试**。 | 1. 新增测试用例 `TestBilling_TokenWithBillingGroupList`，验证在多计费组场景下，最终扣费使用的倍率是最终选定渠道所在计费组的倍率。 |

